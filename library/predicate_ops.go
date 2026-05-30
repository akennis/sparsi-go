package library

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

// ============================================================================
// Float comparisons (tag-based)
// ============================================================================

const IfFloatGtOpDescription = "IfFloatGtOp: reports whether A > B. Inputs: A *float64, B *float64. Output: Match bool."
const IfFloatLtOpDescription = "IfFloatLtOp: reports whether A < B. Inputs: A *float64, B *float64. Output: Match bool."
const IfFloatEqOpDescription = "IfFloatEqOp: reports whether A == B. Inputs: A *float64, B *float64. Output: Match bool."
const IfFloatGeOpDescription = "IfFloatGeOp: reports whether A >= B. Inputs: A *float64, B *float64. Output: Match bool."
const IfFloatLeOpDescription = "IfFloatLeOp: reports whether A <= B. Inputs: A *float64, B *float64. Output: Match bool."

type IfFloatGtOp struct {
	A     *float64 `dag:"input"`
	B     *float64 `dag:"input"`
	Match bool     `dag:"output"`
}

func (op *IfFloatGtOp) Setup(_ *config.Params) error { return nil }
func (op *IfFloatGtOp) Reset() error                 { return nil }
func (op *IfFloatGtOp) Run(_ context.Context) error {
	op.Match = *op.A > *op.B
	return nil
}

type IfFloatLtOp struct {
	A     *float64 `dag:"input"`
	B     *float64 `dag:"input"`
	Match bool     `dag:"output"`
}

func (op *IfFloatLtOp) Setup(_ *config.Params) error { return nil }
func (op *IfFloatLtOp) Reset() error                 { return nil }
func (op *IfFloatLtOp) Run(_ context.Context) error {
	op.Match = *op.A < *op.B
	return nil
}

type IfFloatEqOp struct {
	A     *float64 `dag:"input"`
	B     *float64 `dag:"input"`
	Match bool     `dag:"output"`
}

func (op *IfFloatEqOp) Setup(_ *config.Params) error { return nil }
func (op *IfFloatEqOp) Reset() error                 { return nil }
func (op *IfFloatEqOp) Run(_ context.Context) error {
	op.Match = *op.A == *op.B
	return nil
}

type IfFloatGeOp struct {
	A     *float64 `dag:"input"`
	B     *float64 `dag:"input"`
	Match bool     `dag:"output"`
}

func (op *IfFloatGeOp) Setup(_ *config.Params) error { return nil }
func (op *IfFloatGeOp) Reset() error                 { return nil }
func (op *IfFloatGeOp) Run(_ context.Context) error {
	op.Match = *op.A >= *op.B
	return nil
}

type IfFloatLeOp struct {
	A     *float64 `dag:"input"`
	B     *float64 `dag:"input"`
	Match bool     `dag:"output"`
}

func (op *IfFloatLeOp) Setup(_ *config.Params) error { return nil }
func (op *IfFloatLeOp) Reset() error                 { return nil }
func (op *IfFloatLeOp) Run(_ context.Context) error {
	op.Match = *op.A <= *op.B
	return nil
}

// ============================================================================
// Int comparisons (tag-based)
// ============================================================================

const IfIntGtOpDescription = "IfIntGtOp: reports whether A > B. Inputs: A *int, B *int. Output: Match bool."
const IfIntLtOpDescription = "IfIntLtOp: reports whether A < B. Inputs: A *int, B *int. Output: Match bool."
const IfIntEqOpDescription = "IfIntEqOp: reports whether A == B. Inputs: A *int, B *int. Output: Match bool."
const IfIntGeOpDescription = "IfIntGeOp: reports whether A >= B. Inputs: A *int, B *int. Output: Match bool."
const IfIntLeOpDescription = "IfIntLeOp: reports whether A <= B. Inputs: A *int, B *int. Output: Match bool."

type IfIntGtOp struct {
	A     *int `dag:"input"`
	B     *int `dag:"input"`
	Match bool `dag:"output"`
}

func (op *IfIntGtOp) Setup(_ *config.Params) error { return nil }
func (op *IfIntGtOp) Reset() error                 { return nil }
func (op *IfIntGtOp) Run(_ context.Context) error {
	op.Match = *op.A > *op.B
	return nil
}

type IfIntLtOp struct {
	A     *int `dag:"input"`
	B     *int `dag:"input"`
	Match bool `dag:"output"`
}

func (op *IfIntLtOp) Setup(_ *config.Params) error { return nil }
func (op *IfIntLtOp) Reset() error                 { return nil }
func (op *IfIntLtOp) Run(_ context.Context) error {
	op.Match = *op.A < *op.B
	return nil
}

type IfIntEqOp struct {
	A     *int `dag:"input"`
	B     *int `dag:"input"`
	Match bool `dag:"output"`
}

func (op *IfIntEqOp) Setup(_ *config.Params) error { return nil }
func (op *IfIntEqOp) Reset() error                 { return nil }
func (op *IfIntEqOp) Run(_ context.Context) error {
	op.Match = *op.A == *op.B
	return nil
}

type IfIntGeOp struct {
	A     *int `dag:"input"`
	B     *int `dag:"input"`
	Match bool `dag:"output"`
}

func (op *IfIntGeOp) Setup(_ *config.Params) error { return nil }
func (op *IfIntGeOp) Reset() error                 { return nil }
func (op *IfIntGeOp) Run(_ context.Context) error {
	op.Match = *op.A >= *op.B
	return nil
}

type IfIntLeOp struct {
	A     *int `dag:"input"`
	B     *int `dag:"input"`
	Match bool `dag:"output"`
}

func (op *IfIntLeOp) Setup(_ *config.Params) error { return nil }
func (op *IfIntLeOp) Reset() error                 { return nil }
func (op *IfIntLeOp) Run(_ context.Context) error {
	op.Match = *op.A <= *op.B
	return nil
}

// ============================================================================
// String predicates
// ============================================================================

const IfStringContainsOpDescription = "IfStringContainsOp: reports whether A contains B as a substring. Inputs: A *string, B *string. Output: Match bool."
const IfStringHasPrefixOpDescription = "IfStringHasPrefixOp: reports whether A starts with B. Inputs: A *string, B *string. Output: Match bool."
const IfStringHasSuffixOpDescription = "IfStringHasSuffixOp: reports whether A ends with B. Inputs: A *string, B *string. Output: Match bool."
const IfStringRegexMatchOpDescription = `IfStringRegexMatchOp: reports whether the input matches a compiled regex. Param: pattern (required). Input: Input *string. Output: Match bool.`

type IfStringContainsOp struct {
	A     *string `dag:"input"`
	B     *string `dag:"input"`
	Match bool    `dag:"output"`
}

func (op *IfStringContainsOp) Setup(_ *config.Params) error { return nil }
func (op *IfStringContainsOp) Reset() error                 { return nil }
func (op *IfStringContainsOp) Run(_ context.Context) error {
	op.Match = strings.Contains(*op.A, *op.B)
	return nil
}

type IfStringHasPrefixOp struct {
	A     *string `dag:"input"`
	B     *string `dag:"input"`
	Match bool    `dag:"output"`
}

func (op *IfStringHasPrefixOp) Setup(_ *config.Params) error { return nil }
func (op *IfStringHasPrefixOp) Reset() error                 { return nil }
func (op *IfStringHasPrefixOp) Run(_ context.Context) error {
	op.Match = strings.HasPrefix(*op.A, *op.B)
	return nil
}

type IfStringHasSuffixOp struct {
	A     *string `dag:"input"`
	B     *string `dag:"input"`
	Match bool    `dag:"output"`
}

func (op *IfStringHasSuffixOp) Setup(_ *config.Params) error { return nil }
func (op *IfStringHasSuffixOp) Reset() error                 { return nil }
func (op *IfStringHasSuffixOp) Run(_ context.Context) error {
	op.Match = strings.HasSuffix(*op.A, *op.B)
	return nil
}

// IfStringRegexMatchOp mirrors RegexMatchOp; hand-rolled because the pattern
// param compiles to an unexported *regexp.Regexp and the existing convention
// for regex ops here is hand-rolled.
type IfStringRegexMatchOp struct {
	Input *string
	Match bool
	re    *regexp.Regexp
}

func (op *IfStringRegexMatchOp) Setup(params *config.Params) error {
	pattern := params.GetString("pattern", "")
	if pattern == "" {
		return fmt.Errorf("IfStringRegexMatchOp: pattern param is required")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("IfStringRegexMatchOp: invalid pattern %q: %w", pattern, err)
	}
	op.re = re
	return nil
}
func (op *IfStringRegexMatchOp) Reset() error { return nil }
func (op *IfStringRegexMatchOp) Run(_ context.Context) error {
	op.Match = op.re.MatchString(*op.Input)
	return nil
}
func (op *IfStringRegexMatchOp) InputFields() map[string]any {
	return map[string]any{"Input": &op.Input}
}
func (op *IfStringRegexMatchOp) OutputFields() map[string]any {
	return map[string]any{"Match": &op.Match}
}
func (op *IfStringRegexMatchOp) SetInputField(field string, value any) error {
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
func (op *IfStringRegexMatchOp) ResetFields() { op.Input = nil; op.Match = false }

// ============================================================================
// Emptiness / nil checks
// ============================================================================

const IfEmptyStringOpDescription = "IfEmptyStringOp: reports whether Value is nil or the empty string. Input: Value *string. Output: Match bool."
const IfEmptySliceStringOpDescription = "IfEmptySliceStringOp: reports whether Value is nil or has length 0. Input: Value *[]string. Output: Match bool."
const IfEmptySliceFloat64OpDescription = "IfEmptySliceFloat64Op: reports whether Value is nil or has length 0. Input: Value *[]float64. Output: Match bool."

type IfEmptyStringOp struct {
	Value *string `dag:"input"`
	Match bool    `dag:"output"`
}

func (op *IfEmptyStringOp) Setup(_ *config.Params) error { return nil }
func (op *IfEmptyStringOp) Reset() error                 { return nil }
func (op *IfEmptyStringOp) Run(_ context.Context) error {
	op.Match = op.Value == nil || *op.Value == ""
	return nil
}

type IfEmptySliceStringOp struct {
	Value *[]string
	Match bool
}

func (op *IfEmptySliceStringOp) Setup(_ *config.Params) error { return nil }
func (op *IfEmptySliceStringOp) Reset() error                 { return nil }
func (op *IfEmptySliceStringOp) Run(_ context.Context) error {
	op.Match = op.Value == nil || len(*op.Value) == 0
	return nil
}
func (op *IfEmptySliceStringOp) InputFields() map[string]any {
	return map[string]any{"Value": &op.Value}
}
func (op *IfEmptySliceStringOp) OutputFields() map[string]any {
	return map[string]any{"Match": &op.Match}
}
func (op *IfEmptySliceStringOp) SetInputField(field string, value any) error {
	if field != "Value" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]string)
	if !ok {
		return fmt.Errorf("field Value: expected *[]string, got %T", value)
	}
	op.Value = val
	return nil
}
func (op *IfEmptySliceStringOp) ResetFields() { op.Value = nil; op.Match = false }

type IfEmptySliceFloat64Op struct {
	Value *[]float64
	Match bool
}

func (op *IfEmptySliceFloat64Op) Setup(_ *config.Params) error { return nil }
func (op *IfEmptySliceFloat64Op) Reset() error                 { return nil }
func (op *IfEmptySliceFloat64Op) Run(_ context.Context) error {
	op.Match = op.Value == nil || len(*op.Value) == 0
	return nil
}
func (op *IfEmptySliceFloat64Op) InputFields() map[string]any {
	return map[string]any{"Value": &op.Value}
}
func (op *IfEmptySliceFloat64Op) OutputFields() map[string]any {
	return map[string]any{"Match": &op.Match}
}
func (op *IfEmptySliceFloat64Op) SetInputField(field string, value any) error {
	if field != "Value" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]float64)
	if !ok {
		return fmt.Errorf("field Value: expected *[]float64, got %T", value)
	}
	op.Value = val
	return nil
}
func (op *IfEmptySliceFloat64Op) ResetFields() { op.Value = nil; op.Match = false }

// ============================================================================
// Range
// ============================================================================

const BetweenFloatOpDescription = "BetweenFloatOp: reports whether Min <= Value <= Max (inclusive on both ends). Inputs: Value *float64, Min *float64, Max *float64. Output: Match bool."

type BetweenFloatOp struct {
	Value *float64 `dag:"input"`
	Min   *float64 `dag:"input"`
	Max   *float64 `dag:"input"`
	Match bool     `dag:"output"`
}

func (op *BetweenFloatOp) Setup(_ *config.Params) error { return nil }
func (op *BetweenFloatOp) Reset() error                 { return nil }
func (op *BetweenFloatOp) Run(_ context.Context) error {
	v := *op.Value
	op.Match = v >= *op.Min && v <= *op.Max
	return nil
}

func init() {
	operator.RegisterOp[IfFloatGtOp]()
	operator.RegisterOp[IfFloatLtOp]()
	operator.RegisterOp[IfFloatEqOp]()
	operator.RegisterOp[IfFloatGeOp]()
	operator.RegisterOp[IfFloatLeOp]()
	operator.RegisterOp[IfIntGtOp]()
	operator.RegisterOp[IfIntLtOp]()
	operator.RegisterOp[IfIntEqOp]()
	operator.RegisterOp[IfIntGeOp]()
	operator.RegisterOp[IfIntLeOp]()
	operator.RegisterOp[IfStringContainsOp]()
	operator.RegisterOp[IfStringHasPrefixOp]()
	operator.RegisterOp[IfStringHasSuffixOp]()
	operator.RegisterOp[IfStringRegexMatchOp]()
	operator.RegisterOp[IfEmptyStringOp]()
	operator.RegisterOp[IfEmptySliceStringOp]()
	operator.RegisterOp[IfEmptySliceFloat64Op]()
	operator.RegisterOp[BetweenFloatOp]()
}
