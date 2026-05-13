package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wwz16/dagor/config"
)

// captureSlog redirects the default slog logger to a buffer for the duration
// of a test and restores it on cleanup. Use this to assert on warnings the
// library emits via slog.WarnContext / slog.InfoContext.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// stubRetriever returns a fixed slice of documents and records calls.
type stubRetriever struct {
	mu    sync.Mutex
	docs  []Document
	calls []stubCall
	err   error
}

type stubCall struct {
	query           string
	k               int
	filters         map[string]string
	embedCreds      EmbeddingCredentials
	embedCredsFound bool
}

func (s *stubRetriever) Retrieve(ctx context.Context, query string, k int) ([]Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	filters, _ := RetrievalFiltersFromContext(ctx)
	creds, found := ctx.Value(embeddingCredsKey{}).(EmbeddingCredentials)
	s.calls = append(s.calls, stubCall{
		query:           query,
		k:               k,
		filters:         filters,
		embedCreds:      creds,
		embedCredsFound: found,
	})
	if s.err != nil {
		return nil, s.err
	}
	if k > len(s.docs) {
		k = len(s.docs)
	}
	out := make([]Document, k)
	copy(out, s.docs[:k])
	return out, nil
}

func (s *stubRetriever) snapshot() []stubCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stubCall, len(s.calls))
	copy(out, s.calls)
	return out
}

// withRetrievers swaps the registry to a clean slate for the duration of a
// test, restoring on cleanup. Mirrors withFactories in ai_factory_test.go.
func withRetrievers(t *testing.T, def Retriever) {
	t.Helper()
	retrieverMu.Lock()
	prevDefault := defaultRetriever
	prevRegistry := retrieverRegistry
	defaultRetriever = def
	retrieverRegistry = map[string]Retriever{}
	retrieverMu.Unlock()
	t.Cleanup(func() {
		retrieverMu.Lock()
		defaultRetriever = prevDefault
		retrieverRegistry = prevRegistry
		retrieverMu.Unlock()
	})
}

func mustRetrieveParams(t *testing.T, kv map[string]string) *config.Params {
	t.Helper()
	raw, err := json.Marshal(kv)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	p, err := config.NewFromRaw(raw)
	if err != nil {
		t.Fatalf("new params: %v", err)
	}
	return p
}

func TestResolveRetriever_DefaultWhenNoIDSet(t *testing.T) {
	def := &stubRetriever{}
	withRetrievers(t, def)
	got, err := resolveRetriever("")
	if err != nil {
		t.Fatalf("resolveRetriever(\"\"): %v", err)
	}
	if got != def {
		t.Fatalf("resolveRetriever(\"\") = %p, want default %p", got, def)
	}
}

func TestResolveRetriever_RegisteredIDWins(t *testing.T) {
	def := &stubRetriever{}
	tenant := &stubRetriever{}
	withRetrievers(t, def)
	RegisterRetriever("kb-a", tenant)
	got, err := resolveRetriever("kb-a")
	if err != nil {
		t.Fatalf("resolveRetriever(\"kb-a\"): %v", err)
	}
	if got != tenant {
		t.Fatalf("resolveRetriever(\"kb-a\") = %p, want tenant %p", got, tenant)
	}
}

func TestResolveRetriever_UnknownIDFallsBackToDefault(t *testing.T) {
	def := &stubRetriever{}
	withRetrievers(t, def)
	got, err := resolveRetriever("nope")
	if err != nil {
		t.Fatalf("resolveRetriever(\"nope\"): %v", err)
	}
	if got != def {
		t.Fatalf("resolveRetriever(\"nope\") = %p, want default %p", got, def)
	}
}

func TestResolveRetriever_NoDefaultNoIDErrors(t *testing.T) {
	withRetrievers(t, nil)
	_, err := resolveRetriever("")
	if err == nil {
		t.Fatalf("resolveRetriever(\"\") with no default: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no default Retriever") {
		t.Fatalf("error %q does not mention missing default", err)
	}
}

func TestResolveRetriever_NoDefaultUnknownIDErrors(t *testing.T) {
	withRetrievers(t, nil)
	_, err := resolveRetriever("missing")
	if err == nil {
		t.Fatalf("resolveRetriever(\"missing\") with no default: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error %q does not mention the id", err)
	}
}

func TestRegisterRetriever_NilDeregisters(t *testing.T) {
	def := &stubRetriever{}
	tenant := &stubRetriever{}
	withRetrievers(t, def)
	RegisterRetriever("kb-a", tenant)
	RegisterRetriever("kb-a", nil)
	got, err := resolveRetriever("kb-a")
	if err != nil {
		t.Fatalf("resolveRetriever after deregister: %v", err)
	}
	if got != def {
		t.Fatalf("resolveRetriever(\"kb-a\") after deregister = %p, want default %p", got, def)
	}
}

func TestRetrieveOp_SetupFailsWithoutRetriever(t *testing.T) {
	withRetrievers(t, nil)
	op := &RetrieveOp{}
	err := op.Setup(mustRetrieveParams(t, nil))
	if err == nil {
		t.Fatalf("Setup: expected error when no Retriever is registered")
	}
}

func TestRetrieveOp_SetupParsesK(t *testing.T) {
	withRetrievers(t, &stubRetriever{})
	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"k": "7"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if op.k != 7 {
		t.Fatalf("op.k = %d, want 7", op.k)
	}
}

func TestRetrieveOp_SetupDefaultK(t *testing.T) {
	withRetrievers(t, &stubRetriever{})
	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if op.k != 5 {
		t.Fatalf("default op.k = %d, want 5", op.k)
	}
}

func TestRetrieveOp_SetupRejectsNonPositiveK(t *testing.T) {
	withRetrievers(t, &stubRetriever{})
	op := &RetrieveOp{}
	err := op.Setup(mustRetrieveParams(t, map[string]string{"k": "0"}))
	if err == nil {
		t.Fatalf("Setup with k=0: expected error")
	}
}

func TestRetrieveOp_SelectsRegisteredRetrieverByID(t *testing.T) {
	def := &stubRetriever{docs: []Document{{ID: "def", Content: "from default"}}}
	tenant := &stubRetriever{docs: []Document{{ID: "ten", Content: "from tenant"}}}
	withRetrievers(t, def)
	RegisterRetriever("kb-a", tenant)

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"retriever_id": "kb-a"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(op.Documents) != 1 || op.Documents[0].ID != "ten" {
		t.Fatalf("Documents = %+v, want one doc with ID=ten", op.Documents)
	}
	if len(def.snapshot()) != 0 {
		t.Fatalf("default retriever was called; should have been bypassed for kb-a")
	}
}

func TestRetrieveOp_RunPopulatesDocumentsAndAlignedTexts(t *testing.T) {
	docs := []Document{
		{ID: "a", Content: "alpha body", Score: 0.9},
		{ID: "b", Content: "beta body", Score: 0.7},
		{ID: "c", Content: "gamma body", Score: 0.5},
	}
	withRetrievers(t, &stubRetriever{docs: docs})

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"k": "3"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "anything"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(op.Documents) != 3 || len(op.Texts) != 3 {
		t.Fatalf("len(Documents)=%d len(Texts)=%d, want 3 each", len(op.Documents), len(op.Texts))
	}
	for i, d := range op.Documents {
		if op.Texts[i] != d.Content {
			t.Fatalf("Texts[%d] = %q, want %q (parallel to Documents)", i, op.Texts[i], d.Content)
		}
	}
}

func TestRetrieveOp_RunPropagatesError(t *testing.T) {
	want := errors.New("backend down")
	withRetrievers(t, &stubRetriever{err: want})

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "anything"
	op.Query = &q
	err := op.Run(context.Background())
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("Run err = %v, want wrapping %v", err, want)
	}
}

func TestRetrieveOp_KForwardedToRetriever(t *testing.T) {
	r := &stubRetriever{docs: []Document{
		{ID: "a", Content: "1"}, {ID: "b", Content: "2"}, {ID: "c", Content: "3"},
	}}
	withRetrievers(t, r)

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"k": "2"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "anything"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if len(calls) != 1 || calls[0].k != 2 {
		t.Fatalf("retriever calls = %+v, want one call with k=2", calls)
	}
}

// ─── Document Metadata ────────────────────────────────────────────────────

func TestRetrieveOp_MetadataRoundTripsThroughOp(t *testing.T) {
	docs := []Document{
		{
			ID:      "a",
			Content: "alpha body",
			Score:   0.9,
			Metadata: map[string]any{
				"source_url": "https://example.com/a",
				"highlights": []string{"alpha", "body"},
				"updated_at": time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
				"acl":        []string{"public"},
			},
		},
		{
			ID:       "b",
			Content:  "beta body",
			Score:    0.7,
			Metadata: nil,
		},
	}
	withRetrievers(t, &stubRetriever{docs: docs})

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"k": "2"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "anything"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(op.Documents) != 2 {
		t.Fatalf("len(Documents) = %d, want 2", len(op.Documents))
	}
	got := op.Documents[0].Metadata
	if got["source_url"] != "https://example.com/a" {
		t.Fatalf("Metadata[source_url] = %v, want example.com/a", got["source_url"])
	}
	hl, ok := got["highlights"].([]string)
	if !ok || len(hl) != 2 || hl[0] != "alpha" {
		t.Fatalf("Metadata[highlights] = %v, want []string{alpha, body}", got["highlights"])
	}
	if got["acl"] == nil {
		t.Fatalf("Metadata[acl] not preserved")
	}
	if op.Documents[1].Metadata != nil {
		t.Fatalf("Documents[1].Metadata = %v, want nil (Retriever returned nil)", op.Documents[1].Metadata)
	}
}

// ─── Filter convention ─────────────────────────────────────────────────────

func TestWithRetrievalFilters_RoundTrip(t *testing.T) {
	filters := map[string]string{"tenant": "acme", "category": "billing"}
	ctx := WithRetrievalFilters(context.Background(), filters)
	got, ok := RetrievalFiltersFromContext(ctx)
	if !ok {
		t.Fatalf("RetrievalFiltersFromContext ok=false, want true")
	}
	if got["tenant"] != "acme" || got["category"] != "billing" {
		t.Fatalf("filters round-trip mismatch: %+v", got)
	}
}

func TestRetrievalFiltersFromContext_AbsentReturnsFalse(t *testing.T) {
	_, ok := RetrievalFiltersFromContext(context.Background())
	if ok {
		t.Fatalf("RetrievalFiltersFromContext ok=true on plain ctx, want false")
	}
}

// TestRetrievalFiltersFromContext_ReturnsDefensiveCopy asserts that mutating
// the returned map does not affect the map stored on ctx — a subsequent call
// must still see the original installed filters intact. Guards the defensive-
// copy contract documented on RetrievalFiltersFromContext.
func TestRetrievalFiltersFromContext_ReturnsDefensiveCopy(t *testing.T) {
	installed := map[string]string{"tenant": "acme", "category": "billing"}
	ctx := WithRetrievalFilters(context.Background(), installed)

	first, ok := RetrievalFiltersFromContext(ctx)
	if !ok {
		t.Fatalf("first RetrievalFiltersFromContext ok=false, want true")
	}
	// Mutate the returned map every which way: overwrite, add, delete.
	first["tenant"] = "globex"
	first["injected"] = "bad"
	delete(first, "category")

	second, ok := RetrievalFiltersFromContext(ctx)
	if !ok {
		t.Fatalf("second RetrievalFiltersFromContext ok=false, want true")
	}
	if second["tenant"] != "acme" {
		t.Fatalf("second[tenant] = %q, want %q (mutation of first call leaked into ctx)", second["tenant"], "acme")
	}
	if second["category"] != "billing" {
		t.Fatalf("second[category] = %q, want %q (delete of first call leaked into ctx)", second["category"], "billing")
	}
	if _, present := second["injected"]; present {
		t.Fatalf("second saw injected key from prior mutation; defensive-copy contract violated")
	}
	if len(second) != 2 {
		t.Fatalf("second has %d keys, want 2 (original installed set)", len(second))
	}
}

func TestWithRetrievalFilters_EmptyMapNoop(t *testing.T) {
	base := context.Background()
	if got := WithRetrievalFilters(base, nil); got != base {
		t.Fatalf("nil filters: ctx changed, want unchanged")
	}
	if got := WithRetrievalFilters(base, map[string]string{}); got != base {
		t.Fatalf("empty filters: ctx changed, want unchanged")
	}
}

// ─── RetrieveWithFiltersOp ────────────────────────────────────────────────

func TestRetrieveWithFiltersOp_InstallsFiltersInCtx(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	filters := map[string]string{"tenant": "acme", "category": "billing"}
	op.Filters = &filters
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if len(calls) != 1 {
		t.Fatalf("retriever calls = %d, want 1", len(calls))
	}
	if calls[0].filters["tenant"] != "acme" || calls[0].filters["category"] != "billing" {
		t.Fatalf("retriever saw filters %+v, want acme/billing", calls[0].filters)
	}
}

func TestRetrieveWithFiltersOp_NilFiltersDoesNotInstall(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	var nilMap map[string]string
	op.Filters = &nilMap
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if len(calls) != 1 {
		t.Fatalf("retriever calls = %d, want 1", len(calls))
	}
	if calls[0].filters != nil {
		t.Fatalf("retriever saw filters %+v with nil input, want nil", calls[0].filters)
	}
}

func TestRetrieveWithFiltersOp_WarnsOnEmptyFilters(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)
	buf := captureSlog(t)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	empty := map[string]string{}
	op.Filters = &empty
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "RetrieveWithFiltersOp has no filters") {
		t.Fatalf("expected warning naming RetrieveWithFiltersOp and empty Filters; got logs:\n%s", logs)
	}
	if !strings.Contains(logs, "level=WARN") {
		t.Fatalf("expected WARN-level log; got logs:\n%s", logs)
	}
	// Behavior unchanged: retriever was still called and saw no filters.
	calls := r.snapshot()
	if len(calls) != 1 {
		t.Fatalf("retriever calls = %d, want 1", len(calls))
	}
	if calls[0].filters != nil {
		t.Fatalf("retriever saw filters %+v with empty input, want nil", calls[0].filters)
	}
}

func TestRetrieveWithFiltersOp_WarnsOnNilFiltersMap(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)
	buf := captureSlog(t)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	var nilMap map[string]string
	op.Filters = &nilMap
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(buf.String(), "RetrieveWithFiltersOp has no filters") {
		t.Fatalf("expected warning for nil-map filters; got logs:\n%s", buf.String())
	}
}

func TestRetrieveWithFiltersOp_NoWarnWhenFiltersPresent(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)
	buf := captureSlog(t)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	filters := map[string]string{"tenant": "acme"}
	op.Filters = &filters
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if strings.Contains(buf.String(), "has no filters") {
		t.Fatalf("did not expect empty-filters warning when filters are present; got logs:\n%s", buf.String())
	}
}

func TestRetrieveWithFiltersOp_PopulatesDocumentsAndTexts(t *testing.T) {
	docs := []Document{
		{ID: "a", Content: "alpha body", Score: 0.9},
		{ID: "b", Content: "beta body", Score: 0.7},
	}
	withRetrievers(t, &stubRetriever{docs: docs})

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"k": "2"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "anything"
	op.Query = &q
	filters := map[string]string{"x": "y"}
	op.Filters = &filters
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(op.Documents) != 2 || len(op.Texts) != 2 {
		t.Fatalf("len(Documents)=%d len(Texts)=%d, want 2 each", len(op.Documents), len(op.Texts))
	}
	for i, d := range op.Documents {
		if op.Texts[i] != d.Content {
			t.Fatalf("Texts[%d] = %q, want %q", i, op.Texts[i], d.Content)
		}
	}
}

func TestRetrieveWithFiltersOp_SetupFailsWithoutRetriever(t *testing.T) {
	withRetrievers(t, nil)
	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err == nil {
		t.Fatalf("Setup: expected error when no Retriever is registered")
	}
}

func TestRetrieveWithFiltersOp_RespectsRetrieverID(t *testing.T) {
	def := &stubRetriever{docs: []Document{{ID: "def", Content: "from default"}}}
	tenant := &stubRetriever{docs: []Document{{ID: "ten", Content: "from tenant"}}}
	withRetrievers(t, def)
	RegisterRetriever("kb-a", tenant)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"retriever_id": "kb-a"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	filters := map[string]string{"x": "y"}
	op.Filters = &filters
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(op.Documents) != 1 || op.Documents[0].ID != "ten" {
		t.Fatalf("Documents = %+v, want one doc with ID=ten", op.Documents)
	}
	if len(def.snapshot()) != 0 {
		t.Fatalf("default retriever was called; should have been bypassed for kb-a")
	}
}

// ─── Embedding credentials ctx install ────────────────────────────────────

func TestRetrieveOp_InstallsEmbeddingCredentials(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{
		"credential_ref":         "vault://prod/voyage",
		"client_factory_id":      "tenant-a",
		"api_factory_timeout_ms": "1500",
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if len(calls) != 1 {
		t.Fatalf("retriever calls = %d, want 1", len(calls))
	}
	got := calls[0].embedCreds
	want := EmbeddingCredentials{
		Ref:            "vault://prod/voyage",
		FactoryID:      "tenant-a",
		FactoryTimeout: 1500 * time.Millisecond,
	}
	if got != want {
		t.Fatalf("embedding creds on ctx = %+v, want %+v", got, want)
	}
}

func TestRetrieveOp_DefaultEmbeddingFactoryTimeoutAppliedWhenCredRefSet(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	// When the user sets credential_ref but leaves api_factory_timeout_ms
	// unset, the ctx-installed credentials must carry the 30s default so
	// downstream factories have a sane bound on the credential lookup.
	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"credential_ref": "vault://x"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if got := calls[0].embedCreds.FactoryTimeout; got != 30*time.Second {
		t.Fatalf("default FactoryTimeout = %v, want 30s", got)
	}
}

func TestRetrieveOp_ZeroFactoryTimeoutDisablesDeadline(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"api_factory_timeout_ms": "0"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	// Explicit-zero must be installed on ctx (distinct from "user said
	// nothing"), so downstream code can tell "user disabled the deadline"
	// apart from "no credentials supplied".
	if !calls[0].embedCredsFound {
		t.Fatalf("embedding creds not installed on ctx; want installed with FactoryTimeout=0")
	}
	if got := calls[0].embedCreds.FactoryTimeout; got != 0 {
		t.Fatalf("FactoryTimeout = %v, want 0 (disabled)", got)
	}
}

// TestRetrieveOp_UnsetFactoryTimeoutNotInstalled is the sibling case to
// TestRetrieveOp_ZeroFactoryTimeoutDisablesDeadline: when the user does not
// pass api_factory_timeout_ms at all (and no other credential param), no
// EmbeddingCredentials value must be installed on ctx. This is what makes the
// "explicit zero installs" assertion above non-vacuous.
func TestRetrieveOp_UnsetFactoryTimeoutNotInstalled(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if calls[0].embedCredsFound {
		t.Fatalf("embedding creds installed on ctx = %+v, want not installed when no params set", calls[0].embedCreds)
	}
}

func TestRetrieveOp_SetupRejectsMalformedFactoryTimeout(t *testing.T) {
	withRetrievers(t, &stubRetriever{})
	op := &RetrieveOp{}
	err := op.Setup(mustRetrieveParams(t, map[string]string{"api_factory_timeout_ms": "soon"}))
	if err == nil {
		t.Fatalf("Setup with malformed timeout: expected error, got nil")
	}
}

// TestRetrieveOp_DoesNotInstallEmbeddingCredentialsWhenUnset covers the
// BM25-style Retriever path: when the vertex sets no credential params,
// RetrieveOp must NOT touch the embedding-credentials slot on ctx. Any creds
// the caller installed upstream pass through unchanged; a downstream
// non-embedding Retriever sees the ctx default.
func TestRetrieveOp_DoesNotInstallEmbeddingCredentialsWhenUnset(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if len(calls) != 1 {
		t.Fatalf("retriever calls = %d, want 1", len(calls))
	}
	if calls[0].embedCredsFound {
		t.Fatalf("embedding creds installed on ctx = %+v, want not installed when params unset", calls[0].embedCreds)
	}
}

// TestRetrieveOp_PreservesUpstreamEmbeddingCredentialsWhenUnset covers the
// case where a caller pre-installs credentials on ctx before engine.Run; a
// RetrieveOp with no credential params must leave that value alone.
func TestRetrieveOp_PreservesUpstreamEmbeddingCredentialsWhenUnset(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	upstream := EmbeddingCredentials{Ref: "vault://upstream", FactoryID: "outer", FactoryTimeout: 5 * time.Second}
	ctx := WithEmbeddingCredentials(context.Background(), upstream)
	if err := op.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if calls[0].embedCreds != upstream {
		t.Fatalf("embedding creds on ctx = %+v, want preserved upstream %+v", calls[0].embedCreds, upstream)
	}
}

// TestRetrieveOp_InstallsEmbeddingCredentialsWhenOnlyCredRefSet is the
// counter-test: setting just credential_ref must trigger the full install,
// so we don't silently drop a partially-specified credential intent.
func TestRetrieveOp_InstallsEmbeddingCredentialsWhenOnlyCredRefSet(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"credential_ref": "vault://only-ref"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if calls[0].embedCreds.Ref != "vault://only-ref" {
		t.Fatalf("embedding creds Ref = %q, want vault://only-ref", calls[0].embedCreds.Ref)
	}
}

// TestRetrieveWithFiltersOp_DoesNotInstallEmbeddingCredentialsWhenUnset is
// the sibling test for the with-filters op: when no credential params are
// set, the embedding-credentials slot on ctx is untouched even though
// retrieval filters are installed.
func TestRetrieveWithFiltersOp_DoesNotInstallEmbeddingCredentialsWhenUnset(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"static_filters": "tenant=acme"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if len(calls) != 1 {
		t.Fatalf("retriever calls = %d, want 1", len(calls))
	}
	if calls[0].embedCredsFound {
		t.Fatalf("embedding creds installed on ctx = %+v, want not installed when params unset", calls[0].embedCreds)
	}
	if calls[0].filters["tenant"] != "acme" {
		t.Fatalf("filters on ctx = %+v, want tenant=acme (filters install still runs)", calls[0].filters)
	}
}

// TestRetrieveWithFiltersOp_ZeroFactoryTimeoutDisablesDeadline mirrors the
// RetrieveOp test: an explicit api_factory_timeout_ms=0 must install creds on
// ctx with FactoryTimeout=0, distinguishable from the "user said nothing" case.
func TestRetrieveWithFiltersOp_ZeroFactoryTimeoutDisablesDeadline(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{
		"api_factory_timeout_ms": "0",
		"static_filters":         "tenant=acme",
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if !calls[0].embedCredsFound {
		t.Fatalf("embedding creds not installed on ctx; want installed with FactoryTimeout=0")
	}
	if got := calls[0].embedCreds.FactoryTimeout; got != 0 {
		t.Fatalf("FactoryTimeout = %v, want 0 (disabled)", got)
	}
}

// TestRetrieveWithFiltersOp_UnsetFactoryTimeoutNotInstalled is the sibling
// "unset" case for the with-filters op: when no credential param is set, no
// EmbeddingCredentials value must be installed on ctx (even though filters
// install always runs).
func TestRetrieveWithFiltersOp_UnsetFactoryTimeoutNotInstalled(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"static_filters": "tenant=acme"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if calls[0].embedCredsFound {
		t.Fatalf("embedding creds installed on ctx = %+v, want not installed when no creds param set", calls[0].embedCreds)
	}
}

func TestRetrieveWithFiltersOp_InstallsBothFiltersAndEmbeddingCredentials(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{
		"credential_ref":         "vault://prod/voyage",
		"client_factory_id":      "tenant-a",
		"api_factory_timeout_ms": "2000",
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	filters := map[string]string{"tenant": "acme", "category": "billing"}
	op.Filters = &filters
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if len(calls) != 1 {
		t.Fatalf("retriever calls = %d, want 1", len(calls))
	}
	if calls[0].filters["tenant"] != "acme" || calls[0].filters["category"] != "billing" {
		t.Fatalf("filters on ctx = %+v, want acme/billing", calls[0].filters)
	}
	wantCreds := EmbeddingCredentials{
		Ref:            "vault://prod/voyage",
		FactoryID:      "tenant-a",
		FactoryTimeout: 2000 * time.Millisecond,
	}
	if calls[0].embedCreds != wantCreds {
		t.Fatalf("embedding creds on ctx = %+v, want %+v", calls[0].embedCreds, wantCreds)
	}
}

// blockingRetriever blocks Retrieve until ctx is canceled, then returns
// ctx.Err() so embed_timeout_ms wiring can be observed end-to-end.
type blockingRetriever struct {
	started chan struct{}
	once    sync.Once
}

func (b *blockingRetriever) Retrieve(ctx context.Context, _ string, _ int) ([]Document, error) {
	b.once.Do(func() {
		if b.started != nil {
			close(b.started)
		}
	})
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRetrieveOp_EmbedTimeoutBoundsRetrieveCall(t *testing.T) {
	br := &blockingRetriever{started: make(chan struct{})}
	withRetrievers(t, br)

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"embed_timeout_ms": "20"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "anything"
	op.Query = &q

	start := time.Now()
	err := op.Run(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("Run with embed_timeout_ms=20: expected error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run err = %v, want wrapping context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Run took %v; deadline should have fired well before this", elapsed)
	}
}

func TestRetrieveOp_ZeroEmbedTimeoutImposesNoDeadline(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"embed_timeout_ms": "0"})); err != nil {
		t.Fatalf("Setup with embed_timeout_ms=0: %v", err)
	}
	if op.embedTimeout != 0 {
		t.Fatalf("op.embedTimeout = %v, want 0 (disabled)", op.embedTimeout)
	}
	q := "anything"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRetrieveOp_UnsetEmbedTimeoutImposesNoDeadline(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup with embed_timeout_ms unset: %v", err)
	}
	if op.embedTimeout != 0 {
		t.Fatalf("op.embedTimeout = %v, want 0 (unset)", op.embedTimeout)
	}
}

func TestRetrieveOp_SetupRejectsMalformedEmbedTimeout(t *testing.T) {
	withRetrievers(t, &stubRetriever{})
	op := &RetrieveOp{}
	err := op.Setup(mustRetrieveParams(t, map[string]string{"embed_timeout_ms": "soon"}))
	if err == nil {
		t.Fatalf("Setup with malformed embed_timeout_ms: expected error, got nil")
	}
}

func TestRetrieveOp_SetupRejectsNegativeEmbedTimeout(t *testing.T) {
	withRetrievers(t, &stubRetriever{})
	op := &RetrieveOp{}
	err := op.Setup(mustRetrieveParams(t, map[string]string{"embed_timeout_ms": "-1"}))
	if err == nil {
		t.Fatalf("Setup with negative embed_timeout_ms: expected error, got nil")
	}
}

func TestRetrieveWithFiltersOp_EmbedTimeoutBoundsRetrieveCall(t *testing.T) {
	br := &blockingRetriever{started: make(chan struct{})}
	withRetrievers(t, br)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{"embed_timeout_ms": "20"})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "anything"
	op.Query = &q
	filters := map[string]string{"tenant": "acme"}
	op.Filters = &filters

	start := time.Now()
	err := op.Run(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("Run with embed_timeout_ms=20: expected error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run err = %v, want wrapping context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Run took %v; deadline should have fired well before this", elapsed)
	}
}

func TestRetrieveOp_ConcurrentRetrieveIsSafe(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "x", Content: "x"}}}
	withRetrievers(t, r)

	const N = 20
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			op := &RetrieveOp{}
			if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
				t.Errorf("Setup: %v", err)
				return
			}
			q := "q"
			op.Query = &q
			if err := op.Run(context.Background()); err != nil {
				t.Errorf("Run: %v", err)
			}
		}()
	}
	wg.Wait()
	if len(r.snapshot()) != N {
		t.Fatalf("retriever received %d calls, want %d", len(r.snapshot()), N)
	}
}

// ─── static_filters param ─────────────────────────────────────────────────

func TestRetrieveWithFiltersOp_StaticFiltersOnly_NoRuntimeWire(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)
	buf := captureSlog(t)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{
		"static_filters": "tenant=acme,locale=en",
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	// Filters wire intentionally left disconnected (op.Filters == nil).
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if len(calls) != 1 {
		t.Fatalf("retriever calls = %d, want 1", len(calls))
	}
	if calls[0].filters["tenant"] != "acme" || calls[0].filters["locale"] != "en" {
		t.Fatalf("retriever saw filters %+v, want tenant=acme,locale=en", calls[0].filters)
	}
	if len(calls[0].filters) != 2 {
		t.Fatalf("retriever saw %d filters, want exactly 2", len(calls[0].filters))
	}
	if strings.Contains(buf.String(), "has no filters") {
		t.Fatalf("did not expect empty-filters warning when static_filters is set; got logs:\n%s", buf.String())
	}
}

func TestRetrieveWithFiltersOp_StaticPlusRuntime_RuntimeOverridesOnCollision(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{
		"static_filters": "tenant=acme,locale=en",
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	// runtime overrides "tenant" and adds "category"; "locale" stays static.
	runtime := map[string]string{"tenant": "globex", "category": "billing"}
	op.Filters = &runtime
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if len(calls) != 1 {
		t.Fatalf("retriever calls = %d, want 1", len(calls))
	}
	got := calls[0].filters
	if got["tenant"] != "globex" {
		t.Fatalf("merged[tenant] = %q, want %q (runtime wins on collision)", got["tenant"], "globex")
	}
	if got["locale"] != "en" {
		t.Fatalf("merged[locale] = %q, want %q (static survives when not overridden)", got["locale"], "en")
	}
	if got["category"] != "billing" {
		t.Fatalf("merged[category] = %q, want %q (runtime-only key)", got["category"], "billing")
	}
	if len(got) != 3 {
		t.Fatalf("merged has %d keys, want 3", len(got))
	}
}

func TestRetrieveWithFiltersOp_StaticFiltersWhitespaceAndSpacing(t *testing.T) {
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{
		"static_filters": " tenant = acme , locale = en ",
	})); err != nil {
		t.Fatalf("Setup with padded static_filters: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if calls[0].filters["tenant"] != "acme" || calls[0].filters["locale"] != "en" {
		t.Fatalf("retriever saw filters %+v, want trimmed tenant=acme,locale=en", calls[0].filters)
	}
}

func TestRetrieveWithFiltersOp_StaticFiltersMutationIsolation(t *testing.T) {
	// Run twice; first Run mutates the map a Retriever might keep a reference
	// to. Second Run must still see the parsed static_filters intact — the op
	// owes Retrievers a fresh map per call.
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{
		"static_filters": "tenant=acme",
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run #1: %v", err)
	}
	// Mutate the map the retriever saw.
	calls := r.snapshot()
	calls[0].filters["tenant"] = "mutated"
	calls[0].filters["injected"] = "bad"
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run #2: %v", err)
	}
	calls2 := r.snapshot()
	if calls2[1].filters["tenant"] != "acme" {
		t.Fatalf("Run #2 tenant = %q, want %q (mutation from Run #1 leaked into stored staticFilters)", calls2[1].filters["tenant"], "acme")
	}
	if _, ok := calls2[1].filters["injected"]; ok {
		t.Fatalf("Run #2 saw injected key from prior mutation; staticFilters not isolated per call")
	}
}

func TestRetrieveWithFiltersOp_StaticFiltersMalformed_NoEquals(t *testing.T) {
	withRetrievers(t, &stubRetriever{})
	op := &RetrieveWithFiltersOp{}
	err := op.Setup(mustRetrieveParams(t, map[string]string{
		"static_filters": "tenant",
	}))
	if err == nil {
		t.Fatalf("Setup with malformed static_filters (no '='): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "static_filters") {
		t.Fatalf("error %q does not mention static_filters", err)
	}
}

func TestRetrieveWithFiltersOp_StaticFiltersMalformed_EmptyKey(t *testing.T) {
	withRetrievers(t, &stubRetriever{})
	op := &RetrieveWithFiltersOp{}
	err := op.Setup(mustRetrieveParams(t, map[string]string{
		"static_filters": "=value",
	}))
	if err == nil {
		t.Fatalf("Setup with empty key in static_filters: expected error, got nil")
	}
}

func TestRetrieveWithFiltersOp_StaticFiltersEmptyValueAccepted(t *testing.T) {
	// "key=" with empty value is well-formed by convention — the Retriever
	// can interpret an empty string however it likes.
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)
	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{
		"static_filters": "tenant=",
	})); err != nil {
		t.Fatalf("Setup with empty value (tenant=): %v", err)
	}
	q := "hello"
	op.Query = &q
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := r.snapshot()
	if v, ok := calls[0].filters["tenant"]; !ok || v != "" {
		t.Fatalf("filters[tenant] = (%q,%v), want (\"\",true)", v, ok)
	}
}

func TestRetrieveWithFiltersOp_StaticFiltersMalformed_DuplicateKey(t *testing.T) {
	withRetrievers(t, &stubRetriever{})
	op := &RetrieveWithFiltersOp{}
	err := op.Setup(mustRetrieveParams(t, map[string]string{
		"static_filters": "tenant=a,tenant=b",
	}))
	if err == nil {
		t.Fatalf("Setup with duplicate key in static_filters: expected error, got nil")
	}
}

func TestRetrieveWithFiltersOp_StaticEmpty_RuntimeEmpty_WarnsAndRetrieves(t *testing.T) {
	// U1 warning regression: when BOTH static and runtime are empty, the
	// warning still fires (no false positive when static is set).
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)
	buf := captureSlog(t)

	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, nil)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	empty := map[string]string{}
	op.Filters = &empty
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(buf.String(), "has no filters") {
		t.Fatalf("expected warning when both static and runtime filters are empty; got logs:\n%s", buf.String())
	}
}

func TestRetrieveWithFiltersOp_NoFiltersWireDisconnected_StaticSet_NoPanic(t *testing.T) {
	// Confirms the wire can be left disconnected (op.Filters == nil) when
	// static_filters supplies the values. Pre-U6 this panicked because Run
	// dereferenced op.Filters unconditionally.
	r := &stubRetriever{docs: []Document{{ID: "a", Content: "alpha"}}}
	withRetrievers(t, r)
	op := &RetrieveWithFiltersOp{}
	if err := op.Setup(mustRetrieveParams(t, map[string]string{
		"static_filters": "tenant=acme",
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	q := "hello"
	op.Query = &q
	// op.Filters is the zero value (nil *map[string]string).
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run with disconnected Filters wire: %v", err)
	}
}
