package library

import (
	"context"
	"fmt"
	"log"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

// ConstOp[T] emits a fixed value of type T captured at construction time.
// Use RegisterConst to create and register a named instance.
type ConstOp[T any] struct {
	Result T
	value  T
}

func (op *ConstOp[T]) Setup(_ *config.Params) error { return nil }
func (op *ConstOp[T]) Reset() error                 { return nil }
func (op *ConstOp[T]) Run(_ context.Context) error  { op.Result = op.value; return nil }
func (op *ConstOp[T]) InputFields() map[string]any  { return map[string]any{} }
func (op *ConstOp[T]) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *ConstOp[T]) SetInputField(field string, _ any) error {
	return fmt.Errorf("ConstOp: no input fields (got %q)", field)
}
func (op *ConstOp[T]) ResetFields() { var zero T; op.Result = zero }

// RegisterConst registers a factory under name that emits value on every run.
func RegisterConst[T any](name string, value T) {
	if err := operator.RegisterOpFactory(name, func() operator.IOperator {
		return &ConstOp[T]{value: value}
	}); err != nil {
		log.Fatalf("RegisterConst %q: %v", name, err)
	}
}
