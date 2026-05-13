package library

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/operator"
)

const RetrieveOpDescription = `RetrieveOp: pulls the top-k documents most relevant to a query from a registered Retriever (RAG fan-in).
  Params:   k string — number of documents to return (default "5"). Lower values keep prompts compact; higher values broaden recall.
            retriever_id string — selects a Retriever registered via library.RegisterRetriever (default "" → process default set via library.SetDefaultRetriever).
            credential_ref string — opaque credential identifier installed into ctx for the Retriever (default ""; consumed by library.ResolveEmbeddingClient inside vector-store-backed Retrievers; ignored by Retrievers that don't embed). credential_ref is a LOOKUP REFERENCE (e.g. an env-var name like "CLAUDE_API_KEY" or a factory key like "voyage-prod") that a registered EmbeddingClientFactory translates into a real credential — it is NOT the secret value itself. Never put an API key, OAuth token, vault URL, or any other secret material as the credential_ref value in vertex params: those values land verbatim in the generated main.go, get committed to git, and end up indexed by every tool that reads the repo.
            client_factory_id string — selects a registered EmbeddingClientFactory by id for the Retriever's embedding lookup (default "" → process default set via library.SetDefaultEmbeddingClientFactory). NOTE: the bundled EnvEmbeddingClientFactory only supports provider="gemini". For any other embedding provider (Claude, OpenAI, Voyage, Cohere, …) you MUST register a custom factory via library.RegisterEmbeddingClientFactory (or library.SetDefaultEmbeddingClientFactory) before engine.Run — the default rejects unknown providers with an error. This is asymmetric with AI ops, whose bundled EnvAIClientFactory supports both Claude and Gemini.
            api_factory_timeout_ms string — deadline for the EmbeddingClientFactory credential lookup in milliseconds (default "30000"; "0" disables). Bounds the factory's credential-lookup leg only (Vault / Secrets Manager / KMS round trip); the subsequent Retriever.Retrieve call is NOT covered by this budget. Mirrors AIClientFactory's api_factory_timeout_ms.
            embed_timeout_ms string — wallclock deadline applied to the ENTIRE Retriever.Retrieve call in milliseconds (embedding API call + vector search + post-filtering — whatever the Retriever does end-to-end). Default "" / "0" = no per-op deadline (the call honors only the ambient ctx). Wraps the Retrieve invocation with context.WithTimeout; when it fires, the op returns the wrapped error and the underlying ctx.DeadlineExceeded. Complementary to api_factory_timeout_ms: factory_timeout caps the credential lookup, embed_timeout caps the actual retrieval work.
  Inputs:   Query *string — the natural-language search query.
  Outputs:  Documents []library.Document — full records ({ID, Content, Score, Metadata}) sorted best-first. Use when downstream ops need IDs, scores, or Retriever-specific metadata (citation URL, highlights, timestamps, ACL flags, per-field scores — anything the Retriever stuffed into Metadata).
            Texts []string — parallel slice of Documents[i].Content in the same order. Wire this into AI ops that take *[]string (AISummarizeOp, AIRerankOp, AIBestMatchOp candidates).
  Setup is fail-fast: if no Retriever is registered the graph errors before any vertex runs. Call SetDefaultRetriever (or RegisterRetriever) before engine.Run. The three credential params are installed into ctx via library.WithEmbeddingCredentials and consumed by user Retrievers via library.ResolveEmbeddingClient — omit them for Retrievers that don't embed (BM25, hosted search with its own auth, etc.).`

// RetrieveOp wraps any registered Retriever so a graph can pull external
// context (knowledge base, vector store, search service) into a downstream AI
// op. Documents and Texts are parallel: Texts[i] == Documents[i].Content.
type RetrieveOp struct {
	Query     *string    `dag:"input"`
	Documents []Document `dag:"output"`
	Texts     []string   `dag:"output"`

	k                 int
	retrieverID       string
	credRef           string
	factoryID         string
	factoryTimeout    time.Duration
	factoryTimeoutSet bool
	embedTimeout      time.Duration
	retriever         Retriever
}

func (op *RetrieveOp) Setup(params *config.Params) error {
	op.k = 5
	if s := params.GetString("k", ""); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("RetrieveOp: param k = %q: %w", s, err)
		}
		if n <= 0 {
			return fmt.Errorf("RetrieveOp: param k must be positive, got %d", n)
		}
		op.k = n
	}
	op.retrieverID = params.GetString("retriever_id", "")
	op.credRef = params.GetString("credential_ref", "")
	op.factoryID = params.GetString("client_factory_id", "")
	op.factoryTimeout = 30 * time.Second
	if s := params.GetString("api_factory_timeout_ms", ""); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("RetrieveOp: param api_factory_timeout_ms = %q: %w", s, err)
		}
		op.factoryTimeout = time.Duration(n) * time.Millisecond
		op.factoryTimeoutSet = true
	}
	if s := params.GetString("embed_timeout_ms", ""); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("RetrieveOp: param embed_timeout_ms = %q: %w", s, err)
		}
		if n < 0 {
			return fmt.Errorf("RetrieveOp: param embed_timeout_ms must be non-negative, got %d", n)
		}
		op.embedTimeout = time.Duration(n) * time.Millisecond
	}
	r, err := resolveRetriever(op.retrieverID)
	if err != nil {
		return fmt.Errorf("RetrieveOp: %w", err)
	}
	op.retriever = r
	return nil
}

func (op *RetrieveOp) Reset() error { return nil }

func (op *RetrieveOp) Run(ctx context.Context) error {
	slog.DebugContext(ctx, "RetrieveOp.run", "run_id", dagor.RunID(ctx), "k", op.k, "retriever_id", op.retrieverID)

	// Only install embedding credentials on ctx when the vertex was
	// configured with at least one credential-related param. Retrievers that
	// don't embed (BM25, hosted search with its own auth) leave all three
	// unset; installing the zero value would still overwrite any creds the
	// caller installed upstream and could mislead wrappers that read ctx.
	// A user-set api_factory_timeout_ms=0 (explicit "disable the deadline")
	// must still install — the install signals intent, distinct from the
	// "user said nothing" no-op path.
	if op.credRef != "" || op.factoryID != "" || op.factoryTimeoutSet {
		ctx = WithEmbeddingCredentials(ctx, EmbeddingCredentials{
			Ref:            op.credRef,
			FactoryID:      op.factoryID,
			FactoryTimeout: op.factoryTimeout,
		})
	}

	callCtx := ctx
	if op.embedTimeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, op.embedTimeout)
		defer cancel()
	}
	docs, err := op.retriever.Retrieve(callCtx, *op.Query, op.k)
	if err != nil {
		return fmt.Errorf("RetrieveOp: retrieve: %w", err)
	}
	op.Documents = docs
	op.Texts = make([]string, len(docs))
	for i, d := range docs {
		op.Texts[i] = d.Content
	}
	slog.InfoContext(ctx, "RetrieveOp.retrieved", "run_id", dagor.RunID(ctx), "count", len(docs))
	return nil
}

func init() {
	operator.RegisterOp[RetrieveOp]()
}
