package library

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/operator"
)

const ModeSelectOpDescription = `ModeSelectOp: AI-powered classifier — maps arbitrary input text to exactly one of a fixed set of categories.
  Params:   categories string — comma-separated list of valid output values (e.g. "arithmetic expression,city name").
            max_retries string — parse/validation retries (default "3").
  Inputs:   Input *string — the text to classify.
  Outputs:  Result string — exactly one of the specified categories.
            Reasoning string.`

// ModeSelectOp classifies an arbitrary input string into one of a fixed set of
// categories using an AI call. Use it at the top of a multi-branch workflow to
// dispatch to the correct branch based on what the input represents.
type ModeSelectOp struct {
	Input     *string `dag:"input"`
	Result    string  `dag:"output"`
	Reasoning string  `dag:"output"`

	categories []string
	maxRetries int
}

func (op *ModeSelectOp) Setup(params *config.Params) error {
	raw := params.GetString("categories", "")
	if raw == "" {
		return fmt.Errorf("ModeSelectOp: 'categories' param is required")
	}
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
	return nil
}

func (op *ModeSelectOp) Reset() error { return nil }

func (op *ModeSelectOp) Run(ctx context.Context) error {
	log.Printf("[DEBUG] ModeSelectOp: classifying %q into %v", *op.Input, op.categories)

	apiKey := os.Getenv("CLAUDE_API_KEY")
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	catList := strings.Join(op.categories, ", ")
	basePrompt := fmt.Sprintf(
		"Classify the following input as exactly one of these categories: %s.\n"+
			"Respond with only the category name — no other text.\n"+
			"Input: %s",
		catList, *op.Input,
	)

	catSet := make(map[string]bool, len(op.categories))
	for _, c := range op.categories {
		catSet[c] = true
	}

	prompt := basePrompt
	var lastErr string
	for attempt := 0; attempt <= op.maxRetries; attempt++ {
		msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeSonnet4_6,
			MaxTokens: 64,
			System: []anthropic.TextBlockParam{
				{Text: "Respond with only the requested value. No explanation, no punctuation, no formatting."},
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

		result := strings.TrimSpace(raw)
		if !catSet[result] {
			lastErr = fmt.Sprintf("result %q is not one of %v", result, op.categories)
			prompt = basePrompt + fmt.Sprintf("\n\nPrevious result %q was invalid — must be exactly one of: %s.", result, catList)
			log.Printf("[DEBUG] ModeSelectOp: attempt %d invalid category: %s", attempt+1, lastErr)
			continue
		}
		op.Result = result
		log.Printf("[DEBUG] ModeSelectOp: result=%q", op.Result)
		return nil
	}
	return fmt.Errorf("ModeSelectOp: all %d attempts failed; last error: %s", op.maxRetries+1, lastErr)
}

func init() {
	operator.RegisterOp[ModeSelectOp]()
}
