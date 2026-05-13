package library

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/operator"
)

const RetrieveWithFiltersOpDescription = `RetrieveWithFiltersOp: like RetrieveOp but with filters that scope the retrieval. Filters may come from a runtime Filters input wire (values produced by upstream graph ops — tenant id from an auth step, category from a classifier, date range from a planner), from a compile-time static_filters param (values known when the graph is wired — a hardcoded tenant id, a fixed locale), or both.
  Params:   k string — number of documents to return (default "5").
            retriever_id string — selects a Retriever registered via library.RegisterRetriever (default "" → process default).
            credential_ref string — opaque credential identifier installed into ctx for the Retriever (default ""; consumed by library.ResolveEmbeddingClient inside vector-store-backed Retrievers; ignored by Retrievers that don't embed). credential_ref is a LOOKUP REFERENCE (e.g. an env-var name like "CLAUDE_API_KEY" or a factory key like "voyage-prod") that a registered EmbeddingClientFactory translates into a real credential — it is NOT the secret value itself. Never put an API key, OAuth token, vault URL, or any other secret material as the credential_ref value in vertex params: those values land verbatim in the generated main.go, get committed to git, and end up indexed by every tool that reads the repo.
            client_factory_id string — selects a registered EmbeddingClientFactory by id for the Retriever's embedding lookup (default "" → process default set via library.SetDefaultEmbeddingClientFactory). NOTE: the bundled EnvEmbeddingClientFactory only supports provider="gemini". For any other embedding provider (Claude, OpenAI, Voyage, Cohere, …) you MUST register a custom factory via library.RegisterEmbeddingClientFactory (or library.SetDefaultEmbeddingClientFactory) before engine.Run — the default rejects unknown providers with an error. This is asymmetric with AI ops, whose bundled EnvAIClientFactory supports both Claude and Gemini.
            api_factory_timeout_ms string — deadline for the EmbeddingClientFactory credential lookup in milliseconds (default "30000"; "0" disables). Bounds the factory's credential-lookup leg only (Vault / Secrets Manager / KMS round trip); the subsequent Retriever.Retrieve call is NOT covered by this budget.
            embed_timeout_ms string — wallclock deadline applied to the ENTIRE Retriever.Retrieve call in milliseconds (embedding API call + vector search + post-filtering — whatever the Retriever does end-to-end). Default "" / "0" = no per-op deadline (the call honors only the ambient ctx). Wraps the Retrieve invocation with context.WithTimeout; when it fires, the op returns the wrapped error and the underlying ctx.DeadlineExceeded. Complementary to api_factory_timeout_ms.
            static_filters string — comma-separated key=value pairs of filters known at graph-build time (e.g. "tenant=acme,locale=en"). Parsed at Setup and merged into the filter map every Run. Default "" = no static filters. Malformed entries (no "=", empty key, duplicate key) fail at Setup. Use this when filter values are fixed for the lifetime of the program; use the Filters wire for values computed upstream; supply both to combine them. When both static_filters is unset AND the Filters wire is empty/disconnected at Run, the op logs a WARN and retrieves without filters.
  Inputs:   Query *string — the natural-language search query.
            Filters *map[string]string — optional request-scoped filters. May be left disconnected when static_filters is set. When connected, the map is merged on top of static_filters every Run (runtime wins on key collision). Empty or nil runtime map is treated as "no runtime filters" and falls through to static_filters. The merged map is installed into ctx via library.WithRetrievalFilters; the Retriever reads it via library.RetrievalFiltersFromContext. Values are strings by convention — Retriever implementations parse numbers, split CSV lists, etc.
  Outputs:  Documents []library.Document — full records ({ID, Content, Score, Metadata}) sorted best-first.
            Texts []string — parallel slice of Documents[i].Content.
  Use this op when at least one filter value is needed; use plain RetrieveOp when no filters are needed. Retriever implementations that don't understand the filter keys must ignore them, not error. Embedding credentials install into ctx alongside the filters; both values coexist.`

// RetrieveWithFiltersOp is the dynamic-filter sibling of RetrieveOp. It
// installs filters from the Filters input wire into ctx via
// WithRetrievalFilters before delegating to the resolved Retriever, so
// Retriever implementations see request-scoped filters without the framework
// or the underlying interface having to enumerate them.
type RetrieveWithFiltersOp struct {
	Query     *string            `dag:"input"`
	Filters   *map[string]string `dag:"input"`
	Documents []Document         `dag:"output"`
	Texts     []string           `dag:"output"`

	k                 int
	retrieverID       string
	credRef           string
	factoryID         string
	factoryTimeout    time.Duration
	factoryTimeoutSet bool
	embedTimeout      time.Duration
	staticFilters     map[string]string
	retriever         Retriever
}

// parseStaticFilters parses a comma-separated key=value list into a map.
// Whitespace around keys, values, and separators is trimmed. Empty input
// returns a nil map. Malformed entries (no "=", empty key, duplicate key)
// return an error. Mirrors the *map[string]string parse path in
// AIComputeOp.parseResponse.
func parseStaticFilters(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	m := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.IndexByte(pair, '=')
		if idx < 0 {
			return nil, fmt.Errorf("expected key=value pair, got %q", pair)
		}
		key := strings.TrimSpace(pair[:idx])
		value := strings.TrimSpace(pair[idx+1:])
		if key == "" {
			return nil, fmt.Errorf("empty key in pair %q", pair)
		}
		if _, dup := m[key]; dup {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		m[key] = value
	}
	if len(m) == 0 {
		return nil, nil
	}
	return m, nil
}

func (op *RetrieveWithFiltersOp) Setup(params *config.Params) error {
	op.k = 5
	if s := params.GetString("k", ""); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("RetrieveWithFiltersOp: param k = %q: %w", s, err)
		}
		if n <= 0 {
			return fmt.Errorf("RetrieveWithFiltersOp: param k must be positive, got %d", n)
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
			return fmt.Errorf("RetrieveWithFiltersOp: param api_factory_timeout_ms = %q: %w", s, err)
		}
		op.factoryTimeout = time.Duration(n) * time.Millisecond
		op.factoryTimeoutSet = true
	}
	if s := params.GetString("embed_timeout_ms", ""); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("RetrieveWithFiltersOp: param embed_timeout_ms = %q: %w", s, err)
		}
		if n < 0 {
			return fmt.Errorf("RetrieveWithFiltersOp: param embed_timeout_ms must be non-negative, got %d", n)
		}
		op.embedTimeout = time.Duration(n) * time.Millisecond
	}
	if raw := params.GetString("static_filters", ""); raw != "" {
		sf, err := parseStaticFilters(raw)
		if err != nil {
			return fmt.Errorf("RetrieveWithFiltersOp: param static_filters = %q: %w", raw, err)
		}
		op.staticFilters = sf
	}
	r, err := resolveRetriever(op.retrieverID)
	if err != nil {
		return fmt.Errorf("RetrieveWithFiltersOp: %w", err)
	}
	op.retriever = r
	return nil
}

func (op *RetrieveWithFiltersOp) Reset() error { return nil }

func (op *RetrieveWithFiltersOp) Run(ctx context.Context) error {
	// Start from a fresh copy of the static map so user code can't mutate
	// our stored copy via the ctx-installed map (Retrievers receive the same
	// reference we hand to WithRetrievalFilters).
	merged := make(map[string]string, len(op.staticFilters))
	for k, v := range op.staticFilters {
		merged[k] = v
	}
	// Runtime wire wins on key collision. op.Filters may be nil (wire
	// disconnected) when the caller is using static_filters only; the
	// double-nil-check handles the *map[string]string Setup wiring.
	var runtime map[string]string
	if op.Filters != nil {
		runtime = *op.Filters
	}
	for k, v := range runtime {
		merged[k] = v
	}
	slog.DebugContext(ctx, "RetrieveWithFiltersOp.run", "run_id", dagor.RunID(ctx), "k", op.k, "retriever_id", op.retrieverID, "filter_count", len(merged), "static_count", len(op.staticFilters), "runtime_count", len(runtime))

	if len(merged) == 0 {
		slog.WarnContext(ctx, "WARNING: RetrieveWithFiltersOp has no filters (Filters wire empty/disconnected and static_filters unset); retrieving without filters. If this is intentional, use RetrieveOp instead.", "run_id", dagor.RunID(ctx), "retriever_id", op.retrieverID)
	}

	ctx = WithRetrievalFilters(ctx, merged)
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
		return fmt.Errorf("RetrieveWithFiltersOp: retrieve: %w", err)
	}
	op.Documents = docs
	op.Texts = make([]string, len(docs))
	for i, d := range docs {
		op.Texts[i] = d.Content
	}
	slog.InfoContext(ctx, "RetrieveWithFiltersOp.retrieved", "run_id", dagor.RunID(ctx), "count", len(docs))
	return nil
}

func init() {
	operator.RegisterOp[RetrieveWithFiltersOp]()
}
