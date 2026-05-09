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

// ---- Concrete variants of AIComputeOp ----

const AIExtractStringSliceOpDescription = `AIExtractStringSliceOp: AI-powered extraction of a list from text.
  Params:   operation string — plain-English description (e.g. "extract all ingredient names from this recipe").
            max_retries string — parse retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
  Inputs:   Input *string.
  Outputs:  Result []string (CSV), Reasoning string.`

// AIExtractStringSliceOp extracts a list of strings from arbitrary text.
type AIExtractStringSliceOp struct {
	AIComputeOp[string, []string]
}

const AIExtractMapOpDescription = `AIExtractMapOp: AI-powered extraction of key-value pairs from text.
  Params:   operation string — plain-English description (e.g. "extract name, email, and city from this contact info").
            max_retries string — parse retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
  Inputs:   Input *string.
  Outputs:  Result map[string]string (key=value CSV), Reasoning string.`

// AIExtractMapOp extracts a fixed-key record from arbitrary text.
type AIExtractMapOp struct {
	AIComputeOp[string, map[string]string]
}

const AIParseNumberOpDescription = `AIParseNumberOp: AI-powered number extraction — converts text to float64.
  Params:   operation string — plain-English description (default: leave empty to extract the number from the text).
            max_retries string — parse retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
  Inputs:   Input *string (e.g. "two thousand", "$1.2k", "the price is 42").
  Outputs:  Result float64, Reasoning string.`

// AIParseNumberOp converts free-form text to a float64.
type AIParseNumberOp struct {
	AIComputeOp[string, float64]
}

const AISummarizeOpDescription = `AISummarizeOp: AI-powered summarization of a list of strings into one result string.
  Params:   operation string — plain-English instruction (e.g. "summarize into one concise sentence").
            max_retries string — parse retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
  Inputs:   Input *[]string — items to summarize.
  Outputs:  Result string, Reasoning string.`

// AISummarizeOp summarizes a slice of strings into a single string.
type AISummarizeOp struct {
	AIComputeOp[[]string, string]
}

// ---- Bespoke AI ops ----

const AIClassifyMultiLabelOpDescription = `AIClassifyMultiLabelOp: AI-powered multi-label classifier — maps input to zero or more categories.
  Params:   categories string — comma-separated list of valid labels (e.g. "billing,bug,feature,spam").
            max_retries string — parse/validation retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
  Inputs:   Input *string.
  Outputs:  Result []string — subset of categories (CSV), Reasoning string.`

// AIClassifyMultiLabelOp classifies text into zero or more of a fixed set of categories.
type AIClassifyMultiLabelOp struct {
	Input     *string
	Result    []string
	Reasoning string

	categories []string
	catSet     map[string]bool
	maxRetries int
	provider   string
	model      string
	caller     aiCaller
}

func (op *AIClassifyMultiLabelOp) Setup(params *config.Params) error {
	raw := params.GetString("categories", "")
	if raw == "" {
		return fmt.Errorf("AIClassifyMultiLabelOp: 'categories' param is required")
	}
	op.categories = nil // reset before appending so pool reuse doesn't accumulate
	op.catSet = make(map[string]bool)
	for _, c := range strings.Split(raw, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			op.categories = append(op.categories, c)
			op.catSet[c] = true
		}
	}
	if len(op.categories) < 2 {
		return fmt.Errorf("AIClassifyMultiLabelOp: at least 2 categories required, got %d", len(op.categories))
	}
	op.maxRetries = 3
	if s := params.GetString("max_retries", ""); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			op.maxRetries = n
		}
	}
	op.provider = params.GetString("provider", "claude")
	op.model = params.GetString("model", "claude-sonnet-4-6")
	caller, err := newAICaller(op.provider, op.model, parseRetryConfig(params))
	if err != nil {
		return fmt.Errorf("AIClassifyMultiLabelOp: %w", err)
	}
	op.caller = caller
	return nil
}

func (op *AIClassifyMultiLabelOp) Reset() error { return nil }

func (op *AIClassifyMultiLabelOp) InputFields() map[string]any {
	return map[string]any{"Input": &op.Input}
}

func (op *AIClassifyMultiLabelOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}

func (op *AIClassifyMultiLabelOp) SetInputField(field string, value any) error {
	switch field {
	case "Input":
		val, ok := value.(*string)
		if !ok {
			return fmt.Errorf("field Input: expected *string, got %T", value)
		}
		op.Input = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}

func (op *AIClassifyMultiLabelOp) ResetFields() {
	op.Input = nil
	op.Result = nil
	op.Reasoning = ""
}

func (op *AIClassifyMultiLabelOp) Run(ctx context.Context) error {
	slog.DebugContext(ctx, "AIClassifyMultiLabelOp.run", "run_id", dagor.RunID(ctx), "model", op.model, "categories", op.categories)

	isReasoning := logFromCtx(ctx) != nil
	catList := strings.Join(op.categories, ", ")

	var basePrompt string
	var systemText string
	if isReasoning {
		basePrompt = fmt.Sprintf(
			"Classify the following input into zero or more of these categories: %s.\n"+
				`Respond with a JSON object {"labels": "<comma-separated matching categories or empty string>", "reasoning": "<brief explanation>"}.`+"\n"+
				"Input: %s",
			catList, *op.Input,
		)
		systemText = `Respond with only a JSON object {"labels": "<CSV or empty>", "reasoning": "<explanation>"}. No other text.`
	} else {
		basePrompt = fmt.Sprintf(
			"Classify the following input into zero or more of these categories: %s.\n"+
				"Respond with matching categories as a comma-separated list. If none match, respond with an empty line.\n"+
				"Input: %s",
			catList, *op.Input,
		)
		systemText = "Respond with only the requested value. No explanation, no punctuation beyond commas, no formatting."
	}

	prompt := basePrompt
	var lastErr string
	for attempt := 0; attempt <= op.maxRetries; attempt++ {
		res, err := op.caller.call(ctx, aiCallRequest{
			SystemText: systemText,
			Prompt:     prompt,
			MaxTokens:  256,
		})
		if err != nil {
			return fmt.Errorf("generate content: %w", err)
		}
		slog.InfoContext(ctx, "AIClassifyMultiLabelOp.tokens", "run_id", dagor.RunID(ctx), "model", op.model, "input_tokens", res.InputTokens, "output_tokens", res.OutputTokens)

		raw := strings.TrimSpace(res.Text)

		var labelsCSV, reasoning string
		if isReasoning {
			var envelope struct {
				Labels    string `json:"labels"`
				Reasoning string `json:"reasoning"`
			}
			if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
				lastErr = fmt.Sprintf("expected JSON {labels, reasoning}, got %q: %v", raw, err)
				prompt = basePrompt + fmt.Sprintf("\n\nPrevious response was not valid JSON. Respond with only: {\"labels\": \"<CSV>\", \"reasoning\": \"<string>\"}.")
				slog.DebugContext(ctx, "AIClassifyMultiLabelOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
				continue
			}
			labelsCSV = envelope.Labels
			reasoning = envelope.Reasoning
		} else {
			labelsCSV = raw
		}

		var labels []string
		if labelsCSV != "" {
			for _, item := range strings.Split(labelsCSV, ",") {
				item = strings.TrimSpace(item)
				if item != "" {
					labels = append(labels, item)
				}
			}
		}

		var invalid []string
		for _, label := range labels {
			if !op.catSet[label] {
				invalid = append(invalid, label)
			}
		}

		if len(invalid) > 0 {
			lastErr = fmt.Sprintf("invalid categories %v not in %v", invalid, op.categories)
			prompt = basePrompt + fmt.Sprintf("\n\nPrevious response contained invalid categories: %v. Use only: %s.", invalid, catList)
			slog.DebugContext(ctx, "AIClassifyMultiLabelOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
			continue
		}

		op.Result = labels
		if isReasoning {
			recordReasoning(ctx, "AIClassifyMultiLabelOp", map[string]any{
				"Input":      *op.Input,
				"Categories": op.categories,
			}, op.Result, reasoning)
		}
		return nil
	}
	return fmt.Errorf("AIClassifyMultiLabelOp: all %d attempts failed; last error: %s", op.maxRetries+1, lastErr)
}

const AIScoreOpDescription = `AIScoreOp: AI-powered scoring — returns a float64 in [0,1] measuring a criterion.
  Params:   criterion string — what to measure (e.g. "relevance to the query", "toxicity").
            max_retries string — parse/validation retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
  Inputs:   Input *string.
  Outputs:  Result float64 ∈ [0,1], Reasoning string.`

// AIScoreOp scores text against a criterion, returning a value in [0,1].
type AIScoreOp struct {
	Input     *string
	Result    float64
	Reasoning string

	criterion  string
	maxRetries int
	provider   string
	model      string
	caller     aiCaller
}

func (op *AIScoreOp) Setup(params *config.Params) error {
	op.criterion = params.GetString("criterion", "")
	if op.criterion == "" {
		return fmt.Errorf("AIScoreOp: 'criterion' param is required")
	}
	op.maxRetries = 3
	if s := params.GetString("max_retries", ""); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			op.maxRetries = n
		}
	}
	op.provider = params.GetString("provider", "claude")
	op.model = params.GetString("model", "claude-sonnet-4-6")
	caller, err := newAICaller(op.provider, op.model, parseRetryConfig(params))
	if err != nil {
		return fmt.Errorf("AIScoreOp: %w", err)
	}
	op.caller = caller
	return nil
}

func (op *AIScoreOp) Reset() error { return nil }

func (op *AIScoreOp) InputFields() map[string]any {
	return map[string]any{"Input": &op.Input}
}

func (op *AIScoreOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}

func (op *AIScoreOp) SetInputField(field string, value any) error {
	switch field {
	case "Input":
		val, ok := value.(*string)
		if !ok {
			return fmt.Errorf("field Input: expected *string, got %T", value)
		}
		op.Input = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}

func (op *AIScoreOp) ResetFields() {
	op.Input = nil
	op.Result = 0
	op.Reasoning = ""
}

func (op *AIScoreOp) Run(ctx context.Context) error {
	slog.DebugContext(ctx, "AIScoreOp.run", "run_id", dagor.RunID(ctx), "model", op.model, "criterion", op.criterion)

	isReasoning := logFromCtx(ctx) != nil

	var basePrompt, systemText string
	if isReasoning {
		basePrompt = fmt.Sprintf(
			"Score the following text for %s on a scale from 0.0 to 1.0.\n"+
				`Respond with a JSON object: {"score": <float 0.0–1.0>, "reasoning": "<brief explanation>"}.`+"\n"+
				"Text: %s",
			op.criterion, *op.Input,
		)
		systemText = `Respond with only a JSON object: {"score": <decimal 0.0–1.0>, "reasoning": "<brief explanation>"}. No other text.`
	} else {
		basePrompt = fmt.Sprintf(
			"Score the following text for %s on a scale from 0.0 to 1.0.\n"+
				"Respond with only the numeric score. No explanation.\n"+
				"Text: %s",
			op.criterion, *op.Input,
		)
		systemText = "Respond with only a decimal number between 0.0 and 1.0. No other text."
	}

	prompt := basePrompt
	var lastErr string
	for attempt := 0; attempt <= op.maxRetries; attempt++ {
		maxTokens := int64(16)
		if isReasoning {
			maxTokens = 256
		}
		res, err := op.caller.call(ctx, aiCallRequest{
			SystemText: systemText,
			Prompt:     prompt,
			MaxTokens:  maxTokens,
		})
		if err != nil {
			return fmt.Errorf("generate content: %w", err)
		}
		slog.InfoContext(ctx, "AIScoreOp.tokens", "run_id", dagor.RunID(ctx), "model", op.model, "input_tokens", res.InputTokens, "output_tokens", res.OutputTokens)

		raw := strings.TrimSpace(res.Text)

		if isReasoning {
			var parsed struct {
				Score     float64 `json:"score"`
				Reasoning string  `json:"reasoning"`
			}
			if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				lastErr = fmt.Sprintf("expected JSON {score, reasoning}, got %q: %v", raw, err)
				prompt = basePrompt + "\n\nPrevious response was not valid JSON. Respond with only: {\"score\": <float>, \"reasoning\": \"<string>\"}."
				slog.DebugContext(ctx, "AIScoreOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
				continue
			}
			if parsed.Score < 0 || parsed.Score > 1 {
				lastErr = fmt.Sprintf("score %v out of [0,1]", parsed.Score)
				prompt = basePrompt + fmt.Sprintf("\n\nPrevious score %v was out of range. The score field must be between 0.0 and 1.0.", parsed.Score)
				slog.DebugContext(ctx, "AIScoreOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
				continue
			}
			op.Result = parsed.Score
			recordReasoning(ctx, "AIScoreOp", map[string]any{
				"Input":     *op.Input,
				"Criterion": op.criterion,
			}, op.Result, parsed.Reasoning)
			return nil
		}

		score, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			lastErr = fmt.Sprintf("expected float64, got %q: %v", raw, err)
			prompt = basePrompt + fmt.Sprintf("\n\nPrevious response %q was not a valid number. Respond with only a decimal number between 0.0 and 1.0.", raw)
			slog.DebugContext(ctx, "AIScoreOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
			continue
		}
		if score < 0 || score > 1 {
			lastErr = fmt.Sprintf("score %v out of [0,1]", score)
			prompt = basePrompt + fmt.Sprintf("\n\nPrevious score %v was out of range. Respond with a number between 0.0 and 1.0.", score)
			slog.DebugContext(ctx, "AIScoreOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
			continue
		}
		op.Result = score
		return nil
	}
	return fmt.Errorf("AIScoreOp: all %d attempts failed; last error: %s", op.maxRetries+1, lastErr)
}

const AIBoolOpDescription = `AIBoolOp: AI-powered yes/no predicate.
  Params:   predicate string — the question to answer about the input (e.g. "does this text contain PII?").
            max_retries string — parse/validation retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
  Inputs:   Input *string.
  Outputs:  Result bool, Reasoning string.`

// AIBoolOp answers a yes/no predicate about the input text.
type AIBoolOp struct {
	Input     *string
	Result    bool
	Reasoning string

	predicate  string
	maxRetries int
	provider   string
	model      string
	caller     aiCaller
}

func (op *AIBoolOp) Setup(params *config.Params) error {
	op.predicate = params.GetString("predicate", "")
	if op.predicate == "" {
		return fmt.Errorf("AIBoolOp: 'predicate' param is required")
	}
	op.maxRetries = 3
	if s := params.GetString("max_retries", ""); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			op.maxRetries = n
		}
	}
	op.provider = params.GetString("provider", "claude")
	op.model = params.GetString("model", "claude-sonnet-4-6")
	caller, err := newAICaller(op.provider, op.model, parseRetryConfig(params))
	if err != nil {
		return fmt.Errorf("AIBoolOp: %w", err)
	}
	op.caller = caller
	return nil
}

func (op *AIBoolOp) Reset() error { return nil }

func (op *AIBoolOp) InputFields() map[string]any {
	return map[string]any{"Input": &op.Input}
}

func (op *AIBoolOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}

func (op *AIBoolOp) SetInputField(field string, value any) error {
	switch field {
	case "Input":
		val, ok := value.(*string)
		if !ok {
			return fmt.Errorf("field Input: expected *string, got %T", value)
		}
		op.Input = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}

func (op *AIBoolOp) ResetFields() {
	op.Input = nil
	op.Result = false
	op.Reasoning = ""
}

func (op *AIBoolOp) Run(ctx context.Context) error {
	slog.DebugContext(ctx, "AIBoolOp.run", "run_id", dagor.RunID(ctx), "model", op.model, "predicate", op.predicate)

	isReasoning := logFromCtx(ctx) != nil

	var basePrompt, systemText string
	if isReasoning {
		basePrompt = fmt.Sprintf(
			"Answer the following question about the text.\n"+
				`Respond with a JSON object: {"result": <true or false>, "reasoning": "<brief explanation>"}.`+"\n"+
				"Question: %s\nText: %s",
			op.predicate, *op.Input,
		)
		systemText = `Respond with only a JSON object: {"result": <true|false>, "reasoning": "<explanation>"}. No other text.`
	} else {
		basePrompt = fmt.Sprintf(
			"Answer the following question about the text with only 'true' or 'false'.\n"+
				"Question: %s\nText: %s",
			op.predicate, *op.Input,
		)
		systemText = "Respond with only 'true' or 'false'."
	}

	prompt := basePrompt
	var lastErr string
	for attempt := 0; attempt <= op.maxRetries; attempt++ {
		maxTokens := int64(8)
		if isReasoning {
			maxTokens = 256
		}
		res, err := op.caller.call(ctx, aiCallRequest{
			SystemText: systemText,
			Prompt:     prompt,
			MaxTokens:  maxTokens,
		})
		if err != nil {
			return fmt.Errorf("generate content: %w", err)
		}
		slog.InfoContext(ctx, "AIBoolOp.tokens", "run_id", dagor.RunID(ctx), "model", op.model, "input_tokens", res.InputTokens, "output_tokens", res.OutputTokens)

		raw := strings.TrimSpace(res.Text)

		if isReasoning {
			var parsed struct {
				Result    bool   `json:"result"`
				Reasoning string `json:"reasoning"`
			}
			if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				lastErr = fmt.Sprintf("expected JSON {result, reasoning}, got %q: %v", raw, err)
				prompt = basePrompt + "\n\nPrevious response was not valid JSON. Respond with only: {\"result\": <true|false>, \"reasoning\": \"<string>\"}."
				slog.DebugContext(ctx, "AIBoolOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
				continue
			}
			op.Result = parsed.Result
			recordReasoning(ctx, "AIBoolOp", map[string]any{
				"Input":     *op.Input,
				"Predicate": op.predicate,
			}, op.Result, parsed.Reasoning)
			return nil
		}

		switch strings.ToLower(raw) {
		case "true":
			op.Result = true
			return nil
		case "false":
			op.Result = false
			return nil
		default:
			lastErr = fmt.Sprintf("expected true or false, got %q", raw)
			prompt = basePrompt + fmt.Sprintf("\n\nPrevious response %q was invalid. Respond with only 'true' or 'false'.", raw)
			slog.DebugContext(ctx, "AIBoolOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
		}
	}
	return fmt.Errorf("AIBoolOp: all %d attempts failed; last error: %s", op.maxRetries+1, lastErr)
}

const AIBestMatchOpDescription = `AIBestMatchOp: AI-powered semantic selection — returns the index of the best-matching candidate.
  Params:   max_retries string — parse/validation retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
  Inputs:   Query *string, Candidates *[]string.
  Outputs:  Result int (0-based index), Reasoning string.`

// AIBestMatchOp selects the best-matching candidate for a query, returning its 0-based index.
type AIBestMatchOp struct {
	Query      *string
	Candidates *[]string
	Result     int
	Reasoning  string

	maxRetries int
	provider   string
	model      string
	caller     aiCaller
}

func (op *AIBestMatchOp) Setup(params *config.Params) error {
	op.maxRetries = 3
	if s := params.GetString("max_retries", ""); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			op.maxRetries = n
		}
	}
	op.provider = params.GetString("provider", "claude")
	op.model = params.GetString("model", "claude-sonnet-4-6")
	caller, err := newAICaller(op.provider, op.model, parseRetryConfig(params))
	if err != nil {
		return fmt.Errorf("AIBestMatchOp: %w", err)
	}
	op.caller = caller
	return nil
}

func (op *AIBestMatchOp) Reset() error { return nil }

func (op *AIBestMatchOp) InputFields() map[string]any {
	return map[string]any{"Query": &op.Query, "Candidates": &op.Candidates}
}

func (op *AIBestMatchOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}

func (op *AIBestMatchOp) SetInputField(field string, value any) error {
	switch field {
	case "Query":
		val, ok := value.(*string)
		if !ok {
			return fmt.Errorf("field Query: expected *string, got %T", value)
		}
		op.Query = val
	case "Candidates":
		val, ok := value.(*[]string)
		if !ok {
			return fmt.Errorf("field Candidates: expected *[]string, got %T", value)
		}
		op.Candidates = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}

func (op *AIBestMatchOp) ResetFields() {
	op.Query = nil
	op.Candidates = nil
	op.Result = 0
	op.Reasoning = ""
}

func (op *AIBestMatchOp) Run(ctx context.Context) error {
	n := len(*op.Candidates)
	if n == 0 {
		return fmt.Errorf("AIBestMatchOp: candidates list is empty")
	}

	isReasoning := logFromCtx(ctx) != nil

	var sb strings.Builder
	for i, c := range *op.Candidates {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i, c))
	}

	var basePrompt, systemText string
	if isReasoning {
		basePrompt = fmt.Sprintf(
			"Given the query, return the 0-based index of the best matching candidate.\n"+
				`Respond with a JSON object: {"index": <integer>, "reasoning": "<brief explanation>"}.`+"\n"+
				"Query: %s\nCandidates:\n%s",
			*op.Query, sb.String(),
		)
		systemText = `Respond with only a JSON object: {"index": <integer>, "reasoning": "<explanation>"}. No other text.`
	} else {
		basePrompt = fmt.Sprintf(
			"Given the query, return the 0-based index of the best matching candidate.\n"+
				"Respond with only the integer index. No explanation.\n"+
				"Query: %s\nCandidates:\n%s",
			*op.Query, sb.String(),
		)
		systemText = "Respond with only an integer index."
	}

	prompt := basePrompt
	var lastErr string
	for attempt := 0; attempt <= op.maxRetries; attempt++ {
		maxTokens := int64(8)
		if isReasoning {
			maxTokens = 256
		}
		res, err := op.caller.call(ctx, aiCallRequest{
			SystemText: systemText,
			Prompt:     prompt,
			MaxTokens:  maxTokens,
		})
		if err != nil {
			return fmt.Errorf("generate content: %w", err)
		}
		slog.InfoContext(ctx, "AIBestMatchOp.tokens", "run_id", dagor.RunID(ctx), "model", op.model, "input_tokens", res.InputTokens, "output_tokens", res.OutputTokens)

		raw := strings.TrimSpace(res.Text)

		var idx int
		if isReasoning {
			var parsed struct {
				Index     int    `json:"index"`
				Reasoning string `json:"reasoning"`
			}
			if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				lastErr = fmt.Sprintf("expected JSON {index, reasoning}, got %q: %v", raw, err)
				prompt = basePrompt + "\n\nPrevious response was not valid JSON. Respond with only: {\"index\": <integer>, \"reasoning\": \"<string>\"}."
				slog.DebugContext(ctx, "AIBestMatchOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
				continue
			}
			if parsed.Index < 0 || parsed.Index >= n {
				lastErr = fmt.Sprintf("index %d out of range [0,%d)", parsed.Index, n)
				prompt = basePrompt + fmt.Sprintf("\n\nIndex %d is out of range. Must be between 0 and %d.", parsed.Index, n-1)
				slog.DebugContext(ctx, "AIBestMatchOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
				continue
			}
			op.Result = parsed.Index
			recordReasoning(ctx, "AIBestMatchOp", map[string]any{
				"Query":      *op.Query,
				"Candidates": *op.Candidates,
			}, op.Result, parsed.Reasoning)
			return nil
		}

		idx, err = strconv.Atoi(raw)
		if err != nil {
			lastErr = fmt.Sprintf("expected integer index, got %q: %v", raw, err)
			prompt = basePrompt + fmt.Sprintf("\n\nPrevious response %q was not a valid integer. Respond with only the integer index.", raw)
			slog.DebugContext(ctx, "AIBestMatchOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
			continue
		}
		if idx < 0 || idx >= n {
			lastErr = fmt.Sprintf("index %d out of range [0,%d)", idx, n)
			prompt = basePrompt + fmt.Sprintf("\n\nIndex %d is out of range. Must be between 0 and %d.", idx, n-1)
			slog.DebugContext(ctx, "AIBestMatchOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
			continue
		}
		op.Result = idx
		return nil
	}
	return fmt.Errorf("AIBestMatchOp: all %d attempts failed; last error: %s", op.maxRetries+1, lastErr)
}

const AIRerankOpDescription = `AIRerankOp: AI-powered reranking — returns a permutation of candidate indices, best first.
  Params:   max_retries string — parse/validation retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
  Inputs:   Query *string, Candidates *[]string.
  Outputs:  Result []int (permutation as CSV), Reasoning string.`

// AIRerankOp reranks candidates by relevance to a query, returning a permutation of 0-based indices.
type AIRerankOp struct {
	Query      *string
	Candidates *[]string
	Result     []int
	Reasoning  string

	maxRetries int
	provider   string
	model      string
	caller     aiCaller
}

func (op *AIRerankOp) Setup(params *config.Params) error {
	op.maxRetries = 3
	if s := params.GetString("max_retries", ""); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			op.maxRetries = n
		}
	}
	op.provider = params.GetString("provider", "claude")
	op.model = params.GetString("model", "claude-sonnet-4-6")
	caller, err := newAICaller(op.provider, op.model, parseRetryConfig(params))
	if err != nil {
		return fmt.Errorf("AIRerankOp: %w", err)
	}
	op.caller = caller
	return nil
}

func (op *AIRerankOp) Reset() error { return nil }

func (op *AIRerankOp) InputFields() map[string]any {
	return map[string]any{"Query": &op.Query, "Candidates": &op.Candidates}
}

func (op *AIRerankOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}

func (op *AIRerankOp) SetInputField(field string, value any) error {
	switch field {
	case "Query":
		val, ok := value.(*string)
		if !ok {
			return fmt.Errorf("field Query: expected *string, got %T", value)
		}
		op.Query = val
	case "Candidates":
		val, ok := value.(*[]string)
		if !ok {
			return fmt.Errorf("field Candidates: expected *[]string, got %T", value)
		}
		op.Candidates = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}

func (op *AIRerankOp) ResetFields() {
	op.Query = nil
	op.Candidates = nil
	op.Result = nil
	op.Reasoning = ""
}

func (op *AIRerankOp) Run(ctx context.Context) error {
	n := len(*op.Candidates)
	if n == 0 {
		return fmt.Errorf("AIRerankOp: candidates list is empty")
	}

	isReasoning := logFromCtx(ctx) != nil

	var sb strings.Builder
	for i, c := range *op.Candidates {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i, c))
	}

	var basePrompt, systemText string
	if isReasoning {
		basePrompt = fmt.Sprintf(
			"Rerank the following candidates by relevance to the query, best first.\n"+
				`Respond with a JSON object: {"indices": "<comma-separated 0-based indices>", "reasoning": "<brief explanation>"}.`+"\n"+
				"Query: %s\nCandidates:\n%s",
			*op.Query, sb.String(),
		)
		systemText = `Respond with only a JSON object: {"indices": "<CSV of integers>", "reasoning": "<explanation>"}. No other text.`
	} else {
		basePrompt = fmt.Sprintf(
			"Rerank the following candidates by relevance to the query, best first.\n"+
				"Respond with only the 0-based indices as a comma-separated list. No explanation.\n"+
				"Query: %s\nCandidates:\n%s",
			*op.Query, sb.String(),
		)
		systemText = "Respond with only a comma-separated list of integers."
	}

	parseIndices := func(csv string) ([]int, string) {
		parts := strings.Split(csv, ",")
		indices := make([]int, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			idx, err := strconv.Atoi(p)
			if err != nil {
				return nil, fmt.Sprintf("expected integer, got %q: %v", p, err)
			}
			indices = append(indices, idx)
		}
		return indices, ""
	}

	validateIndices := func(indices []int) string {
		if len(indices) != n {
			return fmt.Sprintf("expected %d indices, got %d", n, len(indices))
		}
		seen := make(map[int]bool, n)
		for _, idx := range indices {
			if idx < 0 || idx >= n {
				return fmt.Sprintf("index %d out of range [0,%d)", idx, n)
			}
			if seen[idx] {
				return fmt.Sprintf("duplicate index %d", idx)
			}
			seen[idx] = true
		}
		return ""
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
		slog.InfoContext(ctx, "AIRerankOp.tokens", "run_id", dagor.RunID(ctx), "model", op.model, "input_tokens", res.InputTokens, "output_tokens", res.OutputTokens)

		raw := strings.TrimSpace(res.Text)

		var indicesCSV, reasoning string
		if isReasoning {
			var parsed struct {
				Indices   string `json:"indices"`
				Reasoning string `json:"reasoning"`
			}
			if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				lastErr = fmt.Sprintf("expected JSON {indices, reasoning}, got %q: %v", raw, err)
				prompt = basePrompt + "\n\nPrevious response was not valid JSON. Respond with only: {\"indices\": \"<CSV>\", \"reasoning\": \"<string>\"}."
				slog.DebugContext(ctx, "AIRerankOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
				continue
			}
			indicesCSV = parsed.Indices
			reasoning = parsed.Reasoning
		} else {
			indicesCSV = raw
		}

		indices, parseErr := parseIndices(indicesCSV)
		if parseErr != "" {
			lastErr = parseErr
			prompt = basePrompt + fmt.Sprintf("\n\nPrevious response %q was invalid: %s. Respond with comma-separated integers only.", raw, parseErr)
			slog.DebugContext(ctx, "AIRerankOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
			continue
		}
		if valErr := validateIndices(indices); valErr != "" {
			lastErr = valErr
			prompt = basePrompt + fmt.Sprintf("\n\nPrevious response was invalid: %s. Return each index 0-%d exactly once.", valErr, n-1)
			slog.DebugContext(ctx, "AIRerankOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", lastErr)
			continue
		}

		op.Result = indices
		if isReasoning {
			recordReasoning(ctx, "AIRerankOp", map[string]any{
				"Query":      *op.Query,
				"Candidates": *op.Candidates,
			}, op.Result, reasoning)
		}
		return nil
	}
	return fmt.Errorf("AIRerankOp: all %d attempts failed; last error: %s", op.maxRetries+1, lastErr)
}

func init() {
	operator.RegisterOp[AIExtractStringSliceOp]()
	operator.RegisterOp[AIExtractMapOp]()
	operator.RegisterOp[AIParseNumberOp]()
	operator.RegisterOp[AISummarizeOp]()
	operator.RegisterOp[AIClassifyMultiLabelOp]()
	operator.RegisterOp[AIScoreOp]()
	operator.RegisterOp[AIBoolOp]()
	operator.RegisterOp[AIBestMatchOp]()
	operator.RegisterOp[AIRerankOp]()
}
