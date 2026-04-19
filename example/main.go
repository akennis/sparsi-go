// Package main is an arithmetic workflow that parses a two-operator,
// three-operand expression (e.g. "5 * 3 + 2") and solves it using a
// deterministic DAG.  AI parses the expression and resolves precedence;
// AddOp/SubOp/DivOp handle the arithmetic deterministically; and
// AIComputeMathOperandsToFloat64Op handles any multiplication step.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/akennis/clawdag-go/library"   // registers library ops
	_ "github.com/wwz16/dagor/operator/builtin" // registers CoalesceNFloat64Op etc.

	"github.com/google/generative-ai-go/genai"
	"github.com/panjf2000/ants/v2"
	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/graph"
	"github.com/wwz16/dagor/operator"
	"github.com/wwz16/dagor/predicate"
	"google.golang.org/api/option"
)

// ─────────────────────────────────────────────────────────────────────────────
// ParseExpressionOp — AI-powered expression parser
//
// Calls AI to parse the arithmetic expression and determine operator precedence.
// Returns the two operations in execution order so the DAG can compute them
// left-to-right with the correct precedence without any further branching logic.
//
//	FirstOp  is computed first:  result1 = FirstA <FirstOp>  FirstB
//	SecondOp is computed second: result2 = result1 <SecondOp> SecondB
// ─────────────────────────────────────────────────────────────────────────────

type ParseExpressionOp struct {
	Expression *string

	FirstA   float64
	FirstB   float64
	FirstOp  string // "add" | "sub" | "mul" | "div"
	SecondOp string // "add" | "sub" | "mul" | "div"
	SecondB  float64
}

func (op *ParseExpressionOp) Setup(_ *config.Params) error { return nil }
func (op *ParseExpressionOp) Reset() error                 { return nil }

func (op *ParseExpressionOp) InputFields() map[string]any {
	return map[string]any{"Expression": &op.Expression}
}

func (op *ParseExpressionOp) OutputFields() map[string]any {
	return map[string]any{
		"FirstA":   &op.FirstA,
		"FirstB":   &op.FirstB,
		"FirstOp":  &op.FirstOp,
		"SecondOp": &op.SecondOp,
		"SecondB":  &op.SecondB,
	}
}

func (op *ParseExpressionOp) SetInputField(field string, value any) error {
	if value == nil {
		return nil
	}
	if field == "Expression" {
		v, ok := value.(*string)
		if !ok {
			return fmt.Errorf("ParseExpressionOp: Expression: expected *string, got %T", value)
		}
		op.Expression = v
		return nil
	}
	return fmt.Errorf("ParseExpressionOp: unknown field %q", field)
}

func (op *ParseExpressionOp) ResetFields() {
	op.Expression = nil
	op.FirstA = 0
	op.FirstB = 0
	op.FirstOp = ""
	op.SecondOp = ""
	op.SecondB = 0
}

func (op *ParseExpressionOp) Run(ctx context.Context) error {
	apiKey := os.Getenv("GOOGLE_GENAI_API_KEY")
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return fmt.Errorf("genai client: %w", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-2.5-flash-lite")
	model.ResponseMIMEType = "application/json"
	model.ResponseSchema = &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"first_a":   {Type: genai.TypeNumber},
			"first_b":   {Type: genai.TypeNumber},
			"first_op":  {Type: genai.TypeString},
			"second_op": {Type: genai.TypeString},
			"second_b":  {Type: genai.TypeNumber},
		},
		Required: []string{"first_a", "first_b", "first_op", "second_op", "second_b"},
	}

	prompt := fmt.Sprintf(`Parse the arithmetic expression "%s".

It contains exactly two binary operators and three operands.
Apply standard operator precedence (* and / before + and -),
and left-to-right ordering for operators of equal precedence.

Return the two computations in execution order:
- first_a, first_b: the two operands for the FIRST computation
- first_op: one of "add", "sub", "mul", "div" — applied FIRST
- second_op: one of "add", "sub", "mul", "div" — applied to (result_of_first, second_b)
- second_b: the remaining operand for the second computation

Examples:
  "5 * 3 + 2"  → first_a=5, first_b=3, first_op=mul, second_op=add, second_b=2  (5*3=15, 15+2=17)
  "2 + 8 / 4"  → first_a=8, first_b=4, first_op=div, second_op=add, second_b=2  (8/4=2,  2+2=4)
  "10 - 3 - 2" → first_a=10, first_b=3, first_op=sub, second_op=sub, second_b=2 (10-3=7, 7-2=5)`, *op.Expression)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return fmt.Errorf("generate content: %w", err)
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return fmt.Errorf("no candidates in response")
	}

	var raw string
	for _, part := range resp.Candidates[0].Content.Parts {
		raw += fmt.Sprintf("%v", part)
	}

	var parsed struct {
		FirstA   float64 `json:"first_a"`
		FirstB   float64 `json:"first_b"`
		FirstOp  string  `json:"first_op"`
		SecondOp string  `json:"second_op"`
		SecondB  float64 `json:"second_b"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return fmt.Errorf("parse AI response: %w\nraw: %s", err, raw)
	}

	op.FirstA = parsed.FirstA
	op.FirstB = parsed.FirstB
	op.FirstOp = parsed.FirstOp
	op.SecondOp = parsed.SecondOp
	op.SecondB = parsed.SecondB

	log.Printf("[DEBUG] ParseExpressionOp: first=%v %s %v, second=<result> %s %v",
		op.FirstA, op.FirstOp, op.FirstB, op.SecondOp, op.SecondB)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// ABPassthroughOp — operator-keyed routing gate
//
// Passes A and B through to AOut and BOut unchanged.  The Op input is not used
// in Run; it exists solely so that condition predicates registered against this
// op can inspect it (via the wire name in the predicate's inputs map) to decide
// whether this gate vertex should execute.
//
// When the predicate returns false the vertex is skipped, which in turn causes
// all downstream vertices (e.g. AddOp, SubOp, DivOp) to also be skipped.
// ─────────────────────────────────────────────────────────────────────────────

type ABPassthroughOp struct {
	A    *float64
	B    *float64
	Op   *string // inspected by predicate, not by Run
	AOut float64
	BOut float64
}

func (op *ABPassthroughOp) Setup(_ *config.Params) error { return nil }
func (op *ABPassthroughOp) Reset() error                 { return nil }

func (op *ABPassthroughOp) InputFields() map[string]any {
	return map[string]any{
		"A":  &op.A,
		"B":  &op.B,
		"Op": &op.Op,
	}
}

func (op *ABPassthroughOp) OutputFields() map[string]any {
	return map[string]any{
		"AOut": &op.AOut,
		"BOut": &op.BOut,
	}
}

func (op *ABPassthroughOp) SetInputField(field string, value any) error {
	if value == nil {
		return nil
	}
	switch field {
	case "A":
		v, ok := value.(*float64)
		if !ok {
			return fmt.Errorf("ABPassthroughOp: A: expected *float64, got %T", value)
		}
		op.A = v
	case "B":
		v, ok := value.(*float64)
		if !ok {
			return fmt.Errorf("ABPassthroughOp: B: expected *float64, got %T", value)
		}
		op.B = v
	case "Op":
		v, ok := value.(*string)
		if !ok {
			return fmt.Errorf("ABPassthroughOp: Op: expected *string, got %T", value)
		}
		op.Op = v
	default:
		return fmt.Errorf("ABPassthroughOp: unknown field %q", field)
	}
	return nil
}

func (op *ABPassthroughOp) ResetFields() {
	op.A = nil
	op.B = nil
	op.Op = nil
	op.AOut = 0
	op.BOut = 0
}

func (op *ABPassthroughOp) Run(_ context.Context) error {
	op.AOut = *op.A
	op.BOut = *op.B
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// init — register custom ops
// ─────────────────────────────────────────────────────────────────────────────

func init() {
	if err := operator.RegisterOp[ParseExpressionOp](); err != nil {
		log.Fatalf("register ParseExpressionOp: %v", err)
	}
	if err := operator.RegisterOp[ABPassthroughOp](); err != nil {
		log.Fatalf("register ABPassthroughOp: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// registerPredicates — routing predicates for operator dispatch
//
// Each predicate checks the Op wire name in the inputs map.  The engine
// populates this map with the *string pointer stored in the upstream
// ParseExpressionOp's output field — so *v is the actual operator string at
// runtime.
// ─────────────────────────────────────────────────────────────────────────────

func registerPredicates() {
	for _, entry := range []struct {
		name string
		wire string
		op   string
	}{
		{"first_op_is_add", "first_op", "add"},
		{"first_op_is_sub", "first_op", "sub"},
		{"first_op_is_div", "first_op", "div"},
		{"first_op_is_mul", "first_op", "mul"},
		{"second_op_is_add", "second_op", "add"},
		{"second_op_is_sub", "second_op", "sub"},
		{"second_op_is_div", "second_op", "div"},
		{"second_op_is_mul", "second_op", "mul"},
	} {
		wire := entry.wire
		opName := entry.op
		if err := predicate.Register(entry.name, func(inputs map[string]any) bool {
			v, ok := inputs[wire].(*string)
			return ok && v != nil && *v == opName
		}); err != nil {
			log.Fatalf("register predicate %s: %v", entry.name, err)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildGraph — DAG definition
//
// Graph topology (repeated for each level):
//
//   StringConstOp ──► ParseExpressionOp
//                         │
//              ┌──────────┼──────────┐──────────┐
//          gate_add   gate_sub   gate_div   gate_mul   (exactly one runs)
//              │          │          │          │
//           AddOp      SubOp      DivOp   Pack+AIMul
//              └──────────┴──────────┴──────────┘
//                           CoalesceNFloat64Op  ──► (level result)
//
// Level 1 uses (first_a, first_b, first_op); level 2 uses (level1_result, second_b, second_op).
// ─────────────────────────────────────────────────────────────────────────────

func buildGraph(expression string) (*graph.Graph, error) {
	return graph.NewBuilder("arithmetic").

		// ── Input: inject user expression as a constant ──────────────────────
		Vertex("expr_const").Op("StringConstOp").
		Params(map[string]string{"Value": expression}).
		Output("Result", "user_expr").

		// ── AI parse: determine execution order ──────────────────────────────
		Vertex("parse").Op("ParseExpressionOp").
		Input("Expression", "user_expr").
		Output("FirstA", "first_a").
		Output("FirstB", "first_b").
		Output("FirstOp", "first_op").
		Output("SecondOp", "second_op").
		Output("SecondB", "second_b").

		// ── Level 1 gates (exactly one will run) ─────────────────────────────
		Vertex("gate1_add").Op("ABPassthroughOp").
		Condition("first_op_is_add").
		Input("A", "first_a").Input("B", "first_b").Input("Op", "first_op").
		Output("AOut", "g1a_a").Output("BOut", "g1a_b").

		Vertex("gate1_sub").Op("ABPassthroughOp").
		Condition("first_op_is_sub").
		Input("A", "first_a").Input("B", "first_b").Input("Op", "first_op").
		Output("AOut", "g1s_a").Output("BOut", "g1s_b").

		Vertex("gate1_div").Op("ABPassthroughOp").
		Condition("first_op_is_div").
		Input("A", "first_a").Input("B", "first_b").Input("Op", "first_op").
		Output("AOut", "g1d_a").Output("BOut", "g1d_b").

		Vertex("gate1_mul").Op("ABPassthroughOp").
		Condition("first_op_is_mul").
		Input("A", "first_a").Input("B", "first_b").Input("Op", "first_op").
		Output("AOut", "g1m_a").Output("BOut", "g1m_b").

		// ── Level 1 deterministic ops (library) ──────────────────────────────
		Vertex("add1").Op("AddOp").
		Input("A", "g1a_a").Input("B", "g1a_b").
		Output("Result", "l1_add").

		Vertex("sub1").Op("SubOp").
		Input("A", "g1s_a").Input("B", "g1s_b").
		Output("Result", "l1_sub").

		Vertex("div1").Op("DivOp").
		Input("A", "g1d_a").Input("B", "g1d_b").
		Output("Result", "l1_div").

		// ── Level 1 multiplication via AI (no MulOp in library) ──────────────
		Vertex("pack1").Op("PackMathOperandsOp").
		Input("A", "g1m_a").Input("B", "g1m_b").
		Output("Result", "pack1_out").

		Vertex("ai_mul1").Op("AIComputeMathOperandsToFloat64Op").
		Params(map[string]string{"operation": "multiply A by B"}).
		Input("Input", "pack1_out").
		Output("Result", "l1_mul").

		// ── Coalesce level 1: first non-nil result wins ───────────────────────
		Vertex("coalesce1").Op("CoalesceNFloat64Op").
		Params(map[string]int{"n": 4}).
		Merge(config.MergeCoalesce).
		Input("Input0", "l1_add").
		Input("Input1", "l1_sub").
		Input("Input2", "l1_div").
		Input("Input3", "l1_mul").
		Output("Result", "first_result").

		// ── Level 2 gates (exactly one will run) ─────────────────────────────
		Vertex("gate2_add").Op("ABPassthroughOp").
		Condition("second_op_is_add").
		Input("A", "first_result").Input("B", "second_b").Input("Op", "second_op").
		Output("AOut", "g2a_a").Output("BOut", "g2a_b").

		Vertex("gate2_sub").Op("ABPassthroughOp").
		Condition("second_op_is_sub").
		Input("A", "first_result").Input("B", "second_b").Input("Op", "second_op").
		Output("AOut", "g2s_a").Output("BOut", "g2s_b").

		Vertex("gate2_div").Op("ABPassthroughOp").
		Condition("second_op_is_div").
		Input("A", "first_result").Input("B", "second_b").Input("Op", "second_op").
		Output("AOut", "g2d_a").Output("BOut", "g2d_b").

		Vertex("gate2_mul").Op("ABPassthroughOp").
		Condition("second_op_is_mul").
		Input("A", "first_result").Input("B", "second_b").Input("Op", "second_op").
		Output("AOut", "g2m_a").Output("BOut", "g2m_b").

		// ── Level 2 deterministic ops (library) ──────────────────────────────
		Vertex("add2").Op("AddOp").
		Input("A", "g2a_a").Input("B", "g2a_b").
		Output("Result", "l2_add").

		Vertex("sub2").Op("SubOp").
		Input("A", "g2s_a").Input("B", "g2s_b").
		Output("Result", "l2_sub").

		Vertex("div2").Op("DivOp").
		Input("A", "g2d_a").Input("B", "g2d_b").
		Output("Result", "l2_div").

		// ── Level 2 multiplication via AI ─────────────────────────────────────
		Vertex("pack2").Op("PackMathOperandsOp").
		Input("A", "g2m_a").Input("B", "g2m_b").
		Output("Result", "pack2_out").

		Vertex("ai_mul2").Op("AIComputeMathOperandsToFloat64Op").
		Params(map[string]string{"operation": "multiply A by B"}).
		Input("Input", "pack2_out").
		Output("Result", "l2_mul").

		// ── Coalesce level 2: final result ────────────────────────────────────
		Vertex("coalesce2").Op("CoalesceNFloat64Op").
		Params(map[string]int{"n": 4}).
		Merge(config.MergeCoalesce).
		Input("Input0", "l2_add").
		Input("Input1", "l2_sub").
		Input("Input2", "l2_div").
		Input("Input3", "l2_mul").
		Output("Result", "final_result").

		Build()
}

// ─────────────────────────────────────────────────────────────────────────────
// main
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	fmt.Println("Arithmetic Workflow")
	fmt.Println("Enter a two-operator, three-operand arithmetic expression.")
	fmt.Println("Supported operators: + - * /")
	fmt.Println("Examples: 5 * 3 + 2   |   8 / 2 + 1   |   10 - 3 * 2")
	fmt.Print("\nExpression: ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		log.Fatalf("reading input: %v", err)
	}
	expression := strings.TrimSpace(line)
	if expression == "" {
		log.Fatal("expression cannot be empty")
	}

	registerPredicates()

	g, err := buildGraph(expression)
	if err != nil {
		log.Fatalf("build graph: %v", err)
	}

	pool, err := ants.NewPool(10)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	defer pool.Release()

	eng, err := dagor.NewEngine(g, pool)
	if err != nil {
		log.Fatalf("create engine: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := eng.Run(ctx); err != nil {
		log.Fatalf("run graph: %v", err)
	}

	raw, ok := eng.GetOutput("final_result")
	if !ok {
		log.Fatal("final_result wire not found in graph output")
	}
	result, ok := raw.(*float64)
	if !ok || result == nil {
		log.Fatalf("unexpected type for final_result: %T", raw)
	}

	output := map[string]any{
		"expression": expression,
		"result":     *result,
	}
	enc, _ := json.Marshal(output)
	fmt.Println(string(enc))
}
