//go:build integration

package library

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

// tracingCaller wraps a real aiCaller and records every request, letting the
// integration test inspect what got threaded across turns.
type tracingCaller struct {
	inner aiCaller
	calls []aiCallRequest
}

func (t *tracingCaller) call(ctx context.Context, req aiCallRequest) (aiCallResult, error) {
	t.calls = append(t.calls, req)
	return t.inner.call(ctx, req)
}

// strictSumIn carries the two integers as a single string for the prompt.
type strictSumIn struct{ Question string }

func (s *strictSumIn) FormatForPrompt() string { return s.Question }

// strictSumOut accepts only the exact wrapped form `<sum>N</sum>`. The
// ExpectedFormat description deliberately understates the requirement so the
// model fails on turn 1 and only learns about the tag wrapper from the repair
// prompt — proving the repair path actually drove the eventual success.
type strictSumOut struct{ Value int }

var sumTagPattern = regexp.MustCompile(`^<sum>(-?\d+)</sum>$`)

func (o *strictSumOut) ExpectedFormat() string {
	return "Reply with the sum as a number. No prose."
}

func (o *strictSumOut) ParseAIResponse(response string) error {
	resp := strings.TrimSpace(response)
	m := sumTagPattern.FindStringSubmatch(resp)
	if m == nil {
		return &ErrRepairable{
			Prompt: "Your response must be wrapped in <sum></sum> tags with nothing else around it. " +
				"For example, if the sum is 15, reply exactly: <sum>15</sum>. " +
				"No prose, no markdown, no surrounding whitespace. Reply again now in that form.",
			Cause: errors.New("response not wrapped in <sum></sum>: " + resp),
		}
	}
	n := 0
	for _, ch := range m[1] {
		if ch == '-' {
			continue
		}
		n = n*10 + int(ch-'0')
	}
	if strings.HasPrefix(m[1], "-") {
		n = -n
	}
	o.Value = n
	return nil
}

// TestAIComputeOp_ConversationalRepair_Integration exercises the real Anthropic
// API. The strictSumOut parser deliberately rejects the natural response shape
// on the first turn so the in-conversation repair path is forced.
//
// Asserts:
//
//   - Run completes successfully (final response satisfies ParseAIResponse).
//   - At least 2 calls were made (turn 1 failed, repair turn(s) followed).
//   - Turn 2's History contains a (user, assistant) pair — i.e. the repair was
//     issued as a follow-up turn on the same conversation, not as a fresh call.
//   - The model produced the strict shape, yielding the correct sum.
func TestAIComputeOp_ConversationalRepair_Integration(t *testing.T) {
	skipIfNoAPIKey(t)

	op := &AIComputeOp[strictSumIn, strictSumOut]{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "Compute the sum of the two integers in the input.",
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	tracer := &tracingCaller{inner: op.caller}
	op.caller = tracer

	op.Input = &strictSumIn{Question: "7 and 8"}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	if err := op.Run(ctx); err != nil {
		t.Fatalf("Run: %v (calls=%d)", err, len(tracer.calls))
	}

	if op.Result.Value != 15 {
		t.Errorf("Result.Value = %d, want 15", op.Result.Value)
	}

	if len(tracer.calls) < 2 {
		t.Fatalf("expected at least 2 calls (turn 1 should fail validation), got %d. "+
			"The model may have inferred the strict format from context — re-tune the test prompt.",
			len(tracer.calls))
	}

	first := tracer.calls[0]
	if len(first.History) != 0 {
		t.Errorf("first call History len = %d, want 0", len(first.History))
	}

	second := tracer.calls[1]
	if len(second.History) < 2 {
		t.Fatalf("second call History len = %d, want >= 2 (conversational repair). "+
			"This indicates the call was sent as a fresh prompt rather than threaded.",
			len(second.History))
	}
	if second.History[0].Role != "user" {
		t.Errorf("second call History[0].Role = %q, want user", second.History[0].Role)
	}
	if second.History[1].Role != "assistant" {
		t.Errorf("second call History[1].Role = %q, want assistant", second.History[1].Role)
	}
	if !strings.Contains(second.Prompt, "<sum>") {
		t.Errorf("second call Prompt should carry the repair instructions, got %q", second.Prompt)
	}

	t.Logf("conversational repair completed in %d turns. final response satisfied <sum>N</sum>.", len(tracer.calls))
	for i, c := range tracer.calls {
		t.Logf("  turn %d: history=%d prompt-prefix=%q", i+1, len(c.History), truncate(c.Prompt, 80))
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
