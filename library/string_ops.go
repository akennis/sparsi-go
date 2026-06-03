package library

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

const StringLookupOpDescription = `StringLookupOp: looks up Key in a hardcoded string→string map; returns "" on miss.
  Params: map — JSON-encoded key→value pairs (e.g. {"hamburger":"ketchup","hotdog":"mustard"}).
  Input:  Key *string.
  Output: Result string (empty string if key not found).`

const AIComputeStringToStringOpDescription = `AIComputeStringToStringOp: AI-powered string→string computation.
  Params:   operation string — plain-English description (e.g. "suggest a condiment that pairs with the given food").
            max_retries string — parse retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            api_factory_timeout_ms string — deadline for AIClientFactory credential lookup in milliseconds (default "30000"; "0" disables).
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
            credential_ref string — opaque credential identifier passed to AIClientFactory (default ""; ignored by the bundled env-var factory).
            client_factory_id string — selects a registered AIClientFactory by id (default "" → process default).
  Inputs:   Input *string — the query string.
  Outputs:  Result string, Reasoning string.`

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

const StringConcatOpDescription = "StringConcatOp: concatenates two strings. Inputs: A *string, B *string. Output: Result string."
const StringSplitOpDescription = `StringSplitOp: splits a string by a separator. Param: sep (default ","). Input: Input *string. Output: Result []string.`
const RegexMatchOpDescription = `RegexMatchOp: reports whether the input matches a compiled regex. Param: pattern (required). Input: Input *string. Output: Match bool.`
const RegexExtractOpDescription = `RegexExtractOp: returns the first match (or submatch group 1 if present) of a regex. Param: pattern (required). Input: Input *string. Output: Result string (empty if no match).`

type StringConcatOp struct {
	A      *string `dag:"input"`
	B      *string `dag:"input"`
	Result string  `dag:"output"`
}

func (op *StringConcatOp) Setup(_ *config.Params) error { return nil }
func (op *StringConcatOp) Reset() error                 { return nil }
func (op *StringConcatOp) Run(_ context.Context) error {
	op.Result = *op.A + *op.B
	return nil
}

type StringSplitOp struct {
	Input  *string
	Result []string
	sep    string
}

func (op *StringSplitOp) Setup(params *config.Params) error {
	op.sep = params.GetString("sep", ",")
	return nil
}
func (op *StringSplitOp) Reset() error { return nil }
func (op *StringSplitOp) Run(_ context.Context) error {
	parts := strings.Split(*op.Input, op.sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	op.Result = out
	return nil
}
func (op *StringSplitOp) InputFields() map[string]any { return map[string]any{"Input": &op.Input} }
func (op *StringSplitOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *StringSplitOp) SetInputField(field string, value any) error {
	if field != "Input" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*string)
	if !ok {
		return fmt.Errorf("field Input: expected *string, got %T", value)
	}
	op.Input = val
	return nil
}
func (op *StringSplitOp) ResetFields() { op.Input = nil; op.Result = nil }

type RegexMatchOp struct {
	Input *string
	Match bool
	re    *regexp.Regexp
}

func (op *RegexMatchOp) Setup(params *config.Params) error {
	pattern := params.GetString("pattern", "")
	if pattern == "" {
		return fmt.Errorf("RegexMatchOp: pattern param is required")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("RegexMatchOp: invalid pattern %q: %w", pattern, err)
	}
	op.re = re
	return nil
}
func (op *RegexMatchOp) Reset() error { return nil }
func (op *RegexMatchOp) Run(_ context.Context) error {
	op.Match = op.re.MatchString(*op.Input)
	return nil
}
func (op *RegexMatchOp) InputFields() map[string]any { return map[string]any{"Input": &op.Input} }
func (op *RegexMatchOp) OutputFields() map[string]any { return map[string]any{"Match": &op.Match} }
func (op *RegexMatchOp) SetInputField(field string, value any) error {
	if field != "Input" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*string)
	if !ok {
		return fmt.Errorf("field Input: expected *string, got %T", value)
	}
	op.Input = val
	return nil
}
func (op *RegexMatchOp) ResetFields() { op.Input = nil; op.Match = false }

type RegexExtractOp struct {
	Input  *string `dag:"input"`
	Result string  `dag:"output"`
	re     *regexp.Regexp
}

func (op *RegexExtractOp) Setup(params *config.Params) error {
	pattern := params.GetString("pattern", "")
	if pattern == "" {
		return fmt.Errorf("RegexExtractOp: pattern param is required")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("RegexExtractOp: invalid pattern %q: %w", pattern, err)
	}
	op.re = re
	return nil
}
func (op *RegexExtractOp) Reset() error { return nil }
func (op *RegexExtractOp) Run(_ context.Context) error {
	m := op.re.FindStringSubmatch(*op.Input)
	if len(m) == 0 {
		op.Result = ""
	} else if len(m) > 1 {
		op.Result = m[1]
	} else {
		op.Result = m[0]
	}
	return nil
}

// AIComputeStringToStringOp is the registered concrete variant of AIComputeOp
// for string→string operations.
type AIComputeStringToStringOp struct {
	AIComputeOp[string, string]
}

func init() {
	operator.RegisterOp[StringLookupOp]()
	operator.RegisterOp[StringToLowerOp]()
	operator.RegisterOp[StringConcatOp]()
	operator.RegisterOp[StringSplitOp]()
	operator.RegisterOp[RegexMatchOp]()
	operator.RegisterOp[RegexExtractOp]()
	operator.RegisterOp[AIComputeStringToStringOp]()
}
