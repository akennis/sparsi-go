package library

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
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
// In is the input type, Out is the output type.
// Do not register AIComputeOp directly — use a concrete variant like AIComputeMathOperandsToFloat64Op.
type AIComputeOp[In, Out any] struct {
	Input     *In    // single strongly-typed input
	Result    Out    // single strongly-typed output
	Reasoning string // always present

	operation  string
	maxRetries int
}

func (op *AIComputeOp[In, Out]) Setup(params *config.Params) error {
	op.operation = params.GetString("operation", "")
	op.maxRetries = 3
	if s := params.GetString("max_retries", ""); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			op.maxRetries = n
		}
	}
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
		"Result":    &op.Result,
		"Reasoning": &op.Reasoning,
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
	log.Printf("[DEBUG] AIComputeOp[%T]: operation=%q", op.Result, op.operation)

	apiKey := os.Getenv("CLAUDE_API_KEY")
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	// Build input description.
	var inputDesc string
	if op.Input != nil {
		if f, ok := any(op.Input).(AIInputFormatter); ok {
			inputDesc = f.FormatForPrompt()
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

	var prevResponse, prevErr string
	for attempt := 0; attempt <= op.maxRetries; attempt++ {
		prompt := basePrompt
		if prevResponse != "" {
			prompt += "\n" + strings.NewReplacer(
				"{{PREVIOUS_RESPONSE}}", prevResponse,
				"{{PARSE_ERROR}}", prevErr,
			).Replace(aiComputeRetryTemplate)
		}

		msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeSonnet4_6,
			MaxTokens: 1024,
			System: []anthropic.TextBlockParam{
				{Text: "Respond only with the requested format. Do not include any explanation or markdown formatting."},
			},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		})
		if err != nil {
			return fmt.Errorf("generate content: %w", err)
		}

		var raw string
		for _, block := range msg.Content {
			if block.Type == "text" {
				raw += block.Text
			}
		}

		if parseErr := op.parseResult(strings.TrimSpace(raw)); parseErr != nil {
			prevResponse = raw
			prevErr = parseErr.Error()
			log.Printf("[DEBUG] AIComputeOp: attempt %d result parse failed: %v", attempt+1, parseErr)
			continue
		}

		log.Printf("[DEBUG] AIComputeOp: result=%v", op.Result)
		return nil
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
		var s []float64
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return fmt.Errorf("expected []float64, got %q: %w", raw, err)
		}
		*v = s
	case *[]string:
		var s []string
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return fmt.Errorf("expected []string, got %q: %w", raw, err)
		}
		*v = s
	case AIResponseParser:
		return v.ParseAIResponse(raw)
	default:
		return fmt.Errorf("unsupported output type %T; implement AIResponseParser", op.Result)
	}
	return nil
}
