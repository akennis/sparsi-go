package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wwz16/dagor/config"
)

// stubRepairable implements RepairableInput. UnmarshalRepair stores the text
// (or returns an error if it begins with "ERR:").
type stubRepairable struct {
	Text string
}

func (s *stubRepairable) UnmarshalRepair(response string) error {
	if strings.HasPrefix(response, "ERR:") {
		return errors.New(strings.TrimPrefix(response, "ERR:"))
	}
	s.Text = response
	return nil
}

// stubInner is a configurable IOperator implementation that records calls and
// can be programmed to fail with *ErrRepairable for the first N runs, or with
// an arbitrary error.
type stubInner struct {
	Input *stubRepairable
	Out   string

	// runs counts inner.Run invocations.
	runs int
	// failures, if non-nil, is consumed in order: each entry is what Run returns
	// for that invocation (nil means success). When exhausted, Run succeeds.
	failures []error
	// onRun is called with the current Input.Text before failures is consulted.
	// Useful for asserting that SetInputField wired the new value.
	onRun func(text string)
}

func (op *stubInner) Setup(_ *config.Params) error { return nil }
func (op *stubInner) Reset() error                 { return nil }
func (op *stubInner) ResetFields()                 { op.Input = nil; op.Out = "" }
func (op *stubInner) InputFields() map[string]any  { return map[string]any{"Input": &op.Input} }
func (op *stubInner) OutputFields() map[string]any { return map[string]any{"Out": &op.Out} }

func (op *stubInner) SetInputField(field string, value any) error {
	if field != "Input" {
		return fmt.Errorf("stubInner: unknown field %q", field)
	}
	v, ok := value.(*stubRepairable)
	if !ok {
		return fmt.Errorf("stubInner: Input: expected *stubRepairable, got %T", value)
	}
	op.Input = v
	return nil
}

func (op *stubInner) Run(_ context.Context) error {
	op.runs++
	text := ""
	if op.Input != nil {
		text = op.Input.Text
	}
	if op.onRun != nil {
		op.onRun(text)
	}
	if len(op.failures) > 0 {
		err := op.failures[0]
		op.failures = op.failures[1:]
		if err != nil {
			return err
		}
	}
	op.Out = "ok:" + text
	return nil
}

// stubCaller returns canned text per call and records prompts received.
type stubCaller struct {
	responses []string
	prompts   []string
	errOn     int // if > 0, return error on the (errOn-1)th call
	calls     int
}

func (c *stubCaller) call(_ context.Context, req aiCallRequest) (aiCallResult, error) {
	c.prompts = append(c.prompts, req.Prompt)
	c.calls++
	if c.errOn > 0 && c.calls == c.errOn {
		return aiCallResult{}, errors.New("simulated API error")
	}
	if len(c.responses) == 0 {
		return aiCallResult{Text: ""}, nil
	}
	r := c.responses[0]
	c.responses = c.responses[1:]
	return aiCallResult{Text: r}, nil
}

// build a wrapper directly (bypassing RegisterWithRepair) so tests can hand-stub
// the caller. Mirrors what RegisterWithRepair's factory produces.
func newTestWrapper(inner *stubInner, cfg RepairConfig, caller aiCaller) *withRepairOp[*stubInner] {
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 2048
	}
	if cfg.InputField == "" {
		cfg.InputField = "Input"
	}
	return &withRepairOp[*stubInner]{
		inner:  inner,
		config: cfg,
		caller: caller,
		name:   "test",
	}
}

func TestWithRepair_SuccessFirstTry_NoLLM(t *testing.T) {
	inner := &stubInner{Input: &stubRepairable{Text: "good"}}
	caller := &stubCaller{}
	w := newTestWrapper(inner, RepairConfig{}, caller)

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if caller.calls != 0 {
		t.Errorf("LLM should not be called on first-try success; got calls=%d", caller.calls)
	}
	if inner.runs != 1 {
		t.Errorf("inner should run exactly once; got runs=%d", inner.runs)
	}
	if inner.Out != "ok:good" {
		t.Errorf("Out=%q want %q", inner.Out, "ok:good")
	}
}

func TestWithRepair_NonRepairableErrorPropagated(t *testing.T) {
	plainErr := errors.New("plain old error")
	inner := &stubInner{
		Input:    &stubRepairable{Text: "bad"},
		failures: []error{plainErr},
	}
	caller := &stubCaller{}
	w := newTestWrapper(inner, RepairConfig{}, caller)

	err := w.Run(context.Background())
	if !errors.Is(err, plainErr) {
		t.Fatalf("want propagated plainErr, got %v", err)
	}
	if caller.calls != 0 {
		t.Errorf("LLM must not be called for non-repairable errors; got calls=%d", caller.calls)
	}
}

func TestWithRepair_RepairsThenSucceeds(t *testing.T) {
	inner := &stubInner{
		Input: &stubRepairable{Text: "broken"},
		failures: []error{
			&ErrRepairable{Prompt: "fix me", Cause: errors.New("schema bad")},
			nil,
		},
	}
	caller := &stubCaller{
		responses: []string{"fixed-by-llm"},
	}
	w := newTestWrapper(inner, RepairConfig{
		PromptPrefix: "[prefix] ",
		PromptSuffix: " [suffix]",
	}, caller)

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if caller.calls != 1 {
		t.Errorf("expected 1 LLM call; got %d", caller.calls)
	}
	if got := caller.prompts[0]; got != "[prefix] fix me [suffix]" {
		t.Errorf("prompt sandwich wrong: got %q", got)
	}
	if inner.runs != 2 {
		t.Errorf("inner should run twice (initial + after repair); got %d", inner.runs)
	}
	if inner.Out != "ok:fixed-by-llm" {
		t.Errorf("Out=%q — repair value not wired into inner; want %q", inner.Out, "ok:fixed-by-llm")
	}
}

func TestWithRepair_MaxAttemptsExhausted(t *testing.T) {
	rep := &ErrRepairable{Prompt: "fix me", Cause: errors.New("still broken")}
	inner := &stubInner{
		Input:    &stubRepairable{Text: "broken"},
		failures: []error{rep, rep, rep, rep}, // initial + 3 repairs all fail
	}
	caller := &stubCaller{
		responses: []string{"r1", "r2", "r3"},
	}
	w := newTestWrapper(inner, RepairConfig{MaxAttempts: 3}, caller)

	err := w.Run(context.Background())
	if err == nil {
		t.Fatalf("expected exhaustion error")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("error %q should mention exhaustion", err)
	}
	if caller.calls != 3 {
		t.Errorf("expected 3 LLM calls; got %d", caller.calls)
	}
	if inner.runs != 4 {
		t.Errorf("expected 4 inner runs (1 initial + 3 repair); got %d", inner.runs)
	}
}

func TestWithRepair_UnparseableResponseCountsAsAttempt(t *testing.T) {
	// Inner fails repairably the first time, then succeeds after the wrapper
	// applies a second LLM response. The first LLM response is unparseable
	// (UnmarshalRepair returns an error) — that attempt must consume budget but
	// must NOT cause inner.Run to be called with bad data.
	rep := &ErrRepairable{Prompt: "fix me", Cause: errors.New("broken")}
	innerCalls := 0
	wiredText := []string{}
	inner := &stubInner{
		Input: &stubRepairable{Text: "orig"},
		failures: []error{
			rep, // initial
			nil, // succeeds when given a parseable LLM response
		},
		onRun: func(text string) {
			innerCalls++
			wiredText = append(wiredText, text)
		},
	}
	caller := &stubCaller{
		responses: []string{
			"ERR:llm gibberish",         // unparseable — UnmarshalRepair returns error
			"good-second-response",      // parseable
		},
	}
	w := newTestWrapper(inner, RepairConfig{MaxAttempts: 3}, caller)

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if caller.calls != 2 {
		t.Errorf("expected 2 LLM calls (1 unparseable, 1 good); got %d", caller.calls)
	}
	if innerCalls != 2 {
		t.Errorf("inner should run twice (initial + after good repair); got %d", innerCalls)
	}
	if len(wiredText) != 2 || wiredText[0] != "orig" || wiredText[1] != "good-second-response" {
		t.Errorf("unexpected wired text sequence: %v", wiredText)
	}
	// The second prompt must include the augmentation about the previous bad response.
	if len(caller.prompts) < 2 || !strings.Contains(caller.prompts[1], "unparseable") {
		t.Errorf("second prompt should announce previous failure; got %q", caller.prompts[1])
	}
}

func TestWithRepair_LLMAPIErrorPropagated(t *testing.T) {
	rep := &ErrRepairable{Prompt: "fix me", Cause: errors.New("broken")}
	inner := &stubInner{
		Input:    &stubRepairable{Text: "orig"},
		failures: []error{rep},
	}
	caller := &stubCaller{
		errOn:     1,
		responses: []string{},
	}
	w := newTestWrapper(inner, RepairConfig{}, caller)

	err := w.Run(context.Background())
	if err == nil {
		t.Fatalf("expected propagated API error")
	}
	if !strings.Contains(err.Error(), "simulated API error") {
		t.Errorf("API error should propagate; got %v", err)
	}
}

func TestValidateRepairableField(t *testing.T) {
	type notRepairable struct{ X int }
	type badInner struct{ NR *notRepairable }
	// hand-rolled minimal IOperator wrapper for badInner — we only need InputFields.
	bad := &fakeOp{fields: map[string]any{"NR": new(*notRepairable)}}
	if err := validateRepairableField(bad, "NR"); err == nil {
		t.Fatalf("expected validation failure for non-RepairableInput field")
	}

	good := &fakeOp{fields: map[string]any{"Input": new(*stubRepairable)}}
	if err := validateRepairableField(good, "Input"); err != nil {
		t.Fatalf("unexpected validation failure: %v", err)
	}

	if err := validateRepairableField(good, "Missing"); err == nil {
		t.Fatalf("expected validation failure for missing field")
	}
}

// fakeOp is an IOperator stub used only by TestValidateRepairableField.
type fakeOp struct {
	fields map[string]any
}

func (f *fakeOp) Setup(_ *config.Params) error                  { return nil }
func (f *fakeOp) Reset() error                                  { return nil }
func (f *fakeOp) Run(_ context.Context) error                   { return nil }
func (f *fakeOp) InputFields() map[string]any                   { return f.fields }
func (f *fakeOp) OutputFields() map[string]any                  { return nil }
func (f *fakeOp) SetInputField(_ string, _ any) error           { return nil }
func (f *fakeOp) ResetFields()                                  {}
