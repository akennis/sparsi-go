package library

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// repairStubCaller records every call and returns programmed responses in
// order. When responses are exhausted it returns the last response forever.
type repairStubCaller struct {
	calls     []aiCallRequest
	responses []aiCallResult
}

func (c *repairStubCaller) call(_ context.Context, req aiCallRequest) (aiCallResult, error) {
	c.calls = append(c.calls, req)
	if len(c.responses) == 0 {
		return aiCallResult{}, errors.New("repairStubCaller: no responses programmed")
	}
	var res aiCallResult
	if len(c.calls) <= len(c.responses) {
		res = c.responses[len(c.calls)-1]
	} else {
		res = c.responses[len(c.responses)-1]
	}
	return res, nil
}

// brokenThenFixedOut is an AIComputeOp Out type that demands an exact "FIXED"
// response. Any other response returns *ErrRepairable so the op enters the
// in-conversation repair loop.
type brokenThenFixedOut struct {
	Value string
}

func (o *brokenThenFixedOut) ParseAIResponse(response string) error {
	resp := strings.TrimSpace(response)
	if resp == "FIXED" {
		o.Value = resp
		return nil
	}
	return &ErrRepairable{
		Prompt: "That response was wrong. Reply with exactly FIXED.",
		Cause:  errors.New("expected FIXED, got " + resp),
	}
}

// brokenThenFixedIn is the input type. The minimal shape needed for
// AIComputeOp to format the prompt.
type brokenThenFixedIn struct {
	Query string
}

func (i *brokenThenFixedIn) FormatForPrompt() string { return i.Query }

// newRepairTestOp builds an AIComputeOp wired with a stub caller, bypassing
// Setup so no API key is needed.
func newRepairTestOp(stub *repairStubCaller, maxRetries int) *AIComputeOp[brokenThenFixedIn, brokenThenFixedOut] {
	op := &AIComputeOp[brokenThenFixedIn, brokenThenFixedOut]{}
	op.operation = "test repair"
	op.maxRetries = maxRetries
	op.provider = "claude"
	op.model = "stub-model"
	op.caller = stub
	return op
}

func TestAIComputeOp_ConversationalRepair_SucceedsOnSecondTurn(t *testing.T) {
	stub := &repairStubCaller{
		responses: []aiCallResult{
			{Text: "WRONG", InputTokens: 10, OutputTokens: 5},
			{Text: "FIXED", InputTokens: 15, OutputTokens: 5},
		},
	}
	op := newRepairTestOp(stub, 3)
	op.Input = &brokenThenFixedIn{Query: "say the magic word"}

	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if op.Result.Value != "FIXED" {
		t.Errorf("Result.Value = %q, want %q", op.Result.Value, "FIXED")
	}

	if got := len(stub.calls); got != 2 {
		t.Fatalf("call count = %d, want 2", got)
	}

	first := stub.calls[0]
	if len(first.History) != 0 {
		t.Errorf("first call History len = %d, want 0", len(first.History))
	}
	if !strings.Contains(first.Prompt, "say the magic word") {
		t.Errorf("first call Prompt missing input description: %q", first.Prompt)
	}

	second := stub.calls[1]
	if len(second.History) != 2 {
		t.Fatalf("second call History len = %d, want 2", len(second.History))
	}
	if second.History[0].Role != "user" {
		t.Errorf("History[0].Role = %q, want user", second.History[0].Role)
	}
	if second.History[0].Text != first.Prompt {
		t.Errorf("History[0].Text should echo first prompt verbatim")
	}
	if second.History[1].Role != "assistant" {
		t.Errorf("History[1].Role = %q, want assistant", second.History[1].Role)
	}
	if second.History[1].Text != "WRONG" {
		t.Errorf("History[1].Text = %q, want WRONG", second.History[1].Text)
	}
	if !strings.Contains(second.Prompt, "exactly FIXED") {
		t.Errorf("second call Prompt should carry the ErrRepairable prompt verbatim, got %q", second.Prompt)
	}
}

func TestAIComputeOp_ConversationalRepair_GrowsHistoryAcrossTurns(t *testing.T) {
	stub := &repairStubCaller{
		responses: []aiCallResult{
			{Text: "WRONG1"},
			{Text: "WRONG2"},
			{Text: "FIXED"},
		},
	}
	op := newRepairTestOp(stub, 5)
	op.Input = &brokenThenFixedIn{Query: "hi"}

	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := len(stub.calls); got != 3 {
		t.Fatalf("call count = %d, want 3", got)
	}

	// Third call should have 4 turns of history: 2 (user/assistant) per prior turn.
	third := stub.calls[2]
	if len(third.History) != 4 {
		t.Fatalf("third call History len = %d, want 4", len(third.History))
	}
	roles := []string{
		third.History[0].Role,
		third.History[1].Role,
		third.History[2].Role,
		third.History[3].Role,
	}
	want := []string{"user", "assistant", "user", "assistant"}
	for i, r := range roles {
		if r != want[i] {
			t.Errorf("History[%d].Role = %q, want %q", i, r, want[i])
		}
	}
	if third.History[3].Text != "WRONG2" {
		t.Errorf("third call History[3].Text = %q, want WRONG2", third.History[3].Text)
	}
}

func TestAIComputeOp_ConversationalRepair_ExhaustsBudget(t *testing.T) {
	// Always-wrong: every response triggers ErrRepairable from ParseAIResponse.
	stub := &repairStubCaller{
		responses: []aiCallResult{
			{Text: "still wrong"},
		},
	}
	op := newRepairTestOp(stub, 2) // 2 retries → 3 attempts total
	op.Input = &brokenThenFixedIn{Query: "doomed"}

	err := op.Run(context.Background())
	if err == nil {
		t.Fatal("expected error after exhausting repair budget, got nil")
	}
	if !strings.Contains(err.Error(), "conversational repair") {
		t.Errorf("error message should mention conversational repair: %v", err)
	}
	if got := len(stub.calls); got != 3 {
		t.Errorf("call count = %d, want 3 (maxRetries+1)", got)
	}
}
