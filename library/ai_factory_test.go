package library

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"google.golang.org/genai"
)

// recordingFactory captures every call and returns nil clients. Tests that use
// it must not invoke the resulting aiCaller's call() method — they only check
// wiring (which factory was selected, what ref was passed).
type recordingFactory struct {
	mu      sync.Mutex
	calls   []factoryCall
	anthErr error
	gemErr  error
}

type factoryCall struct {
	provider string
	ref      string
}

func (f *recordingFactory) Anthropic(_ context.Context, ref string) (*anthropic.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, factoryCall{provider: "claude", ref: ref})
	if f.anthErr != nil {
		return nil, f.anthErr
	}
	return nil, nil
}

func (f *recordingFactory) Gemini(_ context.Context, ref string) (*genai.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, factoryCall{provider: "gemini", ref: ref})
	if f.gemErr != nil {
		return nil, f.gemErr
	}
	return nil, nil
}

func (f *recordingFactory) snapshot() []factoryCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]factoryCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// withFactories swaps the default factory and registry to a clean slate for
// the duration of a test, restoring on cleanup.
func withFactories(t *testing.T, def AIClientFactory) {
	t.Helper()
	factoryMu.Lock()
	prevDefault := defaultFactory
	prevRegistry := factoryRegistry
	defaultFactory = def
	factoryRegistry = map[string]AIClientFactory{}
	factoryMu.Unlock()
	t.Cleanup(func() {
		factoryMu.Lock()
		defaultFactory = prevDefault
		factoryRegistry = prevRegistry
		factoryMu.Unlock()
	})
}

func TestResolveFactory_DefaultWhenNoIDSet(t *testing.T) {
	def := &recordingFactory{}
	withFactories(t, def)
	if got := resolveFactory(""); got != def {
		t.Fatalf("resolveFactory(\"\") = %p, want default %p", got, def)
	}
}

func TestResolveFactory_RegisteredIDWins(t *testing.T) {
	def := &recordingFactory{}
	tenant := &recordingFactory{}
	withFactories(t, def)
	RegisterAIClientFactory("tenant-a", tenant)
	if got := resolveFactory("tenant-a"); got != tenant {
		t.Fatalf("resolveFactory(\"tenant-a\") = %p, want tenant %p", got, tenant)
	}
}

func TestResolveFactory_UnknownIDFallsBackToDefault(t *testing.T) {
	def := &recordingFactory{}
	withFactories(t, def)
	if got := resolveFactory("nope"); got != def {
		t.Fatalf("resolveFactory(\"nope\") = %p, want default %p", got, def)
	}
}

func TestSetDefaultAIClientFactory_NilResetsToEnv(t *testing.T) {
	withFactories(t, &recordingFactory{})
	SetDefaultAIClientFactory(nil)
	if _, ok := resolveFactory("").(*EnvAIClientFactory); !ok {
		t.Fatalf("after SetDefaultAIClientFactory(nil), default = %T, want *EnvAIClientFactory", resolveFactory(""))
	}
}

func TestNewAICaller_DefaultFactoryReceivesRef(t *testing.T) {
	def := &recordingFactory{}
	withFactories(t, def)
	if _, err := newAICaller("claude", "claude-sonnet-4-6", "vault://prod/anthropic", "", retryConfig{}); err != nil {
		t.Fatalf("newAICaller: %v", err)
	}
	calls := def.snapshot()
	if len(calls) != 1 || calls[0].provider != "claude" || calls[0].ref != "vault://prod/anthropic" {
		t.Fatalf("default factory calls = %+v, want one claude call with ref=vault://prod/anthropic", calls)
	}
}

func TestNewAICaller_RegisteredFactoryWinsOverDefault(t *testing.T) {
	def := &recordingFactory{}
	tenant := &recordingFactory{}
	withFactories(t, def)
	RegisterAIClientFactory("tenant-a", tenant)
	if _, err := newAICaller("claude", "m", "ref-x", "tenant-a", retryConfig{}); err != nil {
		t.Fatalf("newAICaller: %v", err)
	}
	if got := tenant.snapshot(); len(got) != 1 || got[0].ref != "ref-x" {
		t.Fatalf("tenant calls = %+v, want one call with ref=ref-x", got)
	}
	if got := def.snapshot(); len(got) != 0 {
		t.Fatalf("default factory calls = %+v, want none", got)
	}
}

func TestNewAICaller_GeminiRoutesToFactory(t *testing.T) {
	def := &recordingFactory{}
	withFactories(t, def)
	if _, err := newAICaller("gemini", "gemini-2.5", "tenants/42", "", retryConfig{}); err != nil {
		t.Fatalf("newAICaller: %v", err)
	}
	calls := def.snapshot()
	if len(calls) != 1 || calls[0].provider != "gemini" || calls[0].ref != "tenants/42" {
		t.Fatalf("default factory calls = %+v, want one gemini call with ref=tenants/42", calls)
	}
}

func TestNewAICaller_FactoryErrorPropagates(t *testing.T) {
	want := errors.New("vault denied")
	withFactories(t, &recordingFactory{anthErr: want})
	_, err := newAICaller("claude", "m", "", "", retryConfig{})
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("newAICaller err = %v, want wrapping %v", err, want)
	}
}

func TestRegisterAIClientFactory_NilDeregisters(t *testing.T) {
	def := &recordingFactory{}
	tenant := &recordingFactory{}
	withFactories(t, def)
	RegisterAIClientFactory("tenant-a", tenant)
	RegisterAIClientFactory("tenant-a", nil)
	if got := resolveFactory("tenant-a"); got != def {
		t.Fatalf("after deregister, resolveFactory(\"tenant-a\") = %p, want default %p", got, def)
	}
}

func TestEnvAIClientFactory_AnthropicCachesByRef(t *testing.T) {
	f := &EnvAIClientFactory{}
	a1, err := f.Anthropic(context.Background(), "")
	if err != nil {
		t.Fatalf("Anthropic: %v", err)
	}
	a2, err := f.Anthropic(context.Background(), "")
	if err != nil {
		t.Fatalf("Anthropic: %v", err)
	}
	if a1 != a2 {
		t.Fatalf("expected cached identical *anthropic.Client across calls; got %p vs %p", a1, a2)
	}
}

// TestEnvAIClientFactory_WarnsOnceWhenRefIgnored asserts the bundled factory
// logs exactly one warning per ref when a caller passes a non-empty ref —
// since the factory ignores ref and uses CLAUDE_API_KEY/GEMINI_API_KEY only,
// silently routing two different refs to the same env key is the surprise
// the warning guards against. The second resolution of the same ref must
// NOT re-log (cache hit dedupes).
func TestEnvAIClientFactory_WarnsOnceWhenRefIgnored(t *testing.T) {
	buf := captureSlog(t)
	f := &EnvAIClientFactory{}
	// First call with ref=A: warn fires.
	if _, err := f.Anthropic(context.Background(), "tenant-a"); err != nil {
		t.Fatalf("Anthropic: %v", err)
	}
	// Second call with same ref=A: cache hit, no warn.
	if _, err := f.Anthropic(context.Background(), "tenant-a"); err != nil {
		t.Fatalf("Anthropic: %v", err)
	}
	out := buf.String()
	n := strings.Count(out, "EnvAIClientFactory: ref=")
	if n != 1 {
		t.Fatalf("expected exactly 1 warning across two same-ref resolutions, got %d; log:\n%s", n, out)
	}
	// The msg field interpolates ref=%q, which renders as ref=\"tenant-a\"
	// (escaped) inside the quoted msg attribute.
	if !strings.Contains(out, `ref=\"tenant-a\"`) {
		t.Fatalf("warning msg should include ref=\"tenant-a\"; log:\n%s", out)
	}
	if !strings.Contains(out, "CLAUDE_API_KEY") {
		t.Fatalf("warning should mention CLAUDE_API_KEY for the anthropic path; log:\n%s", out)
	}
}

// TestEnvAIClientFactory_SilentWhenRefEmpty asserts the warning is NOT
// emitted for the documented "use env defaults" path (ref==""), which is
// the steady state for almost all callers.
func TestEnvAIClientFactory_SilentWhenRefEmpty(t *testing.T) {
	buf := captureSlog(t)
	f := &EnvAIClientFactory{}
	if _, err := f.Anthropic(context.Background(), ""); err != nil {
		t.Fatalf("Anthropic: %v", err)
	}
	if got := buf.String(); strings.Contains(got, "EnvAIClientFactory: ref=") {
		t.Fatalf("expected no warning when ref==\"\"; got:\n%s", got)
	}
}
