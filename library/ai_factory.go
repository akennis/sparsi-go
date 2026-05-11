package library

import (
	"context"
	"fmt"
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
type EnvAIClientFactory struct {
	mu        sync.Mutex
	anthropic map[string]*anthropic.Client
	gemini    map[string]*genai.Client
}

// Anthropic returns an *anthropic.Client built from CLAUDE_API_KEY. ref is
// ignored — the env-var path has nothing to route on.
func (f *EnvAIClientFactory) Anthropic(_ context.Context, ref string) (*anthropic.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.anthropic[ref]; ok {
		return c, nil
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
