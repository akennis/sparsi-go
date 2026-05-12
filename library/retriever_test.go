package library

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/wwz16/dagor/config"
)

// stubRetriever returns a fixed slice of documents and records calls.
type stubRetriever struct {
	mu    sync.Mutex
	docs  []Document
	calls []stubCall
	err   error
}

type stubCall struct {
	query   string
	k       int
	filters map[string]string
}

func (s *stubRetriever) Retrieve(ctx context.Context, query string, k int) ([]Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	filters, _ := RetrievalFiltersFromContext(ctx)
	s.calls = append(s.calls, stubCall{query: query, k: k, filters: filters})
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
