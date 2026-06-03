package library

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

const SliceLenOpDescription = "SliceLenOp: returns the length of a string slice. Input: Input *[]string. Output: Result int."
const SliceAtOpDescription = "SliceAtOp: returns the element at a given index. Param: index (int, used when Index wire is absent). Inputs: Input *[]string, Index *int (optional wire). Output: Result string."
const SliceFirstOpDescription = "SliceFirstOp: returns the first element. Input: Input *[]string. Output: Result string. Error if empty."
const SliceLastOpDescription = "SliceLastOp: returns the last element. Input: Input *[]string. Output: Result string. Error if empty."
const SliceContainsOpDescription = "SliceContainsOp: reports whether a slice contains a value. Inputs: Input *[]string, Value *string. Output: Match bool."
const SliceJoinOpDescription = `SliceJoinOp: joins a string slice with a separator. Param: sep (default ","). Input: Input *[]string. Output: Result string.`
const SliceFilterEqOpDescription = "SliceFilterEqOp: returns elements equal to Value. Inputs: Input *[]string, Value *string. Output: Result []string."
const SliceTopKOpDescription = "SliceTopKOp: returns indices of the K highest scores in descending order. Param: k (int). Input: Scores *[]float64. Output: Result []int."

type SliceLenOp struct {
	Input  *[]string
	Result int
}

func (op *SliceLenOp) Setup(_ *config.Params) error { return nil }
func (op *SliceLenOp) Reset() error                 { return nil }
func (op *SliceLenOp) Run(_ context.Context) error {
	op.Result = len(*op.Input)
	return nil
}
func (op *SliceLenOp) InputFields() map[string]any { return map[string]any{"Input": &op.Input} }
func (op *SliceLenOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *SliceLenOp) SetInputField(field string, value any) error {
	if field != "Input" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]string)
	if !ok {
		return fmt.Errorf("field Input: expected *[]string, got %T", value)
	}
	op.Input = val
	return nil
}
func (op *SliceLenOp) ResetFields() { op.Input = nil; op.Result = 0 }

type SliceAtOp struct {
	Input  *[]string
	Index  *int
	Result string
	index  int
}

func (op *SliceAtOp) Setup(params *config.Params) error {
	s := params.GetString("index", "0")
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("SliceAtOp: invalid index %q: %w", s, err)
	}
	op.index = n
	return nil
}
func (op *SliceAtOp) Reset() error { return nil }
func (op *SliceAtOp) Run(_ context.Context) error {
	idx := op.index
	if op.Index != nil {
		idx = *op.Index
	}
	sl := *op.Input
	if idx < 0 || idx >= len(sl) {
		return fmt.Errorf("SliceAtOp: index %d out of range (len %d)", idx, len(sl))
	}
	op.Result = sl[idx]
	return nil
}
func (op *SliceAtOp) InputFields() map[string]any {
	return map[string]any{"Input": &op.Input, "Index": &op.Index}
}
func (op *SliceAtOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *SliceAtOp) SetInputField(field string, value any) error {
	switch field {
	case "Input":
		val, ok := value.(*[]string)
		if !ok {
			return fmt.Errorf("field Input: expected *[]string, got %T", value)
		}
		op.Input = val
	case "Index":
		val, ok := value.(*int)
		if !ok {
			return fmt.Errorf("field Index: expected *int, got %T", value)
		}
		op.Index = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}
func (op *SliceAtOp) ResetFields() { op.Input = nil; op.Index = nil; op.Result = "" }


type SliceFirstOp struct {
	Input  *[]string
	Result string
}

func (op *SliceFirstOp) Setup(_ *config.Params) error { return nil }
func (op *SliceFirstOp) Reset() error                 { return nil }
func (op *SliceFirstOp) Run(_ context.Context) error {
	if len(*op.Input) == 0 {
		return fmt.Errorf("SliceFirstOp: empty slice")
	}
	op.Result = (*op.Input)[0]
	return nil
}
func (op *SliceFirstOp) InputFields() map[string]any { return map[string]any{"Input": &op.Input} }
func (op *SliceFirstOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *SliceFirstOp) SetInputField(field string, value any) error {
	if field != "Input" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]string)
	if !ok {
		return fmt.Errorf("field Input: expected *[]string, got %T", value)
	}
	op.Input = val
	return nil
}
func (op *SliceFirstOp) ResetFields() { op.Input = nil; op.Result = "" }

type SliceLastOp struct {
	Input  *[]string
	Result string
}

func (op *SliceLastOp) Setup(_ *config.Params) error { return nil }
func (op *SliceLastOp) Reset() error                 { return nil }
func (op *SliceLastOp) Run(_ context.Context) error {
	sl := *op.Input
	if len(sl) == 0 {
		return fmt.Errorf("SliceLastOp: empty slice")
	}
	op.Result = sl[len(sl)-1]
	return nil
}
func (op *SliceLastOp) InputFields() map[string]any { return map[string]any{"Input": &op.Input} }
func (op *SliceLastOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *SliceLastOp) SetInputField(field string, value any) error {
	if field != "Input" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]string)
	if !ok {
		return fmt.Errorf("field Input: expected *[]string, got %T", value)
	}
	op.Input = val
	return nil
}
func (op *SliceLastOp) ResetFields() { op.Input = nil; op.Result = "" }

type SliceContainsOp struct {
	Input *[]string
	Value *string
	Match bool
}

func (op *SliceContainsOp) Setup(_ *config.Params) error { return nil }
func (op *SliceContainsOp) Reset() error                 { return nil }
func (op *SliceContainsOp) Run(_ context.Context) error {
	for _, s := range *op.Input {
		if s == *op.Value {
			op.Match = true
			return nil
		}
	}
	op.Match = false
	return nil
}
func (op *SliceContainsOp) InputFields() map[string]any {
	return map[string]any{"Input": &op.Input, "Value": &op.Value}
}
func (op *SliceContainsOp) OutputFields() map[string]any { return map[string]any{"Match": &op.Match} }
func (op *SliceContainsOp) SetInputField(field string, value any) error {
	switch field {
	case "Input":
		val, ok := value.(*[]string)
		if !ok {
			return fmt.Errorf("field Input: expected *[]string, got %T", value)
		}
		op.Input = val
	case "Value":
		val, ok := value.(*string)
		if !ok {
			return fmt.Errorf("field Value: expected *string, got %T", value)
		}
		op.Value = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}
func (op *SliceContainsOp) ResetFields() { op.Input = nil; op.Value = nil; op.Match = false }

type SliceJoinOp struct {
	Input  *[]string
	Result string
	sep    string
}

func (op *SliceJoinOp) Setup(params *config.Params) error {
	op.sep = params.GetString("sep", ",")
	return nil
}
func (op *SliceJoinOp) Reset() error { return nil }
func (op *SliceJoinOp) Run(_ context.Context) error {
	op.Result = strings.Join(*op.Input, op.sep)
	return nil
}
func (op *SliceJoinOp) InputFields() map[string]any { return map[string]any{"Input": &op.Input} }
func (op *SliceJoinOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *SliceJoinOp) SetInputField(field string, value any) error {
	if field != "Input" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]string)
	if !ok {
		return fmt.Errorf("field Input: expected *[]string, got %T", value)
	}
	op.Input = val
	return nil
}
func (op *SliceJoinOp) ResetFields() { op.Input = nil; op.Result = "" }

type SliceFilterEqOp struct {
	Input  *[]string
	Value  *string
	Result []string
}

func (op *SliceFilterEqOp) Setup(_ *config.Params) error { return nil }
func (op *SliceFilterEqOp) Reset() error                 { return nil }
func (op *SliceFilterEqOp) Run(_ context.Context) error {
	var out []string
	for _, s := range *op.Input {
		if s == *op.Value {
			out = append(out, s)
		}
	}
	op.Result = out
	return nil
}
func (op *SliceFilterEqOp) InputFields() map[string]any {
	return map[string]any{"Input": &op.Input, "Value": &op.Value}
}
func (op *SliceFilterEqOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}
func (op *SliceFilterEqOp) SetInputField(field string, value any) error {
	switch field {
	case "Input":
		val, ok := value.(*[]string)
		if !ok {
			return fmt.Errorf("field Input: expected *[]string, got %T", value)
		}
		op.Input = val
	case "Value":
		val, ok := value.(*string)
		if !ok {
			return fmt.Errorf("field Value: expected *string, got %T", value)
		}
		op.Value = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}
func (op *SliceFilterEqOp) ResetFields() { op.Input = nil; op.Value = nil; op.Result = nil }

type SliceTopKOp struct {
	Scores *[]float64
	Result []int
	k      int
}

func (op *SliceTopKOp) Setup(params *config.Params) error {
	s := params.GetString("k", "1")
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return fmt.Errorf("SliceTopKOp: invalid k %q", s)
	}
	op.k = n
	return nil
}
func (op *SliceTopKOp) Reset() error { return nil }
func (op *SliceTopKOp) Run(_ context.Context) error {
	scores := *op.Scores
	indices := make([]int, len(scores))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		return scores[indices[i]] > scores[indices[j]]
	})
	k := op.k
	if k > len(indices) {
		k = len(indices)
	}
	op.Result = indices[:k]
	return nil
}
func (op *SliceTopKOp) InputFields() map[string]any { return map[string]any{"Scores": &op.Scores} }
func (op *SliceTopKOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *SliceTopKOp) SetInputField(field string, value any) error {
	if field != "Scores" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*[]float64)
	if !ok {
		return fmt.Errorf("field Scores: expected *[]float64, got %T", value)
	}
	op.Scores = val
	return nil
}
func (op *SliceTopKOp) ResetFields() { op.Scores = nil; op.Result = nil }

func init() {
	operator.RegisterOp[SliceLenOp]()
	operator.RegisterOp[SliceAtOp]()
	operator.RegisterOp[SliceFirstOp]()
	operator.RegisterOp[SliceLastOp]()
	operator.RegisterOp[SliceContainsOp]()
	operator.RegisterOp[SliceJoinOp]()
	operator.RegisterOp[SliceFilterEqOp]()
	operator.RegisterOp[SliceTopKOp]()
}
