package library

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

// ============================================================================
// Select (ternary) — tag-based scalars
// ============================================================================

const SelectStringOpDescription = "SelectStringOp: ternary; returns IfTrue when Cond is true, otherwise IfFalse. Inputs: Cond *bool, IfTrue *string, IfFalse *string. Output: Result string."
const SelectFloat64OpDescription = "SelectFloat64Op: ternary; returns IfTrue when Cond is true, otherwise IfFalse. Inputs: Cond *bool, IfTrue *float64, IfFalse *float64. Output: Result float64."
const SelectIntOpDescription = "SelectIntOp: ternary; returns IfTrue when Cond is true, otherwise IfFalse. Inputs: Cond *bool, IfTrue *int, IfFalse *int. Output: Result int."
const SelectBoolOpDescription = "SelectBoolOp: ternary; returns IfTrue when Cond is true, otherwise IfFalse. Inputs: Cond *bool, IfTrue *bool, IfFalse *bool. Output: Result bool."

type SelectStringOp struct {
	Cond    *bool   `dag:"input"`
	IfTrue  *string `dag:"input"`
	IfFalse *string `dag:"input"`
	Result  string  `dag:"output"`
}

func (op *SelectStringOp) Setup(_ *config.Params) error { return nil }
func (op *SelectStringOp) Reset() error                 { return nil }
func (op *SelectStringOp) Run(_ context.Context) error {
	if *op.Cond {
		op.Result = *op.IfTrue
	} else {
		op.Result = *op.IfFalse
	}
	return nil
}

type SelectFloat64Op struct {
	Cond    *bool    `dag:"input"`
	IfTrue  *float64 `dag:"input"`
	IfFalse *float64 `dag:"input"`
	Result  float64  `dag:"output"`
}

func (op *SelectFloat64Op) Setup(_ *config.Params) error { return nil }
func (op *SelectFloat64Op) Reset() error                 { return nil }
func (op *SelectFloat64Op) Run(_ context.Context) error {
	if *op.Cond {
		op.Result = *op.IfTrue
	} else {
		op.Result = *op.IfFalse
	}
	return nil
}

type SelectIntOp struct {
	Cond    *bool `dag:"input"`
	IfTrue  *int  `dag:"input"`
	IfFalse *int  `dag:"input"`
	Result  int   `dag:"output"`
}

func (op *SelectIntOp) Setup(_ *config.Params) error { return nil }
func (op *SelectIntOp) Reset() error                 { return nil }
func (op *SelectIntOp) Run(_ context.Context) error {
	if *op.Cond {
		op.Result = *op.IfTrue
	} else {
		op.Result = *op.IfFalse
	}
	return nil
}

type SelectBoolOp struct {
	Cond    *bool `dag:"input"`
	IfTrue  *bool `dag:"input"`
	IfFalse *bool `dag:"input"`
	Result  bool  `dag:"output"`
}

func (op *SelectBoolOp) Setup(_ *config.Params) error { return nil }
func (op *SelectBoolOp) Reset() error                 { return nil }
func (op *SelectBoolOp) Run(_ context.Context) error {
	if *op.Cond {
		op.Result = *op.IfTrue
	} else {
		op.Result = *op.IfFalse
	}
	return nil
}

// ============================================================================
// Switch — hand-rolled (param parsing in Setup)
// ============================================================================

const SwitchStringOpDescription = `SwitchStringOp: looks up Key in a params-configured cases map; returns the configured default on miss.
  Params: cases — JSON-encoded key→value pairs (e.g. {"red":"stop","green":"go"}).
          default — string returned when Key is nil or not in cases (default "").
  Input:  Key *string.
  Output: Result string.`

type SwitchStringOp struct {
	Key      *string
	Result   string
	cases    map[string]string
	defValue string
}

func (op *SwitchStringOp) Setup(params *config.Params) error {
	raw := params.GetString("cases", "{}")
	if raw == "" {
		raw = "{}"
	}
	if err := json.Unmarshal([]byte(raw), &op.cases); err != nil {
		return fmt.Errorf("SwitchStringOp: invalid cases param: %w", err)
	}
	op.defValue = params.GetString("default", "")
	return nil
}

func (op *SwitchStringOp) Reset() error { return nil }

func (op *SwitchStringOp) Run(_ context.Context) error {
	if op.Key == nil {
		op.Result = op.defValue
		return nil
	}
	if v, ok := op.cases[*op.Key]; ok {
		op.Result = v
	} else {
		op.Result = op.defValue
	}
	return nil
}

func (op *SwitchStringOp) InputFields() map[string]any {
	return map[string]any{"Key": &op.Key}
}

func (op *SwitchStringOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}

func (op *SwitchStringOp) SetInputField(field string, value any) error {
	if field != "Key" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*string)
	if !ok {
		return fmt.Errorf("field Key: expected *string, got %T", value)
	}
	op.Key = val
	return nil
}

func (op *SwitchStringOp) ResetFields() {
	op.Key = nil
	op.Result = ""
}

// ============================================================================
// Default — tag-based scalars; tolerate nil Value via runtime checks.
// ============================================================================

const DefaultStringOpDescription = "DefaultStringOp: returns Default when Value is nil or the empty string; otherwise returns Value. Inputs: Value *string, Default *string. Output: Result string."
const DefaultFloat64OpDescription = "DefaultFloat64Op: returns Default when Value is nil; zero is treated as a valid value. Inputs: Value *float64, Default *float64. Output: Result float64."
const DefaultIntOpDescription = "DefaultIntOp: returns Default when Value is nil; zero is treated as a valid value. Inputs: Value *int, Default *int. Output: Result int."

type DefaultStringOp struct {
	Value   *string `dag:"input"`
	Default *string `dag:"input"`
	Result  string  `dag:"output"`
}

func (op *DefaultStringOp) Setup(_ *config.Params) error { return nil }
func (op *DefaultStringOp) Reset() error                 { return nil }
func (op *DefaultStringOp) Run(_ context.Context) error {
	if op.Value == nil || *op.Value == "" {
		op.Result = *op.Default
	} else {
		op.Result = *op.Value
	}
	return nil
}

type DefaultFloat64Op struct {
	Value   *float64 `dag:"input"`
	Default *float64 `dag:"input"`
	Result  float64  `dag:"output"`
}

func (op *DefaultFloat64Op) Setup(_ *config.Params) error { return nil }
func (op *DefaultFloat64Op) Reset() error                 { return nil }
func (op *DefaultFloat64Op) Run(_ context.Context) error {
	if op.Value == nil {
		op.Result = *op.Default
	} else {
		op.Result = *op.Value
	}
	return nil
}

type DefaultIntOp struct {
	Value   *int `dag:"input"`
	Default *int `dag:"input"`
	Result  int  `dag:"output"`
}

func (op *DefaultIntOp) Setup(_ *config.Params) error { return nil }
func (op *DefaultIntOp) Reset() error                 { return nil }
func (op *DefaultIntOp) Run(_ context.Context) error {
	if op.Value == nil {
		op.Result = *op.Default
	} else {
		op.Result = *op.Value
	}
	return nil
}

func init() {
	operator.RegisterOp[SelectStringOp]()
	operator.RegisterOp[SelectFloat64Op]()
	operator.RegisterOp[SelectIntOp]()
	operator.RegisterOp[SelectBoolOp]()
	operator.RegisterOp[SwitchStringOp]()
	operator.RegisterOp[DefaultStringOp]()
	operator.RegisterOp[DefaultFloat64Op]()
	operator.RegisterOp[DefaultIntOp]()
}
