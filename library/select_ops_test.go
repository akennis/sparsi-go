package library

import (
	"context"
	"strings"
	"testing"

	"github.com/akennis/dagor/graph"
)

// ─────────────────────────────── Select ───────────────────────────────────────

func TestSelectStringOp(t *testing.T) {
	cases := []struct {
		name           string
		cond           bool
		ifTrue, ifFalse string
		want           string
	}{
		{"true_branch", true, "yes", "no", "yes"},
		{"false_branch", false, "yes", "no", "no"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			cond := c.cond
			ifT := c.ifTrue
			ifF := c.ifFalse
			op := &SelectStringOp{Cond: &cond, IfTrue: &ifT, IfFalse: &ifF}
			if err := op.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if op.Result != c.want {
				t.Errorf("Result = %q, want %q", op.Result, c.want)
			}
		})
	}
}

func TestSelectFloat64Op(t *testing.T) {
	cond := true
	a := 1.5
	b := 2.5
	op := &SelectFloat64Op{Cond: &cond, IfTrue: &a, IfFalse: &b}
	if err := op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if op.Result != 1.5 {
		t.Errorf("Result = %v, want 1.5", op.Result)
	}
	cond = false
	if err := op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if op.Result != 2.5 {
		t.Errorf("Result = %v, want 2.5", op.Result)
	}
}

func TestSelectIntOp(t *testing.T) {
	cond := false
	a := 10
	b := 20
	op := &SelectIntOp{Cond: &cond, IfTrue: &a, IfFalse: &b}
	if err := op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if op.Result != 20 {
		t.Errorf("Result = %d, want 20", op.Result)
	}
}

func TestSelectBoolOp(t *testing.T) {
	cond := true
	tt := true
	ff := false
	op := &SelectBoolOp{Cond: &cond, IfTrue: &tt, IfFalse: &ff}
	if err := op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !op.Result {
		t.Errorf("Result = %v, want true", op.Result)
	}
	cond = false
	if err := op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if op.Result {
		t.Errorf("Result = %v, want false", op.Result)
	}
}

func TestSelectStringOp_Boilerplate(t *testing.T) {
	op := &SelectStringOp{}
	for _, name := range []string{"Cond", "IfTrue", "IfFalse"} {
		if _, ok := op.InputFields()[name]; !ok {
			t.Errorf("missing input %s", name)
		}
	}
	if _, ok := op.OutputFields()["Result"]; !ok {
		t.Error("missing output Result")
	}
	// Wrong-typed value
	if err := op.SetInputField("Cond", "not-a-bool-ptr"); err == nil {
		t.Error("expected error for wrong-typed Cond")
	}
	if err := op.SetInputField("Bogus", boolPtr(true)); err == nil {
		t.Error("expected error for unknown field")
	}
	cond := true
	ifT := "x"
	ifF := "y"
	op.Cond = &cond
	op.IfTrue = &ifT
	op.IfFalse = &ifF
	op.Result = "z"
	op.ResetFields()
	if op.Cond != nil || op.IfTrue != nil || op.IfFalse != nil || op.Result != "" {
		t.Errorf("ResetFields did not zero fields: %+v", op)
	}
}

// ─────────────────────────────── Switch ───────────────────────────────────────

func TestSwitchStringOp_Hit(t *testing.T) {
	op := &SwitchStringOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"cases":   `{"red":"stop","green":"go","yellow":"slow"}`,
		"default": "unknown",
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	k := "green"
	op.Key = &k
	if err := op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if op.Result != "go" {
		t.Errorf("Result = %q, want %q", op.Result, "go")
	}
}

func TestSwitchStringOp_Miss(t *testing.T) {
	op := &SwitchStringOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"cases":   `{"red":"stop"}`,
		"default": "unknown",
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	k := "purple"
	op.Key = &k
	if err := op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if op.Result != "unknown" {
		t.Errorf("Result = %q, want %q", op.Result, "unknown")
	}
}

func TestSwitchStringOp_NilKey(t *testing.T) {
	op := &SwitchStringOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"cases":   `{"a":"b"}`,
		"default": "fallback",
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	op.Key = nil
	if err := op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if op.Result != "fallback" {
		t.Errorf("Result = %q, want %q", op.Result, "fallback")
	}
}

func TestSwitchStringOp_DefaultEmpty(t *testing.T) {
	op := &SwitchStringOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"cases": `{"a":"b"}`,
	})); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	k := "missing"
	op.Key = &k
	if err := op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if op.Result != "" {
		t.Errorf("Result = %q, want empty default", op.Result)
	}
}

func TestSwitchStringOp_InvalidCases(t *testing.T) {
	op := &SwitchStringOp{}
	err := op.Setup(mustParams(t, map[string]string{"cases": "not-json"}))
	if err == nil {
		t.Fatal("expected error for invalid cases JSON")
	}
	if !strings.Contains(err.Error(), "invalid cases") {
		t.Errorf("error %q should mention 'invalid cases'", err.Error())
	}
}

func TestSwitchStringOp_Boilerplate(t *testing.T) {
	op := &SwitchStringOp{}
	if _, ok := op.InputFields()["Key"]; !ok {
		t.Error("missing input Key")
	}
	if _, ok := op.OutputFields()["Result"]; !ok {
		t.Error("missing output Result")
	}
	if err := op.SetInputField("Key", 42); err == nil {
		t.Error("expected error for wrong-typed Key")
	}
	if err := op.SetInputField("Bogus", strPtr("x")); err == nil {
		t.Error("expected error for unknown field")
	}
	k := "x"
	op.Key = &k
	op.Result = "y"
	op.ResetFields()
	if op.Key != nil || op.Result != "" {
		t.Errorf("ResetFields did not zero fields: %+v", op)
	}
	if err := op.Reset(); err != nil {
		t.Errorf("Reset: %v", err)
	}
}

// ─────────────────────────────── Default ──────────────────────────────────────

func TestDefaultStringOp(t *testing.T) {
	cases := []struct {
		name        string
		valuePtr    *string
		defaultStr  string
		want        string
	}{
		{"nil_value", nil, "fallback", "fallback"},
		{"empty_value", strPtr(""), "fallback", "fallback"},
		{"set_value", strPtr("real"), "fallback", "real"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			d := c.defaultStr
			op := &DefaultStringOp{Value: c.valuePtr, Default: &d}
			if err := op.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if op.Result != c.want {
				t.Errorf("Result = %q, want %q", op.Result, c.want)
			}
		})
	}
}

func TestDefaultFloat64Op(t *testing.T) {
	t.Run("nil_value_uses_default", func(t *testing.T) {
		d := 99.0
		op := &DefaultFloat64Op{Value: nil, Default: &d}
		if err := op.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if op.Result != 99.0 {
			t.Errorf("Result = %v, want 99.0", op.Result)
		}
	})
	t.Run("zero_is_valid", func(t *testing.T) {
		v := 0.0
		d := 99.0
		op := &DefaultFloat64Op{Value: &v, Default: &d}
		if err := op.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if op.Result != 0.0 {
			t.Errorf("Result = %v, want 0.0 (zero must be a valid value)", op.Result)
		}
	})
	t.Run("set_value", func(t *testing.T) {
		v := 3.14
		d := 99.0
		op := &DefaultFloat64Op{Value: &v, Default: &d}
		if err := op.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if op.Result != 3.14 {
			t.Errorf("Result = %v, want 3.14", op.Result)
		}
	})
}

func TestDefaultIntOp(t *testing.T) {
	t.Run("nil_value_uses_default", func(t *testing.T) {
		d := 7
		op := &DefaultIntOp{Value: nil, Default: &d}
		if err := op.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if op.Result != 7 {
			t.Errorf("Result = %d, want 7", op.Result)
		}
	})
	t.Run("zero_is_valid", func(t *testing.T) {
		v := 0
		d := 7
		op := &DefaultIntOp{Value: &v, Default: &d}
		if err := op.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if op.Result != 0 {
			t.Errorf("Result = %d, want 0", op.Result)
		}
	})
	t.Run("set_value", func(t *testing.T) {
		v := 42
		d := 7
		op := &DefaultIntOp{Value: &v, Default: &d}
		if err := op.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if op.Result != 42 {
			t.Errorf("Result = %d, want 42", op.Result)
		}
	})
}

func TestDefaultStringOp_Boilerplate(t *testing.T) {
	op := &DefaultStringOp{}
	for _, name := range []string{"Value", "Default"} {
		if _, ok := op.InputFields()[name]; !ok {
			t.Errorf("missing input %s", name)
		}
	}
	if _, ok := op.OutputFields()["Result"]; !ok {
		t.Error("missing output Result")
	}
	if err := op.SetInputField("Value", 42); err == nil {
		t.Error("expected error for wrong-typed Value")
	}
	op.Value = strPtr("a")
	op.Default = strPtr("b")
	op.Result = "c"
	op.ResetFields()
	if op.Value != nil || op.Default != nil || op.Result != "" {
		t.Errorf("ResetFields did not zero fields: %+v", op)
	}
}

// ─────────────────────────── Description constants ───────────────────────────

func TestSelectSwitchDefaultDescriptions(t *testing.T) {
	descs := map[string]string{
		"SelectStringOp":   SelectStringOpDescription,
		"SelectFloat64Op":  SelectFloat64OpDescription,
		"SelectIntOp":      SelectIntOpDescription,
		"SelectBoolOp":     SelectBoolOpDescription,
		"SwitchStringOp":   SwitchStringOpDescription,
		"DefaultStringOp":  DefaultStringOpDescription,
		"DefaultFloat64Op": DefaultFloat64OpDescription,
		"DefaultIntOp":     DefaultIntOpDescription,
	}
	for name, desc := range descs {
		if desc == "" {
			t.Errorf("%s description is empty", name)
		}
		if !strings.HasPrefix(desc, name) {
			t.Errorf("%s description should start with op name, got %q", name, desc)
		}
	}
}

// ─────────────────────────── Integration tests ───────────────────────────────

// TestSelectStringOp_Integration wires a SelectStringOp into a real graph and
// confirms it picks the IfTrue branch when Cond is true. The cond bool is
// supplied via IfStringEqOp comparing two ConstStringOps.
func TestSelectStringOp_Integration(t *testing.T) {
	g, err := graph.NewBuilder("select_string_demo").
		Vertex("a").Op("ConstStringOp").
		Params(map[string]string{"Value": "match"}).
		Output("Result", "a_w").
		Vertex("b").Op("ConstStringOp").
		Params(map[string]string{"Value": "match"}).
		Output("Result", "b_w").
		Vertex("eq").Op("IfStringEqOp").
		Input("A", "a_w").Input("B", "b_w").
		Output("Match", "cond_w").
		Vertex("if_true").Op("ConstStringOp").
		Params(map[string]string{"Value": "yes"}).
		Output("Result", "yes_w").
		Vertex("if_false").Op("ConstStringOp").
		Params(map[string]string{"Value": "no"}).
		Output("Result", "no_w").
		Vertex("sel").Op("SelectStringOp").
		Input("Cond", "cond_w").Input("IfTrue", "yes_w").Input("IfFalse", "no_w").
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
	if *v != "yes" {
		t.Errorf("final = %q, want %q", *v, "yes")
	}
}

// TestSelectStringOp_Integration_FalseBranch covers the false path.
func TestSelectStringOp_Integration_FalseBranch(t *testing.T) {
	g, err := graph.NewBuilder("select_string_false_demo").
		Vertex("a").Op("ConstStringOp").
		Params(map[string]string{"Value": "x"}).
		Output("Result", "a_w").
		Vertex("b").Op("ConstStringOp").
		Params(map[string]string{"Value": "y"}).
		Output("Result", "b_w").
		Vertex("eq").Op("IfStringEqOp").
		Input("A", "a_w").Input("B", "b_w").
		Output("Match", "cond_w").
		Vertex("if_true").Op("ConstStringOp").
		Params(map[string]string{"Value": "yes"}).
		Output("Result", "yes_w").
		Vertex("if_false").Op("ConstStringOp").
		Params(map[string]string{"Value": "no"}).
		Output("Result", "no_w").
		Vertex("sel").Op("SelectStringOp").
		Input("Cond", "cond_w").Input("IfTrue", "yes_w").Input("IfFalse", "no_w").
		Output("Result", "final").
		Build()
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	eng := runGraph(t, g)
	raw, _ := eng.GetOutput("final")
	v, _ := raw.(*string)
	if v == nil || *v != "no" {
		t.Errorf("final = %v, want \"no\"", v)
	}
}

// TestSwitchStringOp_Integration exercises the registered switch op with both
// a hit and a miss case to confirm Setup-driven param parsing flows through
// the registry.
func TestSwitchStringOp_Integration_Hit(t *testing.T) {
	g, err := graph.NewBuilder("switch_hit_demo").
		Vertex("k").Op("ConstStringOp").
		Params(map[string]string{"Value": "green"}).
		Output("Result", "k_w").
		Vertex("sw").Op("SwitchStringOp").
		Params(map[string]string{
			"cases":   `{"red":"stop","green":"go"}`,
			"default": "unknown",
		}).
		Input("Key", "k_w").
		Output("Result", "final").
		Build()
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	eng := runGraph(t, g)
	raw, _ := eng.GetOutput("final")
	v, _ := raw.(*string)
	if v == nil || *v != "go" {
		t.Errorf("final = %v, want \"go\"", v)
	}
}

func TestSwitchStringOp_Integration_Miss(t *testing.T) {
	g, err := graph.NewBuilder("switch_miss_demo").
		Vertex("k").Op("ConstStringOp").
		Params(map[string]string{"Value": "purple"}).
		Output("Result", "k_w").
		Vertex("sw").Op("SwitchStringOp").
		Params(map[string]string{
			"cases":   `{"red":"stop"}`,
			"default": "unknown",
		}).
		Input("Key", "k_w").
		Output("Result", "final").
		Build()
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	eng := runGraph(t, g)
	raw, _ := eng.GetOutput("final")
	v, _ := raw.(*string)
	if v == nil || *v != "unknown" {
		t.Errorf("final = %v, want \"unknown\"", v)
	}
}

// TestDefaultStringOp_Integration confirms the default op runs end-to-end
// when Value is supplied and is non-empty (so the value passes through).
func TestDefaultStringOp_Integration(t *testing.T) {
	g, err := graph.NewBuilder("default_string_demo").
		Vertex("v").Op("ConstStringOp").
		Params(map[string]string{"Value": "real"}).
		Output("Result", "v_w").
		Vertex("d").Op("ConstStringOp").
		Params(map[string]string{"Value": "fallback"}).
		Output("Result", "d_w").
		Vertex("def").Op("DefaultStringOp").
		Input("Value", "v_w").Input("Default", "d_w").
		Output("Result", "final").
		Build()
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	eng := runGraph(t, g)
	raw, _ := eng.GetOutput("final")
	v, _ := raw.(*string)
	if v == nil || *v != "real" {
		t.Errorf("final = %v, want \"real\"", v)
	}
}

// TestDefaultStringOp_Integration_EmptyFallsBack: empty value triggers the
// fallback path.
func TestDefaultStringOp_Integration_EmptyFallsBack(t *testing.T) {
	g, err := graph.NewBuilder("default_string_empty_demo").
		Vertex("v").Op("ConstStringOp").
		Params(map[string]string{"Value": ""}).
		Output("Result", "v_w").
		Vertex("d").Op("ConstStringOp").
		Params(map[string]string{"Value": "fallback"}).
		Output("Result", "d_w").
		Vertex("def").Op("DefaultStringOp").
		Input("Value", "v_w").Input("Default", "d_w").
		Output("Result", "final").
		Build()
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	eng := runGraph(t, g)
	raw, _ := eng.GetOutput("final")
	v, _ := raw.(*string)
	if v == nil || *v != "fallback" {
		t.Errorf("final = %v, want \"fallback\"", v)
	}
}

// TestDefaultFloat64Op_Integration confirms the float default op accepts
// zero as a valid value (does NOT fall back).
func TestDefaultFloat64Op_Integration_ZeroIsValid(t *testing.T) {
	g, err := graph.NewBuilder("default_float_zero_demo").
		Vertex("v").Op("ConstFloat64Op").
		Params(map[string]string{"Value": "0"}).
		Output("Result", "v_w").
		Vertex("d").Op("ConstFloat64Op").
		Params(map[string]string{"Value": "99"}).
		Output("Result", "d_w").
		Vertex("def").Op("DefaultFloat64Op").
		Input("Value", "v_w").Input("Default", "d_w").
		Output("Result", "final").
		Build()
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	eng := runGraph(t, g)
	raw, _ := eng.GetOutput("final")
	v, _ := raw.(*float64)
	if v == nil || *v != 0 {
		t.Errorf("final = %v, want 0 (zero must NOT fall back)", v)
	}
}
