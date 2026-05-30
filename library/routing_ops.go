package library

import (
	"context"
	"fmt"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

const IfStringEqOpDescription = "IfStringEqOp: reports whether two strings are equal. Inputs: A *string, B *string. Output: Match bool."

type IfStringEqOp struct {
	A     *string
	B     *string
	Match bool
}

func (op *IfStringEqOp) Setup(_ *config.Params) error { return nil }
func (op *IfStringEqOp) Reset() error                 { return nil }
func (op *IfStringEqOp) Run(_ context.Context) error {
	op.Match = *op.A == *op.B
	return nil
}
func (op *IfStringEqOp) InputFields() map[string]any {
	return map[string]any{"A": &op.A, "B": &op.B}
}
func (op *IfStringEqOp) OutputFields() map[string]any { return map[string]any{"Match": &op.Match} }
func (op *IfStringEqOp) SetInputField(field string, value any) error {
	switch field {
	case "A":
		val, ok := value.(*string)
		if !ok {
			return fmt.Errorf("field A: expected *string, got %T", value)
		}
		op.A = val
	case "B":
		val, ok := value.(*string)
		if !ok {
			return fmt.Errorf("field B: expected *string, got %T", value)
		}
		op.B = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}
func (op *IfStringEqOp) ResetFields() { op.A = nil; op.B = nil; op.Match = false }

func init() {
	operator.RegisterOp[IfStringEqOp]()
}
