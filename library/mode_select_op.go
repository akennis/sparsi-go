package library

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/operator"
)

const ModeSelectOpDescription = `ModeSelectOp: AI-powered classifier — maps arbitrary input text to exactly one of a fixed set of categories.
  Params:   categories string — comma-separated list of valid output values (e.g. "arithmetic expression,city name").
            max_retries string — parse/validation retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
            credential_ref string — opaque credential identifier passed to AIClientFactory (default ""; ignored by the bundled env-var factory).
            client_factory_id string — selects a registered AIClientFactory by id (default "" → process default).
  Inputs:   Input *string — the text to classify.
  Outputs:  Result string — exactly one of the specified categories.`

// ModeSelectOp classifies an arbitrary input string into one of a fixed set of
// categories using an AI call. Use it at the top of a multi-branch workflow to
// dispatch to the correct branch based on what the input represents.
type ModeSelectOp struct {
	Input  *string `dag:"input"`
	Result string  `dag:"output"`

	categories []string
	maxRetries int
	provider   string
	model      string
	caller     aiCaller
}

func (op *ModeSelectOp) Setup(params *config.Params) error {
	raw := params.GetString("categories", "")
	if raw == "" {
		return fmt.Errorf("ModeSelectOp: 'categories' param is required")
	}
	op.categories = nil // reset before appending so pool reuse doesn't accumulate
	for _, c := range strings.Split(raw, ",") {
		if c = strings.TrimSpace(c); c != "" {
			op.categories = append(op.categories, c)
		}
	}
	if len(op.categories) < 2 {
		return fmt.Errorf("ModeSelectOp: at least 2 categories required, got %d", len(op.categories))
	}
	op.maxRetries = 3
	if s := params.GetString("max_retries", ""); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			op.maxRetries = n
		}
	}
	op.provider = params.GetString("provider", "claude")
	op.model = params.GetString("model", "claude-sonnet-4-6")
	credRef := params.GetString("credential_ref", "")
	factoryID := params.GetString("client_factory_id", "")
	caller, err := newAICaller(op.provider, op.model, credRef, factoryID, parseRetryConfig(params))
	if err != nil {
		return fmt.Errorf("ModeSelectOp: %w", err)
	}
	op.caller = caller
	return nil
}

func (op *ModeSelectOp) Reset() error { return nil }

func (op *ModeSelectOp) Run(ctx context.Context) error {
	slog.DebugContext(ctx, "ModeSelectOp.run", "run_id", dagor.RunID(ctx), "categories", op.categories)

	isReasoning := logFromCtx(ctx) != nil

	catList := strings.Join(op.categories, ", ")
	basePrompt := fmt.Sprintf(
		"Classify the following input as exactly one of these categories: %s.\n"+
			"Respond with only the category name — no other text.\n"+
			"Input: %s",
		catList, *op.Input,
	)

	var systemText string
	if isReasoning {
		systemText = `Respond with a JSON object {"result": "<category>", "reasoning": "<brief explanation>"}. No markdown, no other text.`
	} else {
		systemText = "Respond with only the requested value. No explanation, no punctuation, no formatting."
	}

	catSet := make(map[string]bool, len(op.categories))
	for _, c := range op.categories {
		catSet[c] = true
	}

	prompt := basePrompt
	var lastErr string
	for attempt := 0; attempt <= op.maxRetries; attempt++ {
		maxTokens := int64(64)
		if isReasoning {
			maxTokens = 512
		}
		res, err := op.caller.call(ctx, aiCallRequest{
			SystemText: systemText,
			Prompt:     prompt,
			MaxTokens:  maxTokens,
		})
		if err != nil {
			return fmt.Errorf("generate content: %w", err)
		}
		slog.InfoContext(ctx, "ModeSelectOp.tokens", "run_id", dagor.RunID(ctx), "input_tokens", res.InputTokens, "output_tokens", res.OutputTokens)

		raw := strings.TrimSpace(res.Text)

		var result, reasoning string
		if isReasoning {
			var envelope struct {
				Result    string `json:"result"`
				Reasoning string `json:"reasoning"`
			}
			if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
				lastErr = fmt.Sprintf("expected JSON {result, reasoning}, got %q: %v", raw, err)
				prompt = basePrompt + fmt.Sprintf("\n\nPrevious response was invalid JSON — %s.", lastErr)
				slog.DebugContext(ctx, "ModeSelectOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
				continue
			}
			result = strings.TrimSpace(envelope.Result)
			reasoning = envelope.Reasoning
		} else {
			result = raw
		}

		if !catSet[result] {
			lastErr = fmt.Sprintf("result %q is not one of %v", result, op.categories)
			prompt = basePrompt + fmt.Sprintf("\n\nPrevious result %q was invalid — must be exactly one of: %s.", result, catList)
			slog.DebugContext(ctx, "ModeSelectOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
			continue
		}
		op.Result = result
		recordReasoning(ctx, "ModeSelectOp", map[string]any{
			"Input":      *op.Input,
			"Categories": op.categories,
		}, op.Result, reasoning)
		return nil
	}
	return fmt.Errorf("ModeSelectOp: all %d attempts failed; last error: %s", op.maxRetries+1, lastErr)
}

func init() {
	operator.RegisterOp[ModeSelectOp]()
}
