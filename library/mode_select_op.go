package library

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/operator"
	"google.golang.org/api/option"
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

	apiKey := os.Getenv("GOOGLE_GENAI_API_KEY")
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return fmt.Errorf("genai client: %w", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-flash-latest")
	model.ResponseMIMEType = "application/json"
	model.ResponseSchema = &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"result":    {Type: genai.TypeString},
			"reasoning": {Type: genai.TypeString},
		},
		Required: []string{"result", "reasoning"},
	}

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
		resp, err := model.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			return fmt.Errorf("generate content: %w", err)
		}
		if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
			return fmt.Errorf("no candidates in response")
		}
		var raw string
		for _, part := range resp.Candidates[0].Content.Parts {
			raw += fmt.Sprintf("%v", part)
		}
		var envelope struct {
			Result    string `json:"result"`
			Reasoning string `json:"reasoning"`
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			lastErr = fmt.Sprintf("invalid JSON: %v", err)
			prompt = basePrompt + fmt.Sprintf("\n\nPrevious response was not valid JSON (%s). Try again.", lastErr)
			log.Printf("[DEBUG] ModeSelectOp: attempt %d parse failed: %v", attempt+1, lastErr)
			continue
		}
		result := strings.TrimSpace(envelope.Result)
		if !catSet[result] {
			lastErr = fmt.Sprintf("result %q is not one of %v", result, op.categories)
			prompt = basePrompt + fmt.Sprintf("\n\nPrevious result %q was invalid — must be exactly one of: %s.", result, catList)
			log.Printf("[DEBUG] ModeSelectOp: attempt %d invalid category: %s", attempt+1, lastErr)
			continue
		}
		op.Result = result
		op.Reasoning = envelope.Reasoning
		log.Printf("[DEBUG] ModeSelectOp: result=%q reasoning=%q", op.Result, op.Reasoning)
		return nil
	}
	return fmt.Errorf("ModeSelectOp: all %d attempts failed; last error: %s", op.maxRetries+1, lastErr)
}

func init() {
	operator.RegisterOp[ModeSelectOp]()
}
