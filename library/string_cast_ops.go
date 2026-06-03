package library

import (
	"context"
	"fmt"
	"reflect"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

const Float64ToStringOpDescription = "Float64ToStringOp: formats a float64 as string using %v. Input: Value *float64. Output: Result string."
const IntToStringOpDescription = "IntToStringOp: formats an int as string using %v. Input: Value *int. Output: Result string."
const BoolToStringOpDescription = "BoolToStringOp: formats a bool as string (\"true\" or \"false\"). Input: Value *bool. Output: Result string."
const ToStringOpDescription = "ToStringOp: formats any upstream pointer value as string using %v; accepts any pointer type via reflection (escape hatch for custom struct wires). Input: Value (any pointer). Output: Result string."

type Float64ToStringOp struct {
	Value  *float64 `dag:"input"`
	Result string   `dag:"output"`
}

func (op *Float64ToStringOp) Setup(_ *config.Params) error { return nil }
func (op *Float64ToStringOp) Reset() error                 { return nil }
func (op *Float64ToStringOp) Run(_ context.Context) error {
	op.Result = fmt.Sprintf("%v", *op.Value)
	return nil
}

type IntToStringOp struct {
	Value  *int   `dag:"input"`
	Result string `dag:"output"`
}

func (op *IntToStringOp) Setup(_ *config.Params) error { return nil }
func (op *IntToStringOp) Reset() error                 { return nil }
func (op *IntToStringOp) Run(_ context.Context) error {
	op.Result = fmt.Sprintf("%v", *op.Value)
	return nil
}

type BoolToStringOp struct {
	Value  *bool  `dag:"input"`
	Result string `dag:"output"`
}

func (op *BoolToStringOp) Setup(_ *config.Params) error { return nil }
func (op *BoolToStringOp) Reset() error                 { return nil }
func (op *BoolToStringOp) Run(_ context.Context) error {
	op.Result = fmt.Sprintf("%v", *op.Value)
	return nil
}

// ToStringOp converts any upstream wire type to string using fmt.Sprintf("%v").
// SetInputField uses reflection to dereference any pointer — the wire type need
// not be known at compile time. This is the escape hatch for custom struct wires.
type ToStringOp struct {
	Value  *any
	Result string
}

func (op *ToStringOp) Setup(_ *config.Params) error { return nil }
func (op *ToStringOp) Reset() error                 { return nil }
func (op *ToStringOp) Run(_ context.Context) error {
	op.Result = fmt.Sprintf("%v", *op.Value)
	return nil
}
func (op *ToStringOp) InputFields() map[string]any  { return map[string]any{"Value": &op.Value} }
func (op *ToStringOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *ToStringOp) SetInputField(field string, value any) error {
	if field != "Value" {
		return fmt.Errorf("field %s is not defined", field)
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return fmt.Errorf("ToStringOp: Value expected a pointer, got %T", value)
	}
	v := rv.Elem().Interface()
	op.Value = &v
	return nil
}
func (op *ToStringOp) ResetFields() { op.Value = nil; op.Result = "" }

func init() {
	operator.RegisterOp[Float64ToStringOp]()
	operator.RegisterOp[IntToStringOp]()
	operator.RegisterOp[BoolToStringOp]()
	operator.RegisterOp[ToStringOp]()
}
