package library

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
)

//go:embed prompts/ai_compute_base.md
var aiComputeBaseTemplate string

//go:embed prompts/ai_compute_retry.md
var aiComputeRetryTemplate string

//go:embed prompts/format_float64.md
var promptFormatFloat64 string

//go:embed prompts/format_int.md
var promptFormatInt string

//go:embed prompts/format_string.md
var promptFormatString string

//go:embed prompts/format_float64_slice.md
var promptFormatFloat64Slice string

//go:embed prompts/format_string_slice.md
var promptFormatStringSlice string

//go:embed prompts/format_bool.md
var promptFormatBool string

//go:embed prompts/format_map_string_string.md
var promptFormatMapStringString string

//go:embed prompts/format_int_slice.md
var promptFormatIntSlice string

//go:embed prompts/format_default.md
var promptFormatDefault string

// AIInputFormatter is an optional interface for In types to describe themselves in prompts.
type AIInputFormatter interface {
	FormatForPrompt() string
}

// AIOutputFormatter is an optional interface for Out types to describe the expected response format.
type AIOutputFormatter interface {
	ExpectedFormat() string
}

// AIResponseParser must be implemented by Out types that are structs (non-scalar, non-slice).
type AIResponseParser interface {
	ParseAIResponse(response string) error
}

// AIComputeOp is a generic AI-powered compute operator.
// Vertex params: provider ("claude"|"gemini", default "claude"), model (default "claude-sonnet-4-6").
// In is the input type, Out is the output type.
// Do not register AIComputeOp directly — use a concrete variant like AIComputeMathOperandsToFloat64Op.
type AIComputeOp[In, Out any] struct {
	Input     *In    // single strongly-typed input
	Result    Out    // single strongly-typed output
	Reasoning string // always present

	operation  string
	maxRetries int
	provider   string
	model      string
	caller     aiCaller
}

func (op *AIComputeOp[In, Out]) Setup(params *config.Params) error {
	op.operation = params.GetString("operation", "")
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
		return fmt.Errorf("AIComputeOp: %w", err)
	}
	op.caller = caller
	return nil
}

func (op *AIComputeOp[In, Out]) Reset() error { return nil }

func (op *AIComputeOp[In, Out]) InputFields() map[string]any {
	return map[string]any{
		"Input": &op.Input,
	}
}

func (op *AIComputeOp[In, Out]) OutputFields() map[string]any {
	return map[string]any{
		"Result": &op.Result,
	}
}

func (op *AIComputeOp[In, Out]) SetInputField(field string, value any) error {
	switch field {
	case "Input":
		val, ok := value.(*In)
		if !ok {
			return fmt.Errorf("field Input: expected *%T, got %T", op.Input, value)
		}
		op.Input = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}

func (op *AIComputeOp[In, Out]) ResetFields() {
	var zeroInput *In
	op.Input = zeroInput
	var zeroResult Out
	op.Result = zeroResult
	op.Reasoning = ""
}

func (op *AIComputeOp[In, Out]) Run(ctx context.Context) error {
	slog.DebugContext(ctx, "AIComputeOp.run", "run_id", dagor.RunID(ctx), "model", op.model, "operation", op.operation)

	isReasoning := logFromCtx(ctx) != nil

	// Build input description.
	var inputDesc string
	if op.Input != nil {
		if f, ok := any(op.Input).(AIInputFormatter); ok {
			inputDesc = f.FormatForPrompt()
		} else if sp, ok := any(op.Input).(*[]string); ok {
			lines := make([]string, len(*sp))
			for i, s := range *sp {
				lines[i] = fmt.Sprintf("%d. %s", i+1, s)
			}
			inputDesc = strings.Join(lines, "\n")
		} else {
			inputDesc = fmt.Sprintf("%+v", *op.Input)
		}
	}

	// Build output format description.
	var formatDesc string
	var zeroOut Out
	if f, ok := any(&zeroOut).(AIOutputFormatter); ok {
		formatDesc = f.ExpectedFormat()
	} else {
		formatDesc = op.builtinFormatDescription()
	}

	basePrompt := strings.NewReplacer(
		"{{OPERATION}}", op.operation,
		"{{INPUT}}", inputDesc,
		"{{FORMAT}}", formatDesc,
	).Replace(aiComputeBaseTemplate)

	var systemText string
	if isReasoning {
		systemText = `Respond with a JSON object {"result": <your answer in the format described>, "reasoning": "<brief explanation>"}. No markdown, no other text.`
	} else {
		systemText = "Respond only with the requested format. Do not include any explanation or markdown formatting."
	}

	// State across attempts:
	//   - Stateless retry (default): each attempt sends basePrompt plus an
	//     optional retry-template addendum built from prevResponse/prevErr.
	//   - Conversational repair: triggered the first time parseResult returns
	//     *ErrRepairable. Once entered, subsequent attempts grow `history` and
	//     send `nextPrompt` as the latest user turn, threading the model's
	//     reasoning across turns of the same conversation.
	var prevResponse, prevErr string
	var history []aiTurn
	var nextPrompt string
	conversational := false

	for attempt := 0; attempt <= op.maxRetries; attempt++ {
		var sentPrompt string
		var sendHistory []aiTurn
		if conversational {
			sentPrompt = nextPrompt
			sendHistory = history
		} else {
			sentPrompt = basePrompt
			if prevResponse != "" {
				sentPrompt += "\n" + strings.NewReplacer(
					"{{PREVIOUS_RESPONSE}}", prevResponse,
					"{{PARSE_ERROR}}", prevErr,
				).Replace(aiComputeRetryTemplate)
			}
		}

		res, err := op.caller.call(ctx, aiCallRequest{
			SystemText: systemText,
			Prompt:     sentPrompt,
			History:    sendHistory,
			MaxTokens:  16 * 1024,
		})
		if err != nil {
			return fmt.Errorf("generate content: %w", err)
		}
		slog.InfoContext(ctx, "AIComputeOp.tokens", "run_id", dagor.RunID(ctx), "model", op.model, "input_tokens", res.InputTokens, "output_tokens", res.OutputTokens)

		raw := strings.TrimSpace(res.Text)

		// envelopeFailed records an envelope/format failure for retry feedback.
		// When conversational, append a corrective user turn; otherwise fall
		// back to the stateless retry template via prevResponse/prevErr.
		envelopeFailed := func(detail string) {
			if conversational {
				history = append(history, aiTurn{Role: "user", Text: sentPrompt})
				history = append(history, aiTurn{Role: "assistant", Text: raw})
				nextPrompt = "Your last response could not be parsed: " + detail + ". Please try again, following the original format exactly."
				slog.DebugContext(ctx, "AIComputeOp.repair.format", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", detail)
				return
			}
			prevResponse = raw
			prevErr = detail
			slog.DebugContext(ctx, "AIComputeOp.retry", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "error", detail)
		}

		var resultStr, reasoning string
		if isReasoning {
			var envelope struct {
				Result    json.RawMessage `json:"result"`
				Reasoning string          `json:"reasoning"`
			}
			if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
				envelopeFailed(fmt.Sprintf("expected JSON {result, reasoning}, got %q: %v", raw, err))
				continue
			}
			// Unwrap result: JSON string → Go string (handles escaping); otherwise use raw bytes.
			if len(envelope.Result) > 0 && envelope.Result[0] == '"' {
				if err := json.Unmarshal(envelope.Result, &resultStr); err != nil {
					envelopeFailed(fmt.Sprintf("could not decode result field %q: %v", string(envelope.Result), err))
					continue
				}
			} else {
				resultStr = strings.TrimSpace(string(envelope.Result))
			}
			reasoning = envelope.Reasoning
		} else {
			resultStr = raw
		}

		if parseErr := op.parseResult(resultStr); parseErr != nil {
			var rep *ErrRepairable
			if errors.As(parseErr, &rep) {
				// Enter / continue conversational repair.
				history = append(history, aiTurn{Role: "user", Text: sentPrompt})
				history = append(history, aiTurn{Role: "assistant", Text: raw})
				nextPrompt = rep.Prompt
				conversational = true
				slog.DebugContext(ctx, "AIComputeOp.repair.semantic", "run_id", dagor.RunID(ctx), "attempt", attempt+1, "cause", rep.Cause)
				continue
			}
			envelopeFailed(parseErr.Error())
			continue
		}

		if isReasoning {
			recordReasoning(ctx, "AIComputeOp", map[string]any{
				"Operation": op.operation,
				"Input":     inputDesc,
			}, op.Result, reasoning)
		}
		return nil
	}

	if conversational {
		return fmt.Errorf("AIComputeOp: all %d attempts failed in conversational repair; last prompt: %s", op.maxRetries+1, nextPrompt)
	}
	return fmt.Errorf("AIComputeOp: all %d attempts failed; last error: %s", op.maxRetries+1, prevErr)
}

func (op *AIComputeOp[In, Out]) builtinFormatDescription() string {
	var zeroOut Out
	switch any(&zeroOut).(type) {
	case *float64:
		return strings.TrimSpace(promptFormatFloat64)
	case *int:
		return strings.TrimSpace(promptFormatInt)
	case *string:
		return strings.TrimSpace(promptFormatString)
	case *[]float64:
		return strings.TrimSpace(promptFormatFloat64Slice)
	case *[]string:
		return strings.TrimSpace(promptFormatStringSlice)
	case *bool:
		return strings.TrimSpace(promptFormatBool)
	case *map[string]string:
		return strings.TrimSpace(promptFormatMapStringString)
	case *[]int:
		return strings.TrimSpace(promptFormatIntSlice)
	default:
		return strings.TrimSpace(promptFormatDefault)
	}
}

func (op *AIComputeOp[In, Out]) parseResult(raw string) error {
	raw = strings.TrimSpace(raw)
	switch v := any(&op.Result).(type) {
	case *float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("expected float64, got %q: %w", raw, err)
		}
		*v = f
	case *int:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("expected int, got %q: %w", raw, err)
		}
		*v = n
	case *string:
		*v = raw
	case *[]float64:
		if raw == "" {
			*v = nil
			return nil
		}
		parts := strings.Split(raw, ",")
		s := make([]float64, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			f, err := strconv.ParseFloat(p, 64)
			if err != nil {
				return fmt.Errorf("expected []float64 CSV, got %q: %w", raw, err)
			}
			s = append(s, f)
		}
		*v = s
	case *[]string:
		if raw == "" {
			*v = nil
			return nil
		}
		parts := strings.Split(raw, ",")
		s := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				s = append(s, p)
			}
		}
		*v = s
	case *bool:
		switch strings.ToLower(raw) {
		case "true", "yes":
			*v = true
		case "false", "no":
			*v = false
		default:
			return fmt.Errorf("expected bool (true/false), got %q", raw)
		}
	case *map[string]string:
		if raw == "" {
			*v = map[string]string{}
			return nil
		}
		m := make(map[string]string)
		for _, pair := range strings.Split(raw, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			idx := strings.IndexByte(pair, '=')
			if idx < 0 {
				return fmt.Errorf("expected key=value pair, got %q", pair)
			}
			m[strings.TrimSpace(pair[:idx])] = strings.TrimSpace(pair[idx+1:])
		}
		*v = m
	case *[]int:
		if raw == "" {
			*v = nil
			return nil
		}
		parts := strings.Split(raw, ",")
		s := make([]int, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			n, err := strconv.Atoi(p)
			if err != nil {
				return fmt.Errorf("expected []int CSV, got %q: %w", raw, err)
			}
			s = append(s, n)
		}
		*v = s
	case AIResponseParser:
		return v.ParseAIResponse(raw)
	default:
		// Fallback: attempt JSON unmarshal for unknown types that implement json.Unmarshaler
		if err := json.Unmarshal([]byte(raw), &op.Result); err != nil {
			return fmt.Errorf("unsupported output type %T; implement AIResponseParser", op.Result)
		}
	}
	return nil
}
