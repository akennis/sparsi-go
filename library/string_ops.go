package library

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/operator"
)

const StringConstOpDescription = "StringConstOp: injects a constant string value. Params: Value string. Output: Result string."

const StringLookupOpDescription = `StringLookupOp: looks up Key in a hardcoded string→string map; returns "" on miss.
  Params: map — JSON-encoded key→value pairs (e.g. {"hamburger":"ketchup","hotdog":"mustard"}).
  Input:  Key *string.
  Output: Result string (empty string if key not found).`

const AIComputeStringToStringOpDescription = `AIComputeStringToStringOp: AI-powered string→string computation with optional deterministic passthrough.
  Params:   operation string — plain-English description (e.g. "suggest a condiment that pairs with the given food").
            max_retries string — parse retries (default "3").
  Inputs:   Input *string — the query string.
            SkipIf *string — optional; connect to a StringLookupOp Result wire.
                             If non-empty at runtime, the AI call is skipped and SkipIf is used as Result directly.
  Outputs:  Result string, Reasoning string.`

// StringConstOp injects a constant string value into the graph.
type StringConstOp struct {
	Result string `dag:"output"`
	value  string
}

func (op *StringConstOp) Setup(params *config.Params) error {
	op.value = params.GetString("Value", "")
	return nil
}
func (op *StringConstOp) Reset() error { return nil }
func (op *StringConstOp) Run(ctx context.Context) error {
	op.Result = op.value
	log.Printf("[DEBUG] StringConstOp: value=%q", op.Result)
	return nil
}

// StringLookupOp looks up Key in a params-configured map.
// Returns "" when the key is not found, acting as the "no deterministic result" sentinel.
type StringLookupOp struct {
	Key     *string `dag:"input"`
	Result  string  `dag:"output"`
	entries map[string]string
}

func (op *StringLookupOp) Setup(params *config.Params) error {
	raw := params.GetString("map", "{}")
	if raw == "" {
		raw = "{}"
	}
	if err := json.Unmarshal([]byte(raw), &op.entries); err != nil {
		return fmt.Errorf("StringLookupOp: invalid map param: %w", err)
	}
	return nil
}
func (op *StringLookupOp) Reset() error { return nil }
func (op *StringLookupOp) Run(ctx context.Context) error {
	if op.Key == nil {
		op.Result = ""
		return nil
	}
	op.Result = op.entries[*op.Key]
	log.Printf("[DEBUG] StringLookupOp: key=%q result=%q", *op.Key, op.Result)
	return nil
}

const StringToLowerOpDescription = "StringToLowerOp: converts a string to lowercase. Input: Value *string. Output: Result string."

// StringToLowerOp converts its input string to lowercase.
type StringToLowerOp struct {
	Value  *string `dag:"input"`
	Result string  `dag:"output"`
}

func (op *StringToLowerOp) Setup(_ *config.Params) error { return nil }
func (op *StringToLowerOp) Reset() error                  { return nil }
func (op *StringToLowerOp) Run(_ context.Context) error {
	if op.Value != nil {
		op.Result = strings.ToLower(*op.Value)
	}
	return nil
}

// AIComputeStringToStringOp is the registered concrete variant of AIComputeOp
// for string→string operations with optional deterministic passthrough via SkipIf.
type AIComputeStringToStringOp struct {
	AIComputeOp[string, string]
}

func init() {
	operator.RegisterOp[StringConstOp]()
	operator.RegisterOp[StringLookupOp]()
	operator.RegisterOp[StringToLowerOp]()
	operator.RegisterOp[AIComputeStringToStringOp]()
}
