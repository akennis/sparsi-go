package library

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"google.golang.org/genai"
)

// EmbeddingClient is the framework-owned shape user Retrievers consume.
// Implementations call whichever provider SDK they want internally; the
// library only sees []float32 vectors.
//
// Embed returns one vector per input text in the same order. Implementations
// must be safe for concurrent calls from parallel graph vertices.
type EmbeddingClient interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// EmbeddingClientFactory constructs EmbeddingClients. ref is opaque to the
// library, same contract as AIClientFactory.ref (Vault path, tenant id,
// region — implementations decide). provider and model are passed through
// so a single factory can serve multiple embedders (e.g. one factory
// routing "voyage" and "openai" through different SDKs).
//
// Implementations decide whether to cache resulting clients per
// (provider, model, ref) and how to handle unsupported provider strings.
type EmbeddingClientFactory interface {
	Embedder(ctx context.Context, provider, model, ref string) (EmbeddingClient, error)
}

// geminiEmbeddingMaxBatch caps the number of inputs sent in a single
// gemini-embedding-001 EmbedContent request. Gemini's documented per-call
// batch limit for the embedding endpoint is on the order of 100 inputs as of
// Q1 2026; we use 100 as a defensive default to bound request size, memory,
// and quota exposure when a caller (e.g. retrieval over a corpus assembled
// from user uploads) passes a very large slice. The cap is not user-tunable
// — geminiEmbeddingClient.Embed automatically chunks larger inputs into
// sequential calls of at most this size and concatenates results in input
// order.
const geminiEmbeddingMaxBatch = 100

// EnvEmbeddingClientFactory is gemini-only — it is the bundled default and
// supports ONLY provider="gemini". This is asymmetric with the bundled
// EnvAIClientFactory, which supports both Claude and Gemini; for any other
// embedding provider (Claude, OpenAI, Voyage, Cohere, Vertex, …) you must
// register a custom EmbeddingClientFactory via RegisterEmbeddingClientFactory
// (or SetDefaultEmbeddingClientFactory) before engine.Run.
//
// It serves gemini via the existing google.golang.org/genai SDK
// (GEMINI_API_KEY) and rejects every other provider with an error pointing
// the user at RegisterEmbeddingClientFactory. Embedding vendors are
// fragmented (Voyage, OpenAI, Cohere, Vertex, …) and each requires a
// different SDK or HTTP adapter; users wiring those plug in their own
// factory.
//
// The factory caches one *genai.Client per ref; the lightweight
// geminiEmbeddingClient adapter is built on demand wrapping (client, model).
// Env-var credentials don't rotate, so a single entry under the empty ref
// is the steady state for almost all callers.
//
// SECURITY: the per-ref cache has no eviction. Do NOT derive ref from
// per-request input (tenant id, user id, request header value, query
// parameter, anything an attacker can vary): doing so produces an
// unbounded cache that leaks one *genai.Client per distinct value and is
// a memory-exhaustion / DoS vector. Use ref only for the handful of
// named credential lookups the application itself controls (e.g. "prod",
// "staging", "voyage-prod") and define that set at deploy time, not from
// request data.
//
// Batch size: the returned EmbeddingClient.Embed caps each upstream
// EmbedContent call at geminiEmbeddingMaxBatch (100) inputs and chunks
// larger slices sequentially; callers do not need to pre-chunk.
type EnvEmbeddingClientFactory struct {
	mu     sync.Mutex
	gemini map[string]*genai.Client
}

// Embedder returns an EmbeddingClient for the requested provider. Only
// "gemini" is supported by the bundled default; all other providers must
// register a custom factory.
func (f *EnvEmbeddingClientFactory) Embedder(ctx context.Context, provider, model, ref string) (EmbeddingClient, error) {
	if provider != "gemini" {
		return nil, fmt.Errorf("EnvEmbeddingClientFactory: provider %q not supported; register a custom EmbeddingClientFactory via library.RegisterEmbeddingClientFactory (or SetDefaultEmbeddingClientFactory)", provider)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.gemini[ref]
	if !ok {
		// Warn at most once per ref that the bundled factory ignores ref and
		// uses GEMINI_API_KEY only. The cache miss above guarantees the warn
		// fires on first resolution; subsequent calls for the same ref hit
		// the cache and skip this branch entirely. Skip when ref=="" because
		// that's the documented "use env defaults" path.
		if ref != "" {
			slog.WarnContext(ctx, fmt.Sprintf("EnvEmbeddingClientFactory: ref=%q is ignored — bundled factory uses GEMINI_API_KEY env var only. Register a custom factory via RegisterEmbeddingClientFactory for per-ref credential routing.", ref), "ref", ref)
		}
		var err error
		c, err = genai.NewClient(ctx, &genai.ClientConfig{APIKey: os.Getenv("GEMINI_API_KEY")})
		if err != nil {
			return nil, fmt.Errorf("gemini embedding: create client: %w", err)
		}
		if f.gemini == nil {
			f.gemini = map[string]*genai.Client{}
		}
		f.gemini[ref] = c
	}
	return &geminiEmbeddingClient{client: c, model: model}, nil
}

// geminiEmbeddingClient adapts *genai.Client to EmbeddingClient.
//
// embedOnce is an indirection used in tests; production code leaves it nil
// and Embed dispatches to c.client.Models.EmbedContent directly. Tests can
// install a fake to assert chunking behavior without an SDK round trip.
type geminiEmbeddingClient struct {
	client    *genai.Client
	model     string
	embedOnce func(ctx context.Context, contents []*genai.Content) (*genai.EmbedContentResponse, error)
}

// Embed returns one vector per input in the same order. Empty input returns
// nil without touching the API.
//
// Batch size cap: requests are automatically chunked at
// geminiEmbeddingMaxBatch (100) inputs per upstream EmbedContent call. A
// slice of <= 100 makes a single call (today's behavior); larger slices are
// split into sequential calls and concatenated in input order. The cap
// guards against oversize requests, large memory allocations, and quota
// exhaustion when caller text comes from an untrusted corpus; it is not
// user-tunable.
//
// If any chunk fails, Embed surfaces the first error and returns without
// continuing to later chunks. Partial results from earlier chunks are
// discarded.
func (c *geminiEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += geminiEmbeddingMaxBatch {
		end := start + geminiEmbeddingMaxBatch
		if end > len(texts) {
			end = len(texts)
		}
		chunk := texts[start:end]
		vecs, err := c.embedChunk(ctx, chunk)
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

// embedChunk issues a single EmbedContent call for a slice already known to
// be within geminiEmbeddingMaxBatch and validates the response shape.
func (c *geminiEmbeddingClient) embedChunk(ctx context.Context, texts []string) ([][]float32, error) {
	contents := make([]*genai.Content, len(texts))
	for i, t := range texts {
		contents[i] = genai.NewContentFromText(t, genai.RoleUser)
	}
	embedOnce := c.embedOnce
	if embedOnce == nil {
		embedOnce = func(ctx context.Context, contents []*genai.Content) (*genai.EmbedContentResponse, error) {
			return c.client.Models.EmbedContent(ctx, c.model, contents, nil)
		}
	}
	resp, err := embedOnce(ctx, contents)
	if err != nil {
		return nil, fmt.Errorf("gemini embedding: embed content: %w", err)
	}
	if len(resp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("gemini embedding: response count mismatch: got %d, want %d", len(resp.Embeddings), len(texts))
	}
	out := make([][]float32, len(texts))
	for i, e := range resp.Embeddings {
		if e == nil {
			return nil, fmt.Errorf("gemini embedding: nil embedding at index %d", i)
		}
		out[i] = e.Values
	}
	return out, nil
}

var (
	embeddingFactoryMu       sync.RWMutex
	defaultEmbeddingFactory  EmbeddingClientFactory = &EnvEmbeddingClientFactory{}
	embeddingFactoryRegistry                        = map[string]EmbeddingClientFactory{}
)

// SetDefaultEmbeddingClientFactory replaces the process-wide default
// embedding factory. Most enterprise integrations call this once at program
// start. Passing nil resets to the bundled EnvEmbeddingClientFactory.
func SetDefaultEmbeddingClientFactory(f EmbeddingClientFactory) {
	embeddingFactoryMu.Lock()
	defer embeddingFactoryMu.Unlock()
	if f == nil {
		defaultEmbeddingFactory = &EnvEmbeddingClientFactory{}
		return
	}
	defaultEmbeddingFactory = f
}

// RegisterEmbeddingClientFactory registers a factory under an id. RetrieveOp
// and RetrieveWithFiltersOp vertices opt in by setting client_factory_id;
// the value flows to user Retrievers via context so they can call
// ResolveEmbeddingClient against the right credential. Absent or unknown
// ids fall back to the default factory. Passing nil deregisters.
func RegisterEmbeddingClientFactory(id string, f EmbeddingClientFactory) {
	embeddingFactoryMu.Lock()
	defer embeddingFactoryMu.Unlock()
	if f == nil {
		delete(embeddingFactoryRegistry, id)
		return
	}
	embeddingFactoryRegistry[id] = f
}

// resolveEmbeddingFactory looks up an id in the registry; missing ids fall
// back to the process-wide default.
func resolveEmbeddingFactory(id string) EmbeddingClientFactory {
	embeddingFactoryMu.RLock()
	defer embeddingFactoryMu.RUnlock()
	if id != "" {
		if f, ok := embeddingFactoryRegistry[id]; ok {
			return f
		}
	}
	return defaultEmbeddingFactory
}

// embeddingCredsKey is the unexported context key under which
// RetrieveOp / RetrieveWithFiltersOp install credential routing for
// embedding lookups. User Retrievers read it indirectly via
// ResolveEmbeddingClient.
type embeddingCredsKey struct{}

// EmbeddingCredentials carries the credential routing values flowing from a
// retrieval vertex to a user Retriever. Ref and FactoryID match the
// AIClientFactory contract: Ref is opaque to the library, FactoryID selects
// a registered factory (empty → default). FactoryTimeout bounds the factory
// credential lookup only (the subsequent Embed call honors the ambient ctx).
type EmbeddingCredentials struct {
	Ref            string
	FactoryID      string
	FactoryTimeout time.Duration
}

// WithEmbeddingCredentials returns a new context carrying the credentials
// that user Retrievers consume via ResolveEmbeddingClient. RetrieveOp and
// RetrieveWithFiltersOp call this in Run; user code can also call it
// directly to override the vertex-installed values (for example when a
// multi-embed-provider Retriever needs per-provider credentials).
func WithEmbeddingCredentials(ctx context.Context, c EmbeddingCredentials) context.Context {
	return context.WithValue(ctx, embeddingCredsKey{}, c)
}

// EmbeddingCredentialsFromContext extracts credentials installed by
// WithEmbeddingCredentials. Returns the zero value when nothing has been
// installed; user Retrievers should treat empty Ref/FactoryID as
// "use process defaults".
func EmbeddingCredentialsFromContext(ctx context.Context) EmbeddingCredentials {
	c, _ := ctx.Value(embeddingCredsKey{}).(EmbeddingCredentials)
	return c
}

// ResolveEmbeddingClient builds an EmbeddingClient using the credentials
// installed on ctx (via WithEmbeddingCredentials) and the requested provider
// and model. This is the canonical entry point user Retrievers call to
// embed query text; never read embedding env vars directly.
//
// If FactoryTimeout > 0 on the installed credentials, only the factory
// credential lookup is bounded by that deadline — the returned client's
// Embed calls honor whatever ctx the caller passes them.
//
// Deadline scope is narrow: FactoryTimeout bounds ONLY this function's
// call into EmbeddingClientFactory.Embedder (the credential-lookup leg —
// Vault / Secrets Manager / KMS round trip / SDK client construction).
// Once the returned EmbeddingClient is in hand, every subsequent call
// against it (notably Embed) inherits its deadline entirely from
// whatever ctx the caller passes, and depends on the underlying SDK
// honoring ctx for cancellation. Custom Retrievers built on top of this
// MUST thread the inbound ctx into every downstream call (Embed, vector
// search, post-filtering) so the framework-level retrieval timeout
// propagates. RetrieveOp / RetrieveWithFiltersOp wrap the entire
// Retrieve invocation with context.WithTimeout when embed_timeout_ms is
// set, so framework-wired graphs are protected end-to-end; ad-hoc
// Retriever code outside that wrapper is not.
func ResolveEmbeddingClient(ctx context.Context, provider, model string) (EmbeddingClient, error) {
	creds := EmbeddingCredentialsFromContext(ctx)
	factory := resolveEmbeddingFactory(creds.FactoryID)
	callCtx := ctx
	if creds.FactoryTimeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, creds.FactoryTimeout)
		defer cancel()
	}
	client, err := factory.Embedder(callCtx, provider, model, creds.Ref)
	if err != nil {
		return nil, fmt.Errorf("embedding client: %w", err)
	}
	return client, nil
}
