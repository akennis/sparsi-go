package library

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/wwz16/dagor/config"
	"google.golang.org/genai"
)

type aiCallRequest struct {
	SystemText string
	Prompt     string
	MaxTokens  int64
}

type aiCallResult struct {
	Text         string
	InputTokens  int64
	OutputTokens int64
}

type aiCaller interface {
	call(ctx context.Context, req aiCallRequest) (aiCallResult, error)
}

// retryConfig controls exponential backoff for transient API errors.
type retryConfig struct {
	maxRetries     int   // max retry attempts (default 3)
	initialDelayMs int64 // starting delay in ms (default 500)
}

// parseRetryConfig reads api_retries and api_retry_delay_ms from vertex params.
func parseRetryConfig(params *config.Params) retryConfig {
	cfg := retryConfig{maxRetries: 3, initialDelayMs: 500}
	if s := params.GetString("api_retries", ""); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			cfg.maxRetries = n
		}
	}
	if s := params.GetString("api_retry_delay_ms", ""); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			cfg.initialDelayMs = n
		}
	}
	return cfg
}

// newAICaller creates a caller for the given provider and model, wrapped with exponential backoff.
// provider must be "claude" or "gemini"; model is passed through opaquely to the SDK.
// Returns an error for unknown providers so graphs fail fast at Setup.
func newAICaller(provider, model string, cfg retryConfig) (aiCaller, error) {
	var inner aiCaller
	switch provider {
	case "claude":
		inner = &anthropicCaller{model: model}
	case "gemini":
		inner = &geminiCaller{model: model}
	default:
		return nil, fmt.Errorf("unsupported provider %q: must be \"claude\" or \"gemini\"", provider)
	}
	if cfg.maxRetries <= 0 {
		return inner, nil
	}
	return &retryingCaller{inner: inner, cfg: cfg}, nil
}

// isTransientError reports whether an API error is worth retrying (e.g. 503, 429, overloaded).
func isTransientError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, pattern := range []string{
		"503", "429",
		"too many requests", "rate limit", "rate_limit",
		"overloaded", "unavailable",
		"high demand", "try again",
		"service unavailable",
	} {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

// retryingCaller wraps an aiCaller with exponential backoff for transient errors.
type retryingCaller struct {
	inner aiCaller
	cfg   retryConfig
}

func (c *retryingCaller) call(ctx context.Context, req aiCallRequest) (aiCallResult, error) {
	delay := time.Duration(c.cfg.initialDelayMs) * time.Millisecond
	var lastErr error
	for attempt := 0; attempt <= c.cfg.maxRetries; attempt++ {
		if attempt > 0 {
			var jitter time.Duration
			if r := int64(delay) / 4; r > 0 {
				jitter = time.Duration(rand.Int63n(r))
			}
			select {
			case <-ctx.Done():
				return aiCallResult{}, ctx.Err()
			case <-time.After(delay + jitter):
			}
			delay = min(delay*2, 30*time.Second)
		}
		result, err := c.inner.call(ctx, req)
		if err == nil {
			return result, nil
		}
		if !isTransientError(err) {
			return aiCallResult{}, err
		}
		lastErr = err
		slog.WarnContext(ctx, "ai.retry", "attempt", attempt+1, "of", c.cfg.maxRetries, "err", err)
	}
	return aiCallResult{}, fmt.Errorf("after %d retries: %w", c.cfg.maxRetries, lastErr)
}

// anthropicCaller calls the Anthropic Messages API.
// API key is read from CLAUDE_API_KEY.
type anthropicCaller struct{ model string }

func (c *anthropicCaller) call(ctx context.Context, req aiCallRequest) (aiCallResult, error) {
	client := anthropic.NewClient(option.WithAPIKey(os.Getenv("CLAUDE_API_KEY")))
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: req.MaxTokens,
		System:    []anthropic.TextBlockParam{{Text: req.SystemText}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.Prompt))},
	})
	if err != nil {
		return aiCallResult{}, err
	}
	var text string
	for _, block := range msg.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return aiCallResult{
		Text:         text,
		InputTokens:  msg.Usage.InputTokens,
		OutputTokens: msg.Usage.OutputTokens,
	}, nil
}

// geminiCaller calls the Gemini GenerateContent API.
// API key is read from GEMINI_API_KEY.
type geminiCaller struct{ model string }

func (c *geminiCaller) call(ctx context.Context, req aiCallRequest) (aiCallResult, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: os.Getenv("GEMINI_API_KEY"),
	})
	if err != nil {
		return aiCallResult{}, fmt.Errorf("gemini: create client: %w", err)
	}
	config := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(req.SystemText, genai.RoleUser),
		MaxOutputTokens:   int32(req.MaxTokens),
	}
	result, err := client.Models.GenerateContent(ctx, c.model, genai.Text(req.Prompt), config)
	if err != nil {
		return aiCallResult{}, fmt.Errorf("gemini: generate content: %w", err)
	}
	var inputTokens, outputTokens int64
	if result.UsageMetadata != nil {
		inputTokens = int64(result.UsageMetadata.PromptTokenCount)
		outputTokens = int64(result.UsageMetadata.CandidatesTokenCount)
	}
	text := result.Text()
	if text == "" && len(result.Candidates) > 0 {
		slog.WarnContext(ctx, "gemini.empty", "finish_reason", result.Candidates[0].FinishReason)
	}
	return aiCallResult{
		Text:         text,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}
