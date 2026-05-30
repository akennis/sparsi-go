package library

import (
	"context"
	"fmt"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

const BoolNotOpDescription = "BoolNotOp: logical NOT. Input: Value *bool. Output: Result bool."
const BoolAndOpDescription = "BoolAndOp: logical AND. Inputs: A *bool, B *bool. Output: Result bool."
const BoolOrOpDescription = "BoolOrOp: logical OR. Inputs: A *bool, B *bool. Output: Result bool."

type BoolNotOp struct {
	Value  *bool
	Result bool
}

func (op *BoolNotOp) Setup(_ *config.Params) error { return nil }
func (op *BoolNotOp) Reset() error                 { return nil }
func (op *BoolNotOp) Run(_ context.Context) error {
	op.Result = !*op.Value
	return nil
}
func (op *BoolNotOp) InputFields() map[string]any  { return map[string]any{"Value": &op.Value} }
func (op *BoolNotOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *BoolNotOp) SetInputField(field string, value any) error {
	if field != "Value" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*bool)
	if !ok {
		return fmt.Errorf("field Value: expected *bool, got %T", value)
	}
	op.Value = val
	return nil
}
func (op *BoolNotOp) ResetFields() { op.Value = nil; op.Result = false }

type BoolAndOp struct {
	A      *bool
	B      *bool
	Result bool
}

func (op *BoolAndOp) Setup(_ *config.Params) error { return nil }
func (op *BoolAndOp) Reset() error                 { return nil }
func (op *BoolAndOp) Run(_ context.Context) error {
	op.Result = *op.A && *op.B
	return nil
}
func (op *BoolAndOp) InputFields() map[string]any {
	return map[string]any{"A": &op.A, "B": &op.B}
}
func (op *BoolAndOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *BoolAndOp) SetInputField(field string, value any) error {
	switch field {
	case "A":
		val, ok := value.(*bool)
		if !ok {
			return fmt.Errorf("field A: expected *bool, got %T", value)
		}
		op.A = val
	case "B":
		val, ok := value.(*bool)
		if !ok {
			return fmt.Errorf("field B: expected *bool, got %T", value)
		}
		op.B = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}
func (op *BoolAndOp) ResetFields() { op.A = nil; op.B = nil; op.Result = false }

type BoolOrOp struct {
	A      *bool
	B      *bool
	Result bool
}

func (op *BoolOrOp) Setup(_ *config.Params) error { return nil }
func (op *BoolOrOp) Reset() error                 { return nil }
func (op *BoolOrOp) Run(_ context.Context) error {
	op.Result = *op.A || *op.B
	return nil
}
func (op *BoolOrOp) InputFields() map[string]any {
	return map[string]any{"A": &op.A, "B": &op.B}
}
func (op *BoolOrOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *BoolOrOp) SetInputField(field string, value any) error {
	switch field {
	case "A":
		val, ok := value.(*bool)
		if !ok {
			return fmt.Errorf("field A: expected *bool, got %T", value)
		}
		op.A = val
	case "B":
		val, ok := value.(*bool)
		if !ok {
			return fmt.Errorf("field B: expected *bool, got %T", value)
		}
		op.B = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}
func (op *BoolOrOp) ResetFields() { op.A = nil; op.B = nil; op.Result = false }

func init() {
	operator.RegisterOp[BoolNotOp]()
	operator.RegisterOp[BoolAndOp]()
	operator.RegisterOp[BoolOrOp]()
}
