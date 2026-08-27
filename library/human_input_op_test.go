package library

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakePrompter is a scriptable HumanPrompter for op-level tests.
type fakePrompter struct {
	text       string
	boolean    bool
	choiceIdx  int
	err        error
	lastPrompt string
}

func (f *fakePrompter) AskText(_ context.Context, prompt string) (string, error) {
	f.lastPrompt = prompt
	return f.text, f.err
}
func (f *fakePrompter) AskBool(_ context.Context, prompt string) (bool, error) {
	f.lastPrompt = prompt
	return f.boolean, f.err
}
func (f *fakePrompter) AskChoice(_ context.Context, prompt string, _ []string) (int, error) {
	f.lastPrompt = prompt
	return f.choiceIdx, f.err
}

// --- HumanInputOp -----------------------------------------------------------

func TestHumanInputOp(t *testing.T) {
	fp := &fakePrompter{text: "the answer"}
	ctx := WithHumanPrompter(context.Background(), fp)

	op := &HumanInputOp{}
	if err := op.Setup(mustParams(t, map[string]string{"prompt": "What is it?"})); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := op.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	if op.Result != "the answer" {
		t.Fatalf("Result = %q, want %q", op.Result, "the answer")
	}
	if fp.lastPrompt != "What is it?" {
		t.Fatalf("prompt shown = %q", fp.lastPrompt)
	}
}

func TestHumanInputOp_DynamicInputAppended(t *testing.T) {
	fp := &fakePrompter{text: "ok"}
	ctx := WithHumanPrompter(context.Background(), fp)

	op := &HumanInputOp{Input: strPtr("DRAFT BODY")}
	if err := op.Setup(mustParams(t, map[string]string{"prompt": "Approve this?"})); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := op.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "Approve this?\n\nDRAFT BODY"; fp.lastPrompt != want {
		t.Fatalf("prompt = %q, want %q", fp.lastPrompt, want)
	}
}

func TestHumanInputOp_NoPrompter(t *testing.T) {
	op := &HumanInputOp{}
	_ = op.Setup(mustParams(t, map[string]string{"prompt": "x"}))
	if err := op.Run(context.Background()); err == nil {
		t.Fatal("expected error when no prompter on context")
	}
}

func TestHumanInputOp_NoMessage(t *testing.T) {
	fp := &fakePrompter{text: "x"}
	op := &HumanInputOp{}
	_ = op.Setup(mustParams(t, map[string]string{})) // empty prompt, no Input
	if err := op.Run(WithHumanPrompter(context.Background(), fp)); err == nil {
		t.Fatal("expected error when prompt and Input are both empty")
	}
}

// --- HumanBoolInputOp -------------------------------------------------------

func TestHumanBoolInputOp(t *testing.T) {
	for _, want := range []bool{true, false} {
		fp := &fakePrompter{boolean: want}
		op := &HumanBoolInputOp{}
		if err := op.Setup(mustParams(t, map[string]string{"prompt": "Send it?"})); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := op.Run(WithHumanPrompter(context.Background(), fp)); err != nil {
			t.Fatalf("run: %v", err)
		}
		if op.Result != want {
			t.Fatalf("Result = %v, want %v", op.Result, want)
		}
	}
}

func TestHumanBoolInputOp_DeclinePropagates(t *testing.T) {
	fp := &fakePrompter{err: ErrHumanDeclined}
	op := &HumanBoolInputOp{}
	_ = op.Setup(mustParams(t, map[string]string{"prompt": "Send it?"}))
	err := op.Run(WithHumanPrompter(context.Background(), fp))
	if !errors.Is(err, ErrHumanDeclined) {
		t.Fatalf("err = %v, want ErrHumanDeclined", err)
	}
}

// --- HumanChoiceInputOp -----------------------------------------------------

func TestHumanChoiceInputOp(t *testing.T) {
	fp := &fakePrompter{choiceIdx: 1}
	op := &HumanChoiceInputOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"prompt":  "Pick one",
		"options": "approve, revise, reject",
	})); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := op.Run(WithHumanPrompter(context.Background(), fp)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if op.Result != 1 {
		t.Fatalf("Result = %d, want 1", op.Result)
	}
}

func TestHumanChoiceInputOp_RequiresTwoOptions(t *testing.T) {
	op := &HumanChoiceInputOp{}
	if err := op.Setup(mustParams(t, map[string]string{"options": "only-one"})); err == nil {
		t.Fatal("expected error for <2 options")
	}
	if err := op.Setup(mustParams(t, map[string]string{})); err == nil {
		t.Fatal("expected error for missing options param")
	}
}

func TestHumanChoiceInputOp_OutOfRangeGuard(t *testing.T) {
	// A misbehaving prompter returns an index past the option list; the op must
	// reject it rather than emit a bogus index.
	fp := &fakePrompter{choiceIdx: 5}
	op := &HumanChoiceInputOp{}
	_ = op.Setup(mustParams(t, map[string]string{"options": "a,b"}))
	if err := op.Run(WithHumanPrompter(context.Background(), fp)); err == nil {
		t.Fatal("expected out-of-range error")
	}
}

// --- stdinPrompter parsing --------------------------------------------------

func newStdin(input string) *stdinPrompter {
	return &stdinPrompter{r: bufio.NewReader(strings.NewReader(input)), w: io.Discard}
}

func TestStdinPrompter_AskText(t *testing.T) {
	got, err := newStdin("hello world\n").AskText(context.Background(), "?")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestStdinPrompter_AskBool(t *testing.T) {
	cases := map[string]bool{
		"y\n": true, "yes\n": true, "TRUE\n": true, "1\n": true,
		"n\n": false, "no\n": false, "false\n": false, "0\n": false,
		"maybe\nyes\n": true, // re-prompts past an invalid answer
	}
	for in, want := range cases {
		got, err := newStdin(in).AskBool(context.Background(), "?")
		if err != nil {
			t.Fatalf("input %q: err %v", in, err)
		}
		if got != want {
			t.Fatalf("input %q: got %v want %v", in, got, want)
		}
	}
}

func TestStdinPrompter_AskBool_ExhaustsRetries(t *testing.T) {
	_, err := newStdin("a\nb\nc\n").AskBool(context.Background(), "?")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
}

func TestStdinPrompter_AskChoice(t *testing.T) {
	opts := []string{"approve", "revise", "reject"}

	// by 1-based number
	if idx, err := newStdin("2\n").AskChoice(context.Background(), "?", opts); err != nil || idx != 1 {
		t.Fatalf("number: idx=%d err=%v", idx, err)
	}
	// by exact string (case-insensitive)
	if idx, err := newStdin("REJECT\n").AskChoice(context.Background(), "?", opts); err != nil || idx != 2 {
		t.Fatalf("string: idx=%d err=%v", idx, err)
	}
	// out-of-range number re-prompts, then a valid one succeeds
	if idx, err := newStdin("9\n1\n").AskChoice(context.Background(), "?", opts); err != nil || idx != 0 {
		t.Fatalf("recover: idx=%d err=%v", idx, err)
	}
	// all invalid → error
	if _, err := newStdin("x\ny\nz\n").AskChoice(context.Background(), "?", opts); err == nil {
		t.Fatal("expected error for all-invalid input")
	}
}

// overlapDetector is a HumanPrompter that fails if two calls are ever in
// flight at once, proving WithHumanPrompter serializes access to the human.
type overlapDetector struct {
	inFlight int32
	overlap  int32
}

func (d *overlapDetector) enter() {
	if atomic.AddInt32(&d.inFlight, 1) != 1 {
		atomic.StoreInt32(&d.overlap, 1)
	}
	time.Sleep(time.Millisecond) // widen the window for a racing caller
	atomic.AddInt32(&d.inFlight, -1)
}
func (d *overlapDetector) AskText(context.Context, string) (string, error) { d.enter(); return "", nil }
func (d *overlapDetector) AskBool(context.Context, string) (bool, error) {
	d.enter()
	return false, nil
}
func (d *overlapDetector) AskChoice(context.Context, string, []string) (int, error) {
	d.enter()
	return 0, nil
}

func TestWithHumanPrompter_Serializes(t *testing.T) {
	det := &overlapDetector{}
	ctx := WithHumanPrompter(context.Background(), det)
	p, _ := HumanPrompterFrom(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.AskText(context.Background(), "?")
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&det.overlap) != 0 {
		t.Fatal("detected overlapping human prompts; serializer did not hold")
	}
}

func TestWithHumanPrompter_IdempotentWrap(t *testing.T) {
	det := &overlapDetector{}
	ctx := WithHumanPrompter(context.Background(), det)
	p1, _ := HumanPrompterFrom(ctx)
	// Re-installing an already-serialized prompter must not double-wrap.
	ctx2 := WithHumanPrompter(ctx, p1)
	p2, _ := HumanPrompterFrom(ctx2)
	if p1 != p2 {
		t.Fatal("WithHumanPrompter double-wrapped an already-serial prompter")
	}
}

func TestHumanMessage(t *testing.T) {
	if _, err := humanMessage("", nil); err == nil {
		t.Fatal("want error for empty prompt and nil input")
	}
	if msg, _ := humanMessage("Q", nil); msg != "Q" {
		t.Fatalf("got %q", msg)
	}
	if msg, _ := humanMessage("", strPtr("body")); msg != "body" {
		t.Fatalf("got %q", msg)
	}
	if msg, _ := humanMessage("Q", strPtr("body")); msg != "Q\n\nbody" {
		t.Fatalf("got %q", msg)
	}
}
