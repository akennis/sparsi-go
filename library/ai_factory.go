package library

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"google.golang.org/genai"
)

// AIClientFactory constructs provider SDK clients. Implementations decide where
// credentials come from (env vars, Vault, Secrets Manager, workload identity,
// an egress proxy, …) and how to cache the resulting clients.
//
// ref is opaque to the library. Empty means "default"; implementations are free
// to use it as a Vault path, tenant ID, region, or anything else they map onto
// a credential. The library never sees the API key.
type AIClientFactory interface {
	Anthropic(ctx context.Context, ref string) (*anthropic.Client, error)
	Gemini(ctx context.Context, ref string) (*genai.Client, error)
}

// EnvAIClientFactory is the bundled factory. It reads CLAUDE_API_KEY and
// GEMINI_API_KEY from the process environment and caches the constructed
// client per ref. Env-var credentials don't rotate, so a single entry under
// the empty ref is the steady state for almost all callers.
//
// SECURITY: the per-ref cache has no eviction. Do NOT derive ref from
// per-request input (tenant id, user id, request header value, query
// parameter, anything an attacker can vary): doing so produces an
// unbounded cache that leaks one *anthropic.Client / *genai.Client per
// distinct value and is a memory-exhaustion / DoS vector. Use ref only
// for the handful of named credential lookups the application itself
// controls (e.g. "prod", "staging", "tenant-acme") and define that set
// at deploy time, not from request data.
type EnvAIClientFactory struct {
	mu        sync.Mutex
	anthropic map[string]*anthropic.Client
	gemini    map[string]*genai.Client
}

// Anthropic returns an *anthropic.Client built from CLAUDE_API_KEY. ref is
// ignored — the env-var path has nothing to route on.
func (f *EnvAIClientFactory) Anthropic(ctx context.Context, ref string) (*anthropic.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.anthropic[ref]; ok {
		return c, nil
	}
	// Warn at most once per ref that the bundled factory ignores ref and
	// uses CLAUDE_API_KEY only. The cache miss above guarantees the warn
	// fires on first resolution; subsequent calls for the same ref hit
	// the cache and skip this branch entirely. Skip when ref=="" because
	// that's the documented "use env defaults" path.
	if ref != "" {
		slog.WarnContext(ctx, fmt.Sprintf("EnvAIClientFactory: ref=%q is ignored — bundled factory uses CLAUDE_API_KEY env var only. Register a custom factory via RegisterAIClientFactory for per-ref credential routing.", ref), "ref", ref, "provider", "claude")
	}
	c := anthropic.NewClient(option.WithAPIKey(os.Getenv("CLAUDE_API_KEY")))
	if f.anthropic == nil {
		f.anthropic = map[string]*anthropic.Client{}
	}
	f.anthropic[ref] = &c
	return &c, nil
}

// Gemini returns a *genai.Client built from GEMINI_API_KEY. ref is ignored.
func (f *EnvAIClientFactory) Gemini(ctx context.Context, ref string) (*genai.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.gemini[ref]; ok {
		return c, nil
	}
	// Warn at most once per ref that the bundled factory ignores ref and
	// uses GEMINI_API_KEY only. Same dedupe pattern as Anthropic above.
	if ref != "" {
		slog.WarnContext(ctx, fmt.Sprintf("EnvAIClientFactory: ref=%q is ignored — bundled factory uses GEMINI_API_KEY env var only. Register a custom factory via RegisterAIClientFactory for per-ref credential routing.", ref), "ref", ref, "provider", "gemini")
	}
	c, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: os.Getenv("GEMINI_API_KEY")})
	if err != nil {
		return nil, fmt.Errorf("gemini: create client: %w", err)
	}
	if f.gemini == nil {
		f.gemini = map[string]*genai.Client{}
	}
	f.gemini[ref] = c
	return c, nil
}

var (
	factoryMu        sync.RWMutex
	defaultFactory   AIClientFactory = &EnvAIClientFactory{}
	factoryRegistry                  = map[string]AIClientFactory{}
)

// SetDefaultAIClientFactory replaces the process-wide default factory. Most
// enterprise integrations call this once at program start. Passing nil resets
// to the bundled EnvAIClientFactory.
func SetDefaultAIClientFactory(f AIClientFactory) {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	if f == nil {
		defaultFactory = &EnvAIClientFactory{}
		return
	}
	defaultFactory = f
}

// RegisterAIClientFactory registers a factory under an id. AI op vertices opt
// in by setting the client_factory_id vertex param; absent or unknown ids fall
// back to the default factory.
func RegisterAIClientFactory(id string, f AIClientFactory) {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	if f == nil {
		delete(factoryRegistry, id)
		return
	}
	factoryRegistry[id] = f
}

// resolveFactory looks up an id in the registry; missing ids fall back to the
// process-wide default.
func resolveFactory(id string) AIClientFactory {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	if id != "" {
		if f, ok := factoryRegistry[id]; ok {
			return f
		}
	}
	return defaultFactory
}
