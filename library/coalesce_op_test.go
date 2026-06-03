package library

import (
	"testing"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/graph"
	"github.com/akennis/dagor/predicate"
)

func boolPtr(v bool) *bool { return &v }

// ───────────────────────────── Integration tests ──────────────────────────────
//
// These tests verify that dagor's MergeCoalesce skip semantics work correctly
// with the builtin CoalesceOp in conditional-branch DAGs.

// TestCoalesceOp_Integration_ConditionalMerge: two mutually-exclusive branches
// feed into a CoalesceFloat64Op with MergeCoalesce. The positive branch fires
// (value=5>0) and provides A; the negative branch is skipped. The coalesce
// must still execute and pick A.
func TestCoalesceOp_Integration_ConditionalMerge(t *testing.T) {
	const posPred = "test_coalesce_int_is_positive"
	const negPred = "test_coalesce_int_is_negative"
	predicate.Unregister(posPred)
	predicate.Unregister(negPred)
	if err := predicate.Register(posPred, func(inputs map[string]any) bool {
		v, ok := inputs["src_val"].(*float64)
		return ok && v != nil && *v > 0
	}); err != nil {
		t.Fatalf("predicate.Register pos: %v", err)
	}
	defer predicate.Unregister(posPred)
	if err := predicate.Register(negPred, func(inputs map[string]any) bool {
		v, ok := inputs["src_val"].(*float64)
		return ok && v != nil && *v < 0
	}); err != nil {
		t.Fatalf("predicate.Register neg: %v", err)
	}
	defer predicate.Unregister(negPred)

	g, err := graph.NewBuilder("coalesce_int_demo").
		Vertex("src").Op("ConstFloat64Op").
		Params(map[string]string{"Value": "5"}).
		Output("Result", "src_val").
		Vertex("pos").Op("ConstFloat64Op").
		Condition(posPred).
		ConditionInput("src_val").
		Params(map[string]string{"Value": "100"}).
		Output("Result", "pos_out").
		Vertex("neg").Op("ConstFloat64Op").
		Condition(negPred).
		ConditionInput("src_val").
		Params(map[string]string{"Value": "-100"}).
		Output("Result", "neg_out").
		Vertex("coalesce").Op("CoalesceFloat64Op").
		Merge(config.MergeCoalesce).
		Input("A", "pos_out").Input("B", "neg_out").
		Output("Result", "final").
		Build()
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	eng := runGraph(t, g)
	raw, ok := eng.GetOutput("final")
	if !ok {
		t.Fatal("final wire missing")
	}
	v, ok := raw.(*float64)
	if !ok || v == nil {
		t.Fatalf("final: expected *float64, got %T", raw)
	}
	if *v != 100 {
		t.Errorf("final = %v, want 100", *v)
	}
}

// TestCoalesceOp_Integration_StringVariant: exercises op-name resolution for
// CoalesceStringOp. The always-true branch provides A; the always-false branch
// is skipped. Coalesce must pick A.
func TestCoalesceOp_Integration_StringVariant(t *testing.T) {
	const truePred = "test_coalesce_string_always_true"
	const falsePred = "test_coalesce_string_always_false"
	predicate.Unregister(truePred)
	predicate.Unregister(falsePred)
	if err := predicate.Register(truePred, func(_ map[string]any) bool { return true }); err != nil {
		t.Fatalf("register true: %v", err)
	}
	defer predicate.Unregister(truePred)
	if err := predicate.Register(falsePred, func(_ map[string]any) bool { return false }); err != nil {
		t.Fatalf("register false: %v", err)
	}
	defer predicate.Unregister(falsePred)

	g, err := graph.NewBuilder("coalesce_string_demo").
		Vertex("a_branch").Op("ConstStringOp").
		Condition(truePred).
		Params(map[string]string{"Value": "alpha"}).
		Output("Result", "a_out").
		Vertex("b_branch").Op("ConstStringOp").
		Condition(falsePred).
		Params(map[string]string{"Value": "beta"}).
		Output("Result", "b_out").
		Vertex("coalesce").Op("CoalesceStringOp").
		Merge(config.MergeCoalesce).
		Input("A", "a_out").Input("B", "b_out").
		Output("Result", "final").
		Build()
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	eng := runGraph(t, g)
	raw, ok := eng.GetOutput("final")
	if !ok {
		t.Fatal("final wire missing")
	}
	v, ok := raw.(*string)
	if !ok || v == nil {
		t.Fatalf("final: expected *string, got %T", raw)
	}
	if *v != "alpha" {
		t.Errorf("final = %q, want %q", *v, "alpha")
	}
}
