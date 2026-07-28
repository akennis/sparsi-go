package main

import (
	"context"
	"strings"
	"testing"

	"github.com/panjf2000/ants/v2"

	"github.com/akennis/sparsi-go/library"
)

// scriptedPrompter answers by inspecting the prompt text, so the test is
// deterministic no matter which order the (independent, parallel) HITL ops run
// in — exactly the property the per-run serializer buys us.
type scriptedPrompter struct {
	approve bool
	choice  int
	note    string
}

func (s scriptedPrompter) AskText(context.Context, string) (string, error) { return s.note, nil }
func (s scriptedPrompter) AskBool(context.Context, string) (bool, error)   { return s.approve, nil }
func (s scriptedPrompter) AskChoice(_ context.Context, _ string, opts []string) (int, error) {
	return s.choice, nil
}

func TestRunWorkflow_Approved(t *testing.T) {
	pool, _ := ants.NewPool(10)
	defer pool.Release()

	p := scriptedPrompter{approve: true, choice: 1, note: "handle with care"}
	res, err := runWorkflow(context.Background(), pool, p, UserInput{Draft: "Ship v2 to all users"})
	if err != nil {
		t.Fatalf("runWorkflow: %v", err)
	}
	// The bool, the choice index (→ deliveryOptions[1]) and the note must all
	// have flowed into the DecisionOp summary.
	for _, want := range []string{"Approved", "Ship v2 to all users", deliveryOptions[1], "handle with care"} {
		if !strings.Contains(res.Summary, want) {
			t.Fatalf("summary %q missing %q", res.Summary, want)
		}
	}
}

func TestRunWorkflow_Declined(t *testing.T) {
	pool, _ := ants.NewPool(10)
	defer pool.Release()

	p := scriptedPrompter{approve: false, choice: 0}
	res, err := runWorkflow(context.Background(), pool, p, UserInput{Draft: "Ship v2"})
	if err != nil {
		t.Fatalf("runWorkflow: %v", err)
	}
	if !strings.Contains(res.Summary, "Cancelled") {
		t.Fatalf("summary %q, want Cancelled", res.Summary)
	}
}

var _ library.HumanPrompter = scriptedPrompter{}
