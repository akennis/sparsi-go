// Package main is a GitHub README quality reporter.
//
// Given an owner/repo slug (or a fixture file), it fetches the README from
// the main and master branches in parallel, runs five AI quality probes
// concurrently, computes an average score, routes through one of three
// quality lanes (excellent / ok / poor), and appends a "tests not mentioned"
// warning when the has_tests probe returns false.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/akennis/sparsi-go/library"    // registers library ops
	_ "github.com/wwz16/dagor/operator/builtin" // registers CoalesceNStringOp

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/panjf2000/ants/v2"
	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/reporter"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/graph"
	"github.com/wwz16/dagor/operator"
	builtin "github.com/wwz16/dagor/operator/builtin"
	"github.com/wwz16/dagor/predicate"
)

// ─── Context keys ──────────────────────────────────────────────────────────

type (
	readmeBodyKey  struct{} // fixture: pre-read README text
	mainURLKey     struct{} // live: raw.githubusercontent.com main-branch URL
	masterURLKey   struct{} // live: raw.githubusercontent.com master-branch URL
	const200Key    struct{}
	const2Key      struct{}
	warningKey     struct{}
	emptyKey       struct{}
)

// ─── Custom ops ────────────────────────────────────────────────────────────

// StringTruncateOp caps its input to at most max_bytes UTF-8 bytes.
// Needed to stay within AI context limits for large READMEs.
type StringTruncateOp struct {
	Input    *string
	Result   string
	maxBytes int
}

func (op *StringTruncateOp) Setup(params *config.Params) error {
	s := params.GetString("max_bytes", "8192")
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("StringTruncateOp: invalid max_bytes %q: %w", s, err)
	}
	op.maxBytes = n
	return nil
}
func (op *StringTruncateOp) Reset() error { return nil }
func (op *StringTruncateOp) Run(ctx context.Context) error {
	s := *op.Input
	if len(s) > op.maxBytes {
		s = s[:op.maxBytes]
	}
	op.Result = s
	slog.DebugContext(ctx, "StringTruncateOp.done", "run_id", dagor.RunID(ctx), "in_bytes", len(*op.Input), "out_bytes", len(op.Result))
	return nil
}
func (op *StringTruncateOp) InputFields() map[string]any  { return map[string]any{"Input": &op.Input} }
func (op *StringTruncateOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *StringTruncateOp) SetInputField(field string, value any) error {
	if field != "Input" {
		return fmt.Errorf("StringTruncateOp: unknown field %q", field)
	}
	v, ok := value.(*string)
	if !ok {
		return fmt.Errorf("StringTruncateOp: Input: expected *string, got %T", value)
	}
	op.Input = v
	return nil
}
func (op *StringTruncateOp) ResetFields() { op.Input = nil; op.Result = "" }

func init() {
	mustReg := func(name string, f func() operator.IOperator) {
		if err := operator.RegisterOpFactory(name, f); err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}
	mustReg("readme_const",      builtin.ContextValFactory[string](readmeBodyKey{}))
	mustReg("main_url_const",    builtin.ContextValFactory[string](mainURLKey{}))
	mustReg("master_url_const",  builtin.ContextValFactory[string](masterURLKey{}))
	mustReg("const_200",         builtin.ContextValFactory[int](const200Key{}))
	mustReg("const_2",           builtin.ContextValFactory[float64](const2Key{}))
	mustReg("warning_const",     builtin.ContextValFactory[string](warningKey{}))
	mustReg("empty_const",       builtin.ContextValFactory[string](emptyKey{}))

	if err := operator.RegisterOp[StringTruncateOp](); err != nil {
		log.Fatalf("register StringTruncateOp: %v", err)
	}
}

// ─── Predicates ────────────────────────────────────────────────────────────

const (
	excellentMin = 0.75
	okMin        = 0.40
)

func registerPredicates() {
	mustReg := func(name string, fn func(map[string]any) bool) {
		if err := predicate.Register(name, fn); err != nil {
			log.Fatalf("register predicate %s: %v", name, err)
		}
	}
	avgScore := func(inputs map[string]any) (float64, bool) {
		v, ok := inputs["avg_score"].(*float64)
		if !ok || v == nil {
			return 0, false
		}
		return *v, true
	}
	mustReg("readme_excellent", func(in map[string]any) bool {
		s, ok := avgScore(in)
		return ok && s >= excellentMin
	})
	mustReg("readme_ok", func(in map[string]any) bool {
		s, ok := avgScore(in)
		return ok && s >= okMin && s < excellentMin
	})
	mustReg("readme_poor", func(in map[string]any) bool {
		s, ok := avgScore(in)
		return ok && s < okMin
	})
}

// ─── Graph ─────────────────────────────────────────────────────────────────

type sourceMode int

const (
	sourceLive sourceMode = iota
	sourceFixture
)

func buildGraph(mode sourceMode) (*graph.Graph, error) {
	b := graph.NewBuilder("readme_quality")

	// ── Stage 1: produce `readme_raw` ─────────────────────────────────────────
	// Fixture mode injects the file contents directly; live mode does two
	// parallel HTTP fetches and selects whichever returned HTTP 200.
	switch mode {
	case sourceFixture:
		b.
			Vertex("readme_const").Op("readme_const").
			Output("Result", "readme_raw")

	case sourceLive:
		b.
			Vertex("main_url_const").Op("main_url_const").
			Output("Result", "main_url").

			Vertex("master_url_const").Op("master_url_const").
			Output("Result", "master_url").

			// Both fetches run in parallel (no Condition).
			Vertex("fetch_main").Op("HTTPGetOp").
			Input("URL", "main_url").
			Output("Body", "main_body").
			Output("StatusCode", "main_status").

			Vertex("fetch_master").Op("HTTPGetOp").
			Input("URL", "master_url").
			Output("Body", "master_body").

			// Gate: did the main-branch fetch return HTTP 200?
			Vertex("const_200").Op("const_200").
			Output("Result", "int_200").

			Vertex("check_main").Op("IfIntEqOp").
			Input("A", "main_status").
			Input("B", "int_200").
			Output("Match", "main_ok").

			// Pick the body whose branch actually exists.
			Vertex("pick_readme").Op("SelectStringOp").
			Input("Cond", "main_ok").
			Input("IfTrue", "main_body").
			Input("IfFalse", "master_body").
			Output("Result", "readme_raw")
	}

	// ── Stage 2: truncate to 8 KB ─────────────────────────────────────────────
	b.
		Vertex("truncate").Op("StringTruncateOp").
		Params(map[string]string{"max_bytes": "8192"}).
		Input("Input", "readme_raw").
		Output("Result", "readme")

	// ── Stage 3: five parallel AI probes ──────────────────────────────────────
	b.
		Vertex("purpose_op").Op("AIComputeStringToStringOp").
		Params(map[string]string{
			"operation": "summarize the purpose of this project in one concise sentence",
		}).
		Input("Input", "readme").
		Output("Result", "purpose_str").

		Vertex("doc_score_op").Op("AIScoreOp").
		Params(map[string]string{"criterion": "documentation completeness"}).
		Input("Input", "readme").
		Output("Result", "doc_score").

		Vertex("clarity_op").Op("AIScoreOp").
		Params(map[string]string{"criterion": "clarity for new contributors"}).
		Input("Input", "readme").
		Output("Result", "clarity_score").

		Vertex("has_tests_op").Op("AIBoolOp").
		Params(map[string]string{"predicate": "does this README mention tests, CI, or automated checks?"}).
		Input("Input", "readme").
		Output("Result", "has_tests").

		Vertex("has_install_op").Op("AIBoolOp").
		Params(map[string]string{"predicate": "does this README contain installation or usage instructions?"}).
		Input("Input", "readme").
		Output("Result", "has_install")

	// ── Stage 4: deterministic average score ──────────────────────────────────
	b.
		Vertex("sum_scores").Op("AddFloatOp").
		Input("A", "doc_score").
		Input("B", "clarity_score").
		Output("Result", "sum_scores_val").

		Vertex("const_2").Op("const_2").
		Output("Result", "two_f").

		Vertex("avg_score_op").Op("DivFloatOp").
		Input("A", "sum_scores_val").
		Input("B", "two_f").
		Output("Result", "avg_score")

	// ── Stage 5: three quality lanes ──────────────────────────────────────────
	// Each lane is gated by a predicate over avg_score; exactly one fires.
	lanes := []struct {
		name      string
		condition string
		operation string
	}{
		{
			name:      "excellent",
			condition: "readme_excellent",
			operation: "write a one-paragraph endorsement of this README, " +
				"highlighting what makes it exemplary for open-source projects",
		},
		{
			name:      "ok",
			condition: "readme_ok",
			operation: "write a one-paragraph constructive critique of this README " +
				"with 2 specific, actionable suggestions for improvement",
		},
		{
			name:      "poor",
			condition: "readme_poor",
			operation: "write a one-paragraph improvement plan for this README " +
				"listing the 3 highest-impact fixes that would help new contributors",
		},
	}
	for _, lane := range lanes {
		b.
			Vertex(lane.name + "_lane").Op("AIComputeStringToStringOp").
			Condition(lane.condition).
			ConditionInput("avg_score").
			Params(map[string]string{"operation": lane.operation}).
			Input("Input", "readme").
			Output("Result", lane.name+"_text")
	}

	// ── Stage 6: coalesce lanes + append optional warning ─────────────────────
	b.
		Vertex("narrative_op").Op("CoalesceNStringOp").
		Params(map[string]int{"n": 3}).
		Merge(config.MergeCoalesce).
		Input("Input0", "excellent_text").
		Input("Input1", "ok_text").
		Input("Input2", "poor_text").
		Output("Result", "narrative").

		// "WARNING: tests not mentioned" — injected when has_tests is false.
		// SelectStringOp(cond=has_tests, ifTrue="", ifFalse=warning_text)
		Vertex("warning_const").Op("warning_const").
		Output("Result", "warning_text").

		Vertex("empty_const").Op("empty_const").
		Output("Result", "empty_text").

		Vertex("test_warning").Op("SelectStringOp").
		Input("Cond", "has_tests").
		Input("IfTrue", "empty_text").
		Input("IfFalse", "warning_text").
		Output("Result", "test_warning_str").

		Vertex("final_narrative").Op("StringConcatOp").
		Input("A", "narrative").
		Input("B", "test_warning_str").
		Output("Result", "final_narrative")

	return b.Build()
}

// ─── Driver ────────────────────────────────────────────────────────────────

// UserInput is the external boundary of the workflow. In CLI mode it is filled
// from --slug; in MCP mode the SDK derives the tool input schema from this
// struct and deserializes + validates every tools/call request into it. The
// MCP path always fetches the README live — the --fixture offline mode is a
// CLI-only convenience resolved before the shared path.
type UserInput struct {
	Slug string `json:"slug" jsonschema:"the owner/repo slug whose README to assess, e.g. golang/go; required"`
}

// Result is the workflow's structured output: the MCP tool's typed Out (the
// SDK emits it as structured content + a JSON text block) and the CLI's stdout.
type Result struct {
	Slug         string   `json:"slug"`
	Purpose      string   `json:"purpose"`
	DocScore     float64  `json:"doc_score"`
	ClarityScore float64  `json:"clarity_score"`
	AvgScore     float64  `json:"avg_score"`
	HasTests     bool     `json:"has_tests"`
	HasInstall   bool     `json:"has_install"`
	Verdict      string   `json:"verdict"`
	Narrative    string   `json:"narrative"`
	AINodes      []string `json:"ai_nodes"`
}

// runConfig is the resolved data source for one execution: either a live
// owner/repo (two raw.githubusercontent.com URLs) or a pre-read fixture body.
type runConfig struct {
	mode        sourceMode
	displaySlug string
	readmeBody  string
	mainURL     string
	masterURL   string
}

// ─── Shared execution path ─────────────────────────────────────────────────

// execute builds a fresh graph + engine for one resolved runConfig and
// extracts the structured Result. A fresh graph per invocation keeps
// concurrent MCP tool calls from sharing mutable operator state; the
// ants.Pool is safe to share.
func execute(ctx context.Context, pool *ants.Pool, cfg runConfig) (Result, error) {
	g, err := buildGraph(cfg.mode)
	if err != nil {
		return Result{}, fmt.Errorf("build graph: %w", err)
	}

	eng, err := dagor.NewEngine(g, pool, dagor.WithReporter(reporter.New(slog.Default())))
	if err != nil {
		return Result{}, fmt.Errorf("create engine: %w", err)
	}

	ctx = context.WithValue(ctx, const200Key{}, 200)
	ctx = context.WithValue(ctx, const2Key{}, 2.0)
	ctx = context.WithValue(ctx, warningKey{}, "\n\nWARNING: tests not mentioned")
	ctx = context.WithValue(ctx, emptyKey{}, "")
	if cfg.mode == sourceFixture {
		ctx = context.WithValue(ctx, readmeBodyKey{}, cfg.readmeBody)
	} else {
		ctx = context.WithValue(ctx, mainURLKey{}, cfg.mainURL)
		ctx = context.WithValue(ctx, masterURLKey{}, cfg.masterURL)
	}

	if err := eng.Run(ctx); err != nil {
		return Result{}, fmt.Errorf("run graph: %w", err)
	}

	out := Result{Slug: cfg.displaySlug}

	if v, ok := getString(eng, "purpose_str"); ok {
		out.Purpose = v
	}
	if v, ok := getFloat(eng, "doc_score"); ok {
		out.DocScore = v
	}
	if v, ok := getFloat(eng, "clarity_score"); ok {
		out.ClarityScore = v
	}
	if v, ok := getFloat(eng, "avg_score"); ok {
		out.AvgScore = v
	}
	if v, ok := getBool(eng, "has_tests"); ok {
		out.HasTests = v
	}
	if v, ok := getBool(eng, "has_install"); ok {
		out.HasInstall = v
	}
	if v, ok := getString(eng, "final_narrative"); ok {
		out.Narrative = v
	}

	// Derive verdict from avg_score — same thresholds as the predicates.
	switch {
	case out.AvgScore >= excellentMin:
		out.Verdict = "excellent"
	case out.AvgScore >= okMin:
		out.Verdict = "ok"
	default:
		out.Verdict = "poor"
	}

	// Record which AI vertices actually fired.
	candidates := []struct{ op, vertex string }{
		{"AIComputeStringToStringOp(purpose)", "purpose_op"},
		{"AIScoreOp(doc_score)", "doc_score_op"},
		{"AIScoreOp(clarity_score)", "clarity_op"},
		{"AIBoolOp(has_tests)", "has_tests_op"},
		{"AIBoolOp(has_install)", "has_install_op"},
		{"AIComputeStringToStringOp(excellent)", "excellent_lane"},
		{"AIComputeStringToStringOp(ok)", "ok_lane"},
		{"AIComputeStringToStringOp(poor)", "poor_lane"},
	}
	for _, c := range candidates {
		if !eng.VertexSkipped(c.vertex) {
			out.AINodes = append(out.AINodes, c.op)
		}
	}
	return out, nil
}

// runWorkflow is the live path shared by the MCP tool and CLI live mode: it
// derives the main/master README URLs for the slug, then runs the graph.
func runWorkflow(ctx context.Context, pool *ants.Pool, in UserInput) (Result, error) {
	cfg := runConfig{
		mode:        sourceLive,
		displaySlug: in.Slug,
		mainURL:     "https://raw.githubusercontent.com/" + in.Slug + "/main/README.md",
		masterURL:   "https://raw.githubusercontent.com/" + in.Slug + "/master/README.md",
	}
	return execute(ctx, pool, cfg)
}

// ─── MCP server mode ───────────────────────────────────────────────────────

// runMCPServer exposes the workflow as one MCP tool over stdin/stdout. The SDK
// infers the tool input schema from UserInput, auto-deserializes + validates
// each request, and (because Out is non-any) emits Result as structured
// content plus a JSON text block, so the handler returns nil for the result.
func runMCPServer(pool *ants.Pool) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "readme-quality",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "assess_readme",
		Description: "Fetch a GitHub repository's README and run five AI quality probes, returning per-criterion scores, a verdict, and a narrative critique.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in UserInput) (*mcp.CallToolResult, Result, error) {
		if strings.TrimSpace(in.Slug) == "" {
			return nil, Result{}, fmt.Errorf("slug is required")
		}
		res, err := runWorkflow(ctx, pool, in)
		if err != nil {
			return nil, Result{}, err
		}
		return nil, res, nil
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("mcp server: %v", err)
	}
}

// ─── Entrypoint ────────────────────────────────────────────────────────────

func main() {
	var (
		mcpMode = flag.Bool("mcp", false, "run as a stdio MCP server instead of a one-shot CLI")
		slug    = flag.String("slug", "", "owner/repo slug, e.g. golang/go (CLI mode)")
		fixture = flag.String("fixture", "", "path to a pre-saved README file, offline mode (CLI mode)")
	)
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	registerPredicates()

	pool, err := ants.NewPool(10)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	defer pool.Release()

	if *mcpMode {
		runMCPServer(pool)
		return
	}

	if *slug == "" && *fixture == "" {
		fmt.Fprintln(os.Stderr, "usage: 03-readme-quality --slug <owner/repo>  |  --fixture <path>  |  --mcp")
		os.Exit(2)
	}
	if *slug != "" && *fixture != "" {
		fmt.Fprintln(os.Stderr, "specify exactly one of --slug or --fixture")
		os.Exit(2)
	}

	// CLI keeps the offline convenience: resolve the data source here
	// (live slug or --fixture) and feed the shared execution path.
	var cfg runConfig
	if *fixture != "" {
		raw, err := os.ReadFile(*fixture)
		if err != nil {
			log.Fatalf("read fixture: %v", err)
		}
		cfg = runConfig{mode: sourceFixture, displaySlug: *fixture, readmeBody: string(raw)}
	} else {
		cfg = runConfig{
			mode:        sourceLive,
			displaySlug: *slug,
			mainURL:     "https://raw.githubusercontent.com/" + *slug + "/main/README.md",
			masterURL:   "https://raw.githubusercontent.com/" + *slug + "/master/README.md",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	res, err := execute(ctx, pool, cfg)
	if err != nil {
		log.Fatalf("workflow: %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		log.Fatalf("encode output: %v", err)
	}
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func getString(eng *dagor.Engine, name string) (string, bool) {
	raw, ok := eng.GetOutput(name)
	if !ok {
		return "", false
	}
	p, ok := raw.(*string)
	if !ok || p == nil {
		return "", false
	}
	return *p, true
}

func getFloat(eng *dagor.Engine, name string) (float64, bool) {
	raw, ok := eng.GetOutput(name)
	if !ok {
		return 0, false
	}
	p, ok := raw.(*float64)
	if !ok || p == nil {
		return 0, false
	}
	return *p, true
}

func getBool(eng *dagor.Engine, name string) (bool, bool) {
	raw, ok := eng.GetOutput(name)
	if !ok {
		return false, false
	}
	p, ok := raw.(*bool)
	if !ok || p == nil {
		return false, false
	}
	return *p, true
}
