package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/akennis/dagor/config"
	"google.golang.org/genai"
)

// blockingFactory blocks Anthropic() on the supplied ctx until cancellation,
// then returns ctx.Err(). It lets tests assert that newAICaller bounds the
// factory call with a deadline.
type blockingFactory struct{}

func (blockingFactory) Anthropic(ctx context.Context, _ string) (*anthropic.Client, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingFactory) Gemini(ctx context.Context, _ string) (*genai.Client, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestNewAICaller_FactoryTimeoutFires(t *testing.T) {
	withFactories(t, blockingFactory{})
	start := time.Now()
	_, err := newAICaller("claude", "m", "", "", retryConfig{factoryTimeout: 50 * time.Millisecond})
	elapsed := time.Since(start)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("newAICaller err = %v, want wrapping context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("newAICaller took %v, expected ~50ms", elapsed)
	}
}

func TestParseRetryConfig_DefaultFactoryTimeoutApplied(t *testing.T) {
	params, err := config.NewFromRaw(nil)
	if err != nil {
		t.Fatalf("config.NewFromRaw: %v", err)
	}
	cfg := parseRetryConfig(params)
	if cfg.factoryTimeout != 30*time.Second {
		t.Fatalf("default factoryTimeout = %v, want 30s", cfg.factoryTimeout)
	}
}

func TestParseRetryConfig_FactoryTimeoutOverride(t *testing.T) {
	params, err := config.NewFromRaw([]byte(`{"api_factory_timeout_ms":"1500"}`))
	if err != nil {
		t.Fatalf("config.NewFromRaw: %v", err)
	}
	cfg := parseRetryConfig(params)
	if cfg.factoryTimeout != 1500*time.Millisecond {
		t.Fatalf("factoryTimeout = %v, want 1.5s", cfg.factoryTimeout)
	}
}

func TestNewAICaller_ZeroTimeoutDisablesDeadline(t *testing.T) {
	// With factoryTimeout=0 and a fast (recording) factory, newAICaller must
	// not produce a deadline error — it must reach the factory and return its
	// (nil) result.
	withFactories(t, &recordingFactory{})
	if _, err := newAICaller("claude", "m", "ref", "", retryConfig{factoryTimeout: 0}); err != nil {
		t.Fatalf("newAICaller with factoryTimeout=0: %v", err)
	}
}
