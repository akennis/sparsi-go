package library

import (
	"context"
	"fmt"
	"math"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

// MathOperands aggregates two float64 inputs for AI-powered binary numeric operations.
type MathOperands struct{ A, B float64 }

// FormatForPrompt implements AIInputFormatter.
func (m MathOperands) FormatForPrompt() string {
	return fmt.Sprintf("A=%v, B=%v", m.A, m.B)
}

const AddFloatOpDescription = "AddFloatOp: deterministic float64 addition. Inputs: A *float64, B *float64. Output: Result float64."
const SubFloatOpDescription = "SubFloatOp: A minus B (float64). Inputs: A *float64, B *float64. Output: Result float64."
const DivFloatOpDescription = "DivFloatOp: A divided by B (float64). Inputs: A *float64, B *float64. Output: Result float64. Error if B==0."
const MulFloatOpDescription = "MulFloatOp: A multiplied by B (float64). Inputs: A *float64, B *float64. Output: Result float64."
const AddIntOpDescription = "AddIntOp: deterministic int addition. Inputs: A *int, B *int. Output: Result int."
const SubIntOpDescription = "SubIntOp: A minus B (int). Inputs: A *int, B *int. Output: Result int."
const MulIntOpDescription = "MulIntOp: A multiplied by B (int). Inputs: A *int, B *int. Output: Result int."
const DivIntOpDescription = "DivIntOp: A divided by B (int, truncates toward zero). Inputs: A *int, B *int. Output: Result int. Error if B==0."
const PowFloatOpDescription = "PowFloatOp: A raised to the power B (float64). Inputs: A *float64, B *float64. Output: Result float64."
const PowIntOpDescription = "PowIntOp: A raised to the power B (int). Inputs: A *int, B *int. Output: Result int. Error if B<0."
const ModFloatOpDescription = "ModFloatOp: floating-point remainder of A/B. Inputs: A *float64, B *float64. Output: Result float64. Error if B==0."
const ModIntOpDescription = "ModIntOp: integer remainder of A/B. Inputs: A *int, B *int. Output: Result int. Error if B==0."
const IntToFloat64OpDescription = "IntToFloat64Op: widens an int wire to float64. Input: Value *int. Output: Result float64."
const Float64ToIntOpDescription = "Float64ToIntOp: truncates a float64 wire to int. Input: Value *float64. Output: Result int."

// ── Float infix ops ─────────────────────────────────────────────────────────

type AddFloatOp struct {
	A      *float64 `dag:"input"`
	B      *float64 `dag:"input"`
	Result float64  `dag:"output"`
}

func (op *AddFloatOp) Setup(params *config.Params) error {
	if params.Exists("A") {
		a := params.GetFloat64("A", 0)
		op.A = &a
	}
	if params.Exists("B") {
		b := params.GetFloat64("B", 0)
		op.B = &b
	}
	return nil
}
func (op *AddFloatOp) Reset() error                      { return nil }
func (op *AddFloatOp) Run(_ context.Context) error {
	op.Result = *op.A + *op.B
	return nil
}

type SubFloatOp struct {
	A      *float64 `dag:"input"`
	B      *float64 `dag:"input"`
	Result float64  `dag:"output"`
}

func (op *SubFloatOp) Setup(_ *config.Params) error { return nil }
func (op *SubFloatOp) Reset() error                 { return nil }
func (op *SubFloatOp) Run(_ context.Context) error {
	op.Result = *op.A - *op.B
	return nil
}

type DivFloatOp struct {
	A      *float64 `dag:"input"`
	B      *float64 `dag:"input"`
	Result float64  `dag:"output"`
}

func (op *DivFloatOp) Setup(_ *config.Params) error { return nil }
func (op *DivFloatOp) Reset() error                 { return nil }
func (op *DivFloatOp) Run(_ context.Context) error {
	if *op.B == 0 {
		return fmt.Errorf("division by zero")
	}
	op.Result = *op.A / *op.B
	return nil
}

type MulFloatOp struct {
	A      *float64 `dag:"input"`
	B      *float64 `dag:"input"`
	Result float64  `dag:"output"`
}

func (op *MulFloatOp) Setup(_ *config.Params) error { return nil }
func (op *MulFloatOp) Reset() error                 { return nil }
func (op *MulFloatOp) Run(_ context.Context) error {
	op.Result = *op.A * *op.B
	return nil
}

type PowFloatOp struct {
	A      *float64 `dag:"input"`
	B      *float64 `dag:"input"`
	Result float64  `dag:"output"`
}

func (op *PowFloatOp) Setup(_ *config.Params) error { return nil }
func (op *PowFloatOp) Reset() error                 { return nil }
func (op *PowFloatOp) Run(_ context.Context) error {
	op.Result = math.Pow(*op.A, *op.B)
	return nil
}

type ModFloatOp struct {
	A      *float64 `dag:"input"`
	B      *float64 `dag:"input"`
	Result float64  `dag:"output"`
}

func (op *ModFloatOp) Setup(_ *config.Params) error { return nil }
func (op *ModFloatOp) Reset() error                 { return nil }
func (op *ModFloatOp) Run(_ context.Context) error {
	if *op.B == 0 {
		return fmt.Errorf("modulo by zero")
	}
	op.Result = math.Mod(*op.A, *op.B)
	return nil
}

// ── Int infix ops ───────────────────────────────────────────────────────────

type AddIntOp struct {
	A      *int `dag:"input"`
	B      *int `dag:"input"`
	Result int  `dag:"output"`
}

func (op *AddIntOp) Setup(_ *config.Params) error { return nil }
func (op *AddIntOp) Reset() error                 { return nil }
func (op *AddIntOp) Run(_ context.Context) error {
	op.Result = *op.A + *op.B
	return nil
}

type SubIntOp struct {
	A      *int `dag:"input"`
	B      *int `dag:"input"`
	Result int  `dag:"output"`
}

func (op *SubIntOp) Setup(_ *config.Params) error { return nil }
func (op *SubIntOp) Reset() error                 { return nil }
func (op *SubIntOp) Run(_ context.Context) error {
	op.Result = *op.A - *op.B
	return nil
}

type MulIntOp struct {
	A      *int `dag:"input"`
	B      *int `dag:"input"`
	Result int  `dag:"output"`
}

func (op *MulIntOp) Setup(_ *config.Params) error { return nil }
func (op *MulIntOp) Reset() error                 { return nil }
func (op *MulIntOp) Run(_ context.Context) error {
	op.Result = *op.A * *op.B
	return nil
}

type DivIntOp struct {
	A      *int `dag:"input"`
	B      *int `dag:"input"`
	Result int  `dag:"output"`
}

func (op *DivIntOp) Setup(_ *config.Params) error { return nil }
func (op *DivIntOp) Reset() error                 { return nil }
func (op *DivIntOp) Run(_ context.Context) error {
	if *op.B == 0 {
		return fmt.Errorf("division by zero")
	}
	op.Result = *op.A / *op.B
	return nil
}

type PowIntOp struct {
	A      *int `dag:"input"`
	B      *int `dag:"input"`
	Result int  `dag:"output"`
}

func (op *PowIntOp) Setup(_ *config.Params) error { return nil }
func (op *PowIntOp) Reset() error                 { return nil }
func (op *PowIntOp) Run(_ context.Context) error {
	if *op.B < 0 {
		return fmt.Errorf("negative exponent for integer power")
	}
	base, exp := *op.A, *op.B
	result := 1
	for exp > 0 {
		if exp%2 == 1 {
			result *= base
		}
		base *= base
		exp /= 2
	}
	op.Result = result
	return nil
}

type ModIntOp struct {
	A      *int `dag:"input"`
	B      *int `dag:"input"`
	Result int  `dag:"output"`
}

func (op *ModIntOp) Setup(_ *config.Params) error { return nil }
func (op *ModIntOp) Reset() error                 { return nil }
func (op *ModIntOp) Run(_ context.Context) error {
	if *op.B == 0 {
		return fmt.Errorf("modulo by zero")
	}
	op.Result = *op.A % *op.B
	return nil
}

// ── Cast ops ─────────────────────────────────────────────────────────────────

type IntToFloat64Op struct {
	Value  *int    `dag:"input"`
	Result float64 `dag:"output"`
}

func (op *IntToFloat64Op) Setup(_ *config.Params) error { return nil }
func (op *IntToFloat64Op) Reset() error                 { return nil }
func (op *IntToFloat64Op) Run(_ context.Context) error {
	op.Result = float64(*op.Value)
	return nil
}

type Float64ToIntOp struct {
	Value  *float64 `dag:"input"`
	Result int      `dag:"output"`
}

func (op *Float64ToIntOp) Setup(_ *config.Params) error { return nil }
func (op *Float64ToIntOp) Reset() error                 { return nil }
func (op *Float64ToIntOp) Run(_ context.Context) error {
	op.Result = int(*op.Value)
	return nil
}

// ── Aggregate/utility ops (unchanged) ───────────────────────────────────────

const PackMathOperandsOpDescription = "PackMathOperandsOp: packs two float64 inputs into a MathOperands struct. Inputs: A *float64, B *float64. Output: Result MathOperands."
const AIComputeMathOperandsToFloat64OpDescription = `AIComputeMathOperandsToFloat64Op: AI-powered fallback for operations not available in the library.
  Params:   operation string — plain-English description of what to compute (e.g. "multiply A by B").
            max_retries string — number of parse retries (default "3").
            api_retries string — transient-error retries with exponential backoff (default "3").
            api_retry_delay_ms string — initial backoff delay in milliseconds (default "500").
            api_factory_timeout_ms string — deadline for AIClientFactory credential lookup in milliseconds (default "30000"; "0" disables).
            provider string — AI provider: "claude" (default) or "gemini".
            model string — model name passed through to the provider (default: "claude-sonnet-4-6").
            credential_ref string — opaque credential identifier passed to AIClientFactory (default ""; ignored by the bundled env-var factory).
            client_factory_id string — selects a registered AIClientFactory by id (default "" → process default).
  Inputs:   Input *MathOperands (connect PackMathOperandsOp's Result wire).
  Outputs:  Result float64, Reasoning string.`

// PackMathOperandsOp packs two scalar float64 inputs into a single MathOperands struct.
type PackMathOperandsOp struct {
	A      *float64     `dag:"input"`
	B      *float64     `dag:"input"`
	Result MathOperands `dag:"output"`
}

func (op *PackMathOperandsOp) Setup(_ *config.Params) error { return nil }
func (op *PackMathOperandsOp) Reset() error                 { return nil }
func (op *PackMathOperandsOp) Run(_ context.Context) error {
	op.Result = MathOperands{A: *op.A, B: *op.B}
	return nil
}

// AIComputeMathOperandsToFloat64Op is the registered concrete variant of AIComputeOp
// for binary float64 math operations.
type AIComputeMathOperandsToFloat64Op struct {
	AIComputeOp[MathOperands, float64]
}

const RoundOpDescription = "RoundOp: rounds Value to nearest integer. Input: Value *float64. Output: Result float64."
const ClampFloatOpDescription = "ClampFloatOp: clamps Value to [Min, Max] (float64). Inputs: Value *float64, Min *float64, Max *float64. Output: Result float64."
const ClampIntOpDescription = "ClampIntOp: clamps Value to [Min, Max] (int). Inputs: Value *int, Min *int, Max *int. Output: Result int."
const SumFloatOpDescription = "SumFloatOp: sums all values in a float64 slice. Input: Values *[]float64. Output: Result float64."
const SumIntOpDescription = "SumIntOp: sums all values in an int slice. Input: Values *[]int. Output: Result int."
const MinFloatOpDescription = "MinFloatOp: returns the minimum value in a float64 slice. Input: Values *[]float64. Output: Result float64. Error if empty."
const MinIntOpDescription = "MinIntOp: returns the minimum value in an int slice. Input: Values *[]int. Output: Result int. Error if empty."
const MaxFloatOpDescription = "MaxFloatOp: returns the maximum value in a float64 slice. Input: Values *[]float64. Output: Result float64. Error if empty."
const MaxIntOpDescription = "MaxIntOp: returns the maximum value in an int slice. Input: Values *[]int. Output: Result int. Error if empty."

type RoundOp struct {
	Value  *float64 `dag:"input"`
	Result float64  `dag:"output"`
}

func (op *RoundOp) Setup(_ *config.Params) error { return nil }
func (op *RoundOp) Reset() error                 { return nil }
func (op *RoundOp) Run(_ context.Context) error {
	op.Result = math.Round(*op.Value)
	return nil
}

type ClampFloatOp struct {
	Value  *float64 `dag:"input"`
	Min    *float64 `dag:"input"`
	Max    *float64 `dag:"input"`
	Result float64  `dag:"output"`
}

func (op *ClampFloatOp) Setup(_ *config.Params) error { return nil }
func (op *ClampFloatOp) Reset() error                 { return nil }
func (op *ClampFloatOp) Run(_ context.Context) error {
	v := *op.Value
	if v < *op.Min {
		v = *op.Min
	} else if v > *op.Max {
		v = *op.Max
	}
	op.Result = v
	return nil
}

type ClampIntOp struct {
	Value  *int `dag:"input"`
	Min    *int `dag:"input"`
	Max    *int `dag:"input"`
	Result int  `dag:"output"`
}

func (op *ClampIntOp) Setup(_ *config.Params) error { return nil }
func (op *ClampIntOp) Reset() error                 { return nil }
func (op *ClampIntOp) Run(_ context.Context) error {
	v := *op.Value
	if v < *op.Min {
		v = *op.Min
	} else if v > *op.Max {
		v = *op.Max
	}
	op.Result = v
	return nil
}

type SumFloatOp struct {
	Values *[]float64
	Result float64
}

func (op *SumFloatOp) Setup(_ *config.Params) error { return nil }
func (op *SumFloatOp) Reset() error                 { return nil }
func (op *SumFloatOp) Run(_ context.Context) error {
	var sum float64
	for _, v := range *op.Values {
		sum += v
	}
	op.Result = sum
	return nil
}
func (op *SumFloatOp) InputFields() map[string]any { return map[string]any{"Values": &op.Values} }
func (op *SumFloatOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *SumFloatOp) SetInputField(field string, value any) error {
	if field != "Values" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]float64)
	if !ok {
		return fmt.Errorf("field Values: expected *[]float64, got %T", value)
	}
	op.Values = val
	return nil
}
func (op *SumFloatOp) ResetFields() { op.Values = nil; op.Result = 0 }

type SumIntOp struct {
	Values *[]int
	Result int
}

func (op *SumIntOp) Setup(_ *config.Params) error { return nil }
func (op *SumIntOp) Reset() error                 { return nil }
func (op *SumIntOp) Run(_ context.Context) error {
	var sum int
	for _, v := range *op.Values {
		sum += v
	}
	op.Result = sum
	return nil
}
func (op *SumIntOp) InputFields() map[string]any { return map[string]any{"Values": &op.Values} }
func (op *SumIntOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *SumIntOp) SetInputField(field string, value any) error {
	if field != "Values" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]int)
	if !ok {
		return fmt.Errorf("field Values: expected *[]int, got %T", value)
	}
	op.Values = val
	return nil
}
func (op *SumIntOp) ResetFields() { op.Values = nil; op.Result = 0 }

type MinFloatOp struct {
	Values *[]float64
	Result float64
}

func (op *MinFloatOp) Setup(_ *config.Params) error { return nil }
func (op *MinFloatOp) Reset() error                 { return nil }
func (op *MinFloatOp) Run(_ context.Context) error {
	if len(*op.Values) == 0 {
		return fmt.Errorf("MinFloatOp: empty slice")
	}
	m := (*op.Values)[0]
	for _, v := range (*op.Values)[1:] {
		if v < m {
			m = v
		}
	}
	op.Result = m
	return nil
}
func (op *MinFloatOp) InputFields() map[string]any { return map[string]any{"Values": &op.Values} }
func (op *MinFloatOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *MinFloatOp) SetInputField(field string, value any) error {
	if field != "Values" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]float64)
	if !ok {
		return fmt.Errorf("field Values: expected *[]float64, got %T", value)
	}
	op.Values = val
	return nil
}
func (op *MinFloatOp) ResetFields() { op.Values = nil; op.Result = 0 }

type MinIntOp struct {
	Values *[]int
	Result int
}

func (op *MinIntOp) Setup(_ *config.Params) error { return nil }
func (op *MinIntOp) Reset() error                 { return nil }
func (op *MinIntOp) Run(_ context.Context) error {
	if len(*op.Values) == 0 {
		return fmt.Errorf("MinIntOp: empty slice")
	}
	m := (*op.Values)[0]
	for _, v := range (*op.Values)[1:] {
		if v < m {
			m = v
		}
	}
	op.Result = m
	return nil
}
func (op *MinIntOp) InputFields() map[string]any { return map[string]any{"Values": &op.Values} }
func (op *MinIntOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *MinIntOp) SetInputField(field string, value any) error {
	if field != "Values" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]int)
	if !ok {
		return fmt.Errorf("field Values: expected *[]int, got %T", value)
	}
	op.Values = val
	return nil
}
func (op *MinIntOp) ResetFields() { op.Values = nil; op.Result = 0 }

type MaxFloatOp struct {
	Values *[]float64
	Result float64
}

func (op *MaxFloatOp) Setup(_ *config.Params) error { return nil }
func (op *MaxFloatOp) Reset() error                 { return nil }
func (op *MaxFloatOp) Run(_ context.Context) error {
	if len(*op.Values) == 0 {
		return fmt.Errorf("MaxFloatOp: empty slice")
	}
	m := (*op.Values)[0]
	for _, v := range (*op.Values)[1:] {
		if v > m {
			m = v
		}
	}
	op.Result = m
	return nil
}
func (op *MaxFloatOp) InputFields() map[string]any { return map[string]any{"Values": &op.Values} }
func (op *MaxFloatOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *MaxFloatOp) SetInputField(field string, value any) error {
	if field != "Values" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]float64)
	if !ok {
		return fmt.Errorf("field Values: expected *[]float64, got %T", value)
	}
	op.Values = val
	return nil
}
func (op *MaxFloatOp) ResetFields() { op.Values = nil; op.Result = 0 }

type MaxIntOp struct {
	Values *[]int
	Result int
}

func (op *MaxIntOp) Setup(_ *config.Params) error { return nil }
func (op *MaxIntOp) Reset() error                 { return nil }
func (op *MaxIntOp) Run(_ context.Context) error {
	if len(*op.Values) == 0 {
		return fmt.Errorf("MaxIntOp: empty slice")
	}
	m := (*op.Values)[0]
	for _, v := range (*op.Values)[1:] {
		if v > m {
			m = v
		}
	}
	op.Result = m
	return nil
}
func (op *MaxIntOp) InputFields() map[string]any { return map[string]any{"Values": &op.Values} }
func (op *MaxIntOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *MaxIntOp) SetInputField(field string, value any) error {
	if field != "Values" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]int)
	if !ok {
		return fmt.Errorf("field Values: expected *[]int, got %T", value)
	}
	op.Values = val
	return nil
}
func (op *MaxIntOp) ResetFields() { op.Values = nil; op.Result = 0 }

func init() {
	operator.RegisterOp[AddFloatOp]()
	operator.RegisterOp[SubFloatOp]()
	operator.RegisterOp[DivFloatOp]()
	operator.RegisterOp[MulFloatOp]()
	operator.RegisterOp[PowFloatOp]()
	operator.RegisterOp[ModFloatOp]()
	operator.RegisterOp[AddIntOp]()
	operator.RegisterOp[SubIntOp]()
	operator.RegisterOp[MulIntOp]()
	operator.RegisterOp[DivIntOp]()
	operator.RegisterOp[PowIntOp]()
	operator.RegisterOp[ModIntOp]()
	operator.RegisterOp[IntToFloat64Op]()
	operator.RegisterOp[Float64ToIntOp]()
	operator.RegisterOp[RoundOp]()
	operator.RegisterOp[ClampFloatOp]()
	operator.RegisterOp[ClampIntOp]()
	operator.RegisterOp[SumFloatOp]()
	operator.RegisterOp[SumIntOp]()
	operator.RegisterOp[MinFloatOp]()
	operator.RegisterOp[MinIntOp]()
	operator.RegisterOp[MaxFloatOp]()
	operator.RegisterOp[MaxIntOp]()
	operator.RegisterOp[PackMathOperandsOp]()
	operator.RegisterOp[AIComputeMathOperandsToFloat64Op]()
}
