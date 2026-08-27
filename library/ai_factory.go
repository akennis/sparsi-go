package library

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go"
	openaioption "github.com/openai/openai-go/option"
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
	// OpenAI returns a client for the OpenAI Chat Completions API. The same
	// method serves OpenAI itself and any OpenAI-compatible server (e.g. a
	// local Ollama instance) — implementations choose the base URL.
	OpenAI(ctx context.Context, ref string) (*openai.Client, error)
}

// EnvAIClientFactory is the bundled factory. It reads CLAUDE_API_KEY,
// GEMINI_API_KEY, and OPENAI_API_KEY from the process environment and caches
// the constructed client per ref. Env-var credentials don't rotate, so a
// single entry under the empty ref is the steady state for almost all callers.
//
// To target a local Ollama server (or any OpenAI-compatible endpoint) through
// the "openai" provider, set OPENAI_BASE_URL (e.g.
// http://localhost:11434/v1). When OPENAI_BASE_URL is set and OPENAI_API_KEY
// is empty, a placeholder key is used so servers that don't check auth (like
// Ollama) work out of the box.
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
	openai    map[string]*openai.Client
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

// OpenAI returns an *openai.Client built from OPENAI_API_KEY, optionally
// pointed at OPENAI_BASE_URL (set it to a local Ollama endpoint such as
// http://localhost:11434/v1 to route the "openai" provider there). ref is
// ignored — the env-var path has nothing to route on.
func (f *EnvAIClientFactory) OpenAI(ctx context.Context, ref string) (*openai.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.openai[ref]; ok {
		return c, nil
	}
	// Warn at most once per ref that the bundled factory ignores ref and uses
	// OPENAI_API_KEY only. Same dedupe pattern as Anthropic above.
	if ref != "" {
		slog.WarnContext(ctx, fmt.Sprintf("EnvAIClientFactory: ref=%q is ignored — bundled factory uses OPENAI_API_KEY env var only. Register a custom factory via RegisterAIClientFactory for per-ref credential routing.", ref), "ref", ref, "provider", "openai")
	}
	key := os.Getenv("OPENAI_API_KEY")
	base := os.Getenv("OPENAI_BASE_URL")
	opts := []openaioption.RequestOption{}
	if base != "" {
		opts = append(opts, openaioption.WithBaseURL(base))
		// Local OpenAI-compatible servers (e.g. Ollama) don't check auth but
		// the SDK still sends an Authorization header; supply a placeholder so
		// callers needn't invent a key.
		if key == "" {
			key = "ollama"
		}
	}
	opts = append(opts, openaioption.WithAPIKey(key))
	c := openai.NewClient(opts...)
	if f.openai == nil {
		f.openai = map[string]*openai.Client{}
	}
	f.openai[ref] = &c
	return &c, nil
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
