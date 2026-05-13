package library

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/genai"
)

// recordingEmbeddingFactory captures every Embedder call and returns a nil
// EmbeddingClient. Tests using it must not invoke Embed on the returned
// client; they only verify wiring (which factory was selected, what
// provider/model/ref was passed).
type recordingEmbeddingFactory struct {
	mu    sync.Mutex
	calls []embeddingFactoryCall
	err   error
}

type embeddingFactoryCall struct {
	provider string
	model    string
	ref      string
}

func (f *recordingEmbeddingFactory) Embedder(_ context.Context, provider, model, ref string) (EmbeddingClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, embeddingFactoryCall{provider: provider, model: model, ref: ref})
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

func (f *recordingEmbeddingFactory) snapshot() []embeddingFactoryCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]embeddingFactoryCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// blockingEmbeddingFactory blocks Embedder on the supplied ctx until
// cancellation, then returns ctx.Err(). Used to assert that
// ResolveEmbeddingClient bounds the factory call with FactoryTimeout.
type blockingEmbeddingFactory struct{}

func (blockingEmbeddingFactory) Embedder(ctx context.Context, _, _, _ string) (EmbeddingClient, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// withEmbeddingFactories swaps the default factory and registry to a clean
// slate for the duration of a test, restoring on cleanup. Mirrors
// withFactories in ai_factory_test.go.
func withEmbeddingFactories(t *testing.T, def EmbeddingClientFactory) {
	t.Helper()
	embeddingFactoryMu.Lock()
	prevDefault := defaultEmbeddingFactory
	prevRegistry := embeddingFactoryRegistry
	defaultEmbeddingFactory = def
	embeddingFactoryRegistry = map[string]EmbeddingClientFactory{}
	embeddingFactoryMu.Unlock()
	t.Cleanup(func() {
		embeddingFactoryMu.Lock()
		defaultEmbeddingFactory = prevDefault
		embeddingFactoryRegistry = prevRegistry
		embeddingFactoryMu.Unlock()
	})
}

func TestResolveEmbeddingFactory_DefaultWhenNoIDSet(t *testing.T) {
	def := &recordingEmbeddingFactory{}
	withEmbeddingFactories(t, def)
	if got := resolveEmbeddingFactory(""); got != def {
		t.Fatalf("resolveEmbeddingFactory(\"\") = %p, want default %p", got, def)
	}
}

func TestResolveEmbeddingFactory_RegisteredIDWins(t *testing.T) {
	def := &recordingEmbeddingFactory{}
	tenant := &recordingEmbeddingFactory{}
	withEmbeddingFactories(t, def)
	RegisterEmbeddingClientFactory("tenant-a", tenant)
	if got := resolveEmbeddingFactory("tenant-a"); got != tenant {
		t.Fatalf("resolveEmbeddingFactory(\"tenant-a\") = %p, want tenant %p", got, tenant)
	}
}

func TestResolveEmbeddingFactory_UnknownIDFallsBackToDefault(t *testing.T) {
	def := &recordingEmbeddingFactory{}
	withEmbeddingFactories(t, def)
	if got := resolveEmbeddingFactory("nope"); got != def {
		t.Fatalf("resolveEmbeddingFactory(\"nope\") = %p, want default %p", got, def)
	}
}

func TestSetDefaultEmbeddingClientFactory_NilResetsToEnv(t *testing.T) {
	withEmbeddingFactories(t, &recordingEmbeddingFactory{})
	SetDefaultEmbeddingClientFactory(nil)
	if _, ok := resolveEmbeddingFactory("").(*EnvEmbeddingClientFactory); !ok {
		t.Fatalf("after SetDefaultEmbeddingClientFactory(nil), default = %T, want *EnvEmbeddingClientFactory", resolveEmbeddingFactory(""))
	}
}

func TestRegisterEmbeddingClientFactory_NilDeregisters(t *testing.T) {
	def := &recordingEmbeddingFactory{}
	tenant := &recordingEmbeddingFactory{}
	withEmbeddingFactories(t, def)
	RegisterEmbeddingClientFactory("tenant-a", tenant)
	RegisterEmbeddingClientFactory("tenant-a", nil)
	if got := resolveEmbeddingFactory("tenant-a"); got != def {
		t.Fatalf("after deregister, resolveEmbeddingFactory(\"tenant-a\") = %p, want default %p", got, def)
	}
}

func TestResolveEmbeddingClient_PassesProviderModelRef(t *testing.T) {
	def := &recordingEmbeddingFactory{}
	withEmbeddingFactories(t, def)
	ctx := WithEmbeddingCredentials(context.Background(), EmbeddingCredentials{Ref: "vault://prod/voyage"})
	if _, err := ResolveEmbeddingClient(ctx, "voyage", "voyage-3"); err != nil {
		t.Fatalf("ResolveEmbeddingClient: %v", err)
	}
	calls := def.snapshot()
	if len(calls) != 1 {
		t.Fatalf("factory calls = %d, want 1", len(calls))
	}
	if calls[0].provider != "voyage" || calls[0].model != "voyage-3" || calls[0].ref != "vault://prod/voyage" {
		t.Fatalf("factory call = %+v, want provider=voyage model=voyage-3 ref=vault://prod/voyage", calls[0])
	}
}

func TestResolveEmbeddingClient_RegisteredFactoryWinsOverDefault(t *testing.T) {
	def := &recordingEmbeddingFactory{}
	tenant := &recordingEmbeddingFactory{}
	withEmbeddingFactories(t, def)
	RegisterEmbeddingClientFactory("tenant-a", tenant)
	ctx := WithEmbeddingCredentials(context.Background(), EmbeddingCredentials{Ref: "ref-x", FactoryID: "tenant-a"})
	if _, err := ResolveEmbeddingClient(ctx, "openai", "text-embedding-3-small"); err != nil {
		t.Fatalf("ResolveEmbeddingClient: %v", err)
	}
	if got := tenant.snapshot(); len(got) != 1 || got[0].ref != "ref-x" {
		t.Fatalf("tenant calls = %+v, want one call with ref=ref-x", got)
	}
	if got := def.snapshot(); len(got) != 0 {
		t.Fatalf("default factory calls = %+v, want none", got)
	}
}

func TestResolveEmbeddingClient_FactoryErrorPropagates(t *testing.T) {
	want := errors.New("vault denied")
	withEmbeddingFactories(t, &recordingEmbeddingFactory{err: want})
	_, err := ResolveEmbeddingClient(context.Background(), "openai", "text-embedding-3-small")
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("ResolveEmbeddingClient err = %v, want wrapping %v", err, want)
	}
}

func TestResolveEmbeddingClient_FactoryTimeoutFires(t *testing.T) {
	withEmbeddingFactories(t, blockingEmbeddingFactory{})
	ctx := WithEmbeddingCredentials(context.Background(), EmbeddingCredentials{FactoryTimeout: 50 * time.Millisecond})
	start := time.Now()
	_, err := ResolveEmbeddingClient(ctx, "voyage", "voyage-3")
	elapsed := time.Since(start)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ResolveEmbeddingClient err = %v, want wrapping context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("ResolveEmbeddingClient took %v, expected ~50ms", elapsed)
	}
}

func TestResolveEmbeddingClient_ZeroTimeoutDisablesDeadline(t *testing.T) {
	// With FactoryTimeout=0 and a fast (recording) factory, ResolveEmbeddingClient
	// must reach the factory and return its (nil) result rather than producing a
	// deadline error.
	withEmbeddingFactories(t, &recordingEmbeddingFactory{})
	ctx := WithEmbeddingCredentials(context.Background(), EmbeddingCredentials{Ref: "ref", FactoryTimeout: 0})
	if _, err := ResolveEmbeddingClient(ctx, "voyage", "voyage-3"); err != nil {
		t.Fatalf("ResolveEmbeddingClient with FactoryTimeout=0: %v", err)
	}
}

func TestWithEmbeddingCredentials_RoundTrip(t *testing.T) {
	creds := EmbeddingCredentials{Ref: "vault://x", FactoryID: "tenant-a", FactoryTimeout: 750 * time.Millisecond}
	ctx := WithEmbeddingCredentials(context.Background(), creds)
	got := EmbeddingCredentialsFromContext(ctx)
	if got != creds {
		t.Fatalf("credentials round-trip mismatch: got %+v, want %+v", got, creds)
	}
}

func TestEmbeddingCredentialsFromContext_EmptyByDefault(t *testing.T) {
	got := EmbeddingCredentialsFromContext(context.Background())
	if (got != EmbeddingCredentials{}) {
		t.Fatalf("EmbeddingCredentialsFromContext(plain ctx) = %+v, want zero value", got)
	}
}

func TestEnvEmbeddingClientFactory_RejectsUnknownProvider(t *testing.T) {
	f := &EnvEmbeddingClientFactory{}
	_, err := f.Embedder(context.Background(), "openai", "text-embedding-3-small", "")
	if err == nil {
		t.Fatalf("Embedder(openai): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "RegisterEmbeddingClientFactory") {
		t.Fatalf("error %q should mention RegisterEmbeddingClientFactory so users know how to fix", err)
	}
}

// TestEnvEmbeddingClientFactory_WarnsOnceWhenRefIgnored asserts the bundled
// factory logs exactly one warning per ref when the caller passes a
// non-empty ref — bundled gemini path uses GEMINI_API_KEY only, so routing
// two refs to the same env key is the silent failure the warn flags. The
// second resolution of the same ref must NOT re-log (cache hit dedupes).
//
// We set GEMINI_API_KEY to a fake value so genai.NewClient succeeds
// (otherwise the client errors before we can cache, and re-warn would
// fire on the second call). The fake key never leaves the process.
func TestEnvEmbeddingClientFactory_WarnsOnceWhenRefIgnored(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-fake-key-not-used")
	buf := captureSlog(t)
	f := &EnvEmbeddingClientFactory{}
	// First call with ref=A: warn fires.
	if _, err := f.Embedder(context.Background(), "gemini", "gemini-embedding-001", "tenant-a"); err != nil {
		t.Fatalf("Embedder: %v", err)
	}
	// Second call with same ref=A: cache hit, no warn.
	if _, err := f.Embedder(context.Background(), "gemini", "gemini-embedding-001", "tenant-a"); err != nil {
		t.Fatalf("Embedder: %v", err)
	}
	out := buf.String()
	n := strings.Count(out, "EnvEmbeddingClientFactory: ref=")
	if n != 1 {
		t.Fatalf("expected exactly 1 warning across two same-ref resolutions, got %d; log:\n%s", n, out)
	}
	// The msg field interpolates ref=%q, which renders as ref=\"tenant-a\"
	// (escaped) inside the quoted msg attribute.
	if !strings.Contains(out, `ref=\"tenant-a\"`) {
		t.Fatalf("warning msg should include ref=\"tenant-a\"; log:\n%s", out)
	}
	if !strings.Contains(out, "GEMINI_API_KEY") {
		t.Fatalf("warning should mention GEMINI_API_KEY; log:\n%s", out)
	}
}

// TestEnvEmbeddingClientFactory_SilentWhenRefEmpty asserts the warning is
// NOT emitted for the documented "use env defaults" path (ref==""), which
// is the steady state for almost all callers.
func TestEnvEmbeddingClientFactory_SilentWhenRefEmpty(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-fake-key-not-used")
	buf := captureSlog(t)
	f := &EnvEmbeddingClientFactory{}
	if _, err := f.Embedder(context.Background(), "gemini", "gemini-embedding-001", ""); err != nil {
		t.Fatalf("Embedder: %v", err)
	}
	if got := buf.String(); strings.Contains(got, "EnvEmbeddingClientFactory: ref=") {
		t.Fatalf("expected no warning when ref==\"\"; got:\n%s", got)
	}
}

func TestRegisterEmbeddingClientFactory_ConcurrentRegistrationIsSafe(t *testing.T) {
	withEmbeddingFactories(t, &recordingEmbeddingFactory{})
	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			RegisterEmbeddingClientFactory("id", &recordingEmbeddingFactory{})
			_ = resolveEmbeddingFactory("id")
		}(i)
	}
	wg.Wait()
}

// fakeEmbedOnce records every call it receives and returns one stubbed
// embedding per input. It powers the chunking tests below without going
// through the genai SDK.
type fakeEmbedOnce struct {
	mu        sync.Mutex
	callSizes []int
	errOnCall map[int]error // 1-indexed call number → error to return
}

func (f *fakeEmbedOnce) fn(_ context.Context, contents []*genai.Content) (*genai.EmbedContentResponse, error) {
	f.mu.Lock()
	f.callSizes = append(f.callSizes, len(contents))
	call := len(f.callSizes)
	f.mu.Unlock()
	if err, ok := f.errOnCall[call]; ok {
		return nil, err
	}
	embeds := make([]*genai.ContentEmbedding, len(contents))
	for i := range contents {
		// Encode the call number and within-call index into the vector so
		// tests can verify order across chunks.
		embeds[i] = &genai.ContentEmbedding{Values: []float32{float32(call), float32(i)}}
	}
	return &genai.EmbedContentResponse{Embeddings: embeds}, nil
}

func TestGeminiEmbeddingClient_Embed_UnderCapMakesSingleCall(t *testing.T) {
	fake := &fakeEmbedOnce{}
	c := &geminiEmbeddingClient{model: "gemini-embedding-001", embedOnce: fake.fn}
	texts := make([]string, geminiEmbeddingMaxBatch) // exactly at cap → still a single call
	for i := range texts {
		texts[i] = "t"
	}
	out, err := c.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(out) != geminiEmbeddingMaxBatch {
		t.Fatalf("len(out) = %d, want %d", len(out), geminiEmbeddingMaxBatch)
	}
	if got := fake.callSizes; len(got) != 1 || got[0] != geminiEmbeddingMaxBatch {
		t.Fatalf("callSizes = %v, want one call of size %d", got, geminiEmbeddingMaxBatch)
	}
}

func TestGeminiEmbeddingClient_Embed_OverCapChunksAndPreservesOrder(t *testing.T) {
	fake := &fakeEmbedOnce{}
	c := &geminiEmbeddingClient{model: "gemini-embedding-001", embedOnce: fake.fn}
	const n = 2*geminiEmbeddingMaxBatch + 17 // 217 → 100 + 100 + 17
	texts := make([]string, n)
	for i := range texts {
		texts[i] = "t"
	}
	out, err := c.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(out) != n {
		t.Fatalf("len(out) = %d, want %d", len(out), n)
	}
	wantSizes := []int{geminiEmbeddingMaxBatch, geminiEmbeddingMaxBatch, 17}
	if got := fake.callSizes; len(got) != len(wantSizes) || got[0] != wantSizes[0] || got[1] != wantSizes[1] || got[2] != wantSizes[2] {
		t.Fatalf("callSizes = %v, want %v", got, wantSizes)
	}
	// Verify order across chunks: fake encodes (callNumber, indexWithinCall)
	// into each vector. Reconstruct the expected sequence and compare.
	for i, v := range out {
		expectedCall := i/geminiEmbeddingMaxBatch + 1
		expectedIdx := i % geminiEmbeddingMaxBatch
		if len(v) != 2 || int(v[0]) != expectedCall || int(v[1]) != expectedIdx {
			t.Fatalf("out[%d] = %v, want [%d %d]", i, v, expectedCall, expectedIdx)
		}
	}
}

func TestGeminiEmbeddingClient_Embed_ErrorInSecondChunkSurfaces(t *testing.T) {
	wantErr := errors.New("upstream boom")
	fake := &fakeEmbedOnce{errOnCall: map[int]error{2: wantErr}}
	c := &geminiEmbeddingClient{model: "gemini-embedding-001", embedOnce: fake.fn}
	texts := make([]string, geminiEmbeddingMaxBatch+5)
	for i := range texts {
		texts[i] = "t"
	}
	out, err := c.Embed(context.Background(), texts)
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("Embed err = %v, want wrapping %v", err, wantErr)
	}
	if out != nil {
		t.Fatalf("Embed out = %v on error, want nil", out)
	}
	// First chunk should have been attempted (and succeeded); second chunk is
	// where the error fires; no third chunk in this layout.
	if got := fake.callSizes; len(got) != 2 {
		t.Fatalf("callSizes = %v, want exactly 2 calls (success then error)", got)
	}
}

func TestGeminiEmbeddingClient_Embed_EmptyInputSkipsAPI(t *testing.T) {
	fake := &fakeEmbedOnce{}
	c := &geminiEmbeddingClient{model: "gemini-embedding-001", embedOnce: fake.fn}
	out, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed(nil): %v", err)
	}
	if out != nil {
		t.Fatalf("Embed(nil) out = %v, want nil", out)
	}
	if len(fake.callSizes) != 0 {
		t.Fatalf("callSizes = %v, want no calls for empty input", fake.callSizes)
	}
}

func TestGeminiEmbeddingClient_Embed_ResponseCountMismatchErrors(t *testing.T) {
	// Fake returns fewer embeddings than inputs — embedChunk must reject.
	short := func(_ context.Context, contents []*genai.Content) (*genai.EmbedContentResponse, error) {
		return &genai.EmbedContentResponse{Embeddings: make([]*genai.ContentEmbedding, len(contents)-1)}, nil
	}
	c := &geminiEmbeddingClient{model: "gemini-embedding-001", embedOnce: short}
	_, err := c.Embed(context.Background(), []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "response count mismatch") {
		t.Fatalf("Embed err = %v, want response count mismatch", err)
	}
}
