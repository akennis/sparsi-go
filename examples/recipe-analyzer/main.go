// Package main is a recipe difficulty analyzer.
//
// Given a meal name (or a captured TheMealDB fixture), it extracts the
// instructions, runs three AI extractors in parallel (ingredients, steps,
// estimated cook minutes), computes a deterministic difficulty score from
// those signals, then routes the result through one of three difficulty-
// specific advice lanes.  The lanes are gated by predicates registered
// against the score wire and merged with CoalesceNStringOp.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/akennis/sparsi-go/library"
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
	bodyKey             struct{} // fixture: raw API JSON
	urlKey              struct{} // live: TheMealDB URL
	pathInstructionsKey struct{}
	pathMealnameKey     struct{}
	stepWeightKey       struct{}
	cookWeightKey       struct{}
)

func init() {
	mustReg := func(name string, f func() operator.IOperator) {
		if err := operator.RegisterOpFactory(name, f); err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}
	mustReg("body_const",          builtin.ContextValFactory[string](bodyKey{}))
	mustReg("url_const",           builtin.ContextValFactory[string](urlKey{}))
	mustReg("path_instructions",   builtin.ContextValFactory[string](pathInstructionsKey{}))
	mustReg("path_mealname",       builtin.ContextValFactory[string](pathMealnameKey{}))
	mustReg("step_weight",         builtin.ContextValFactory[float64](stepWeightKey{}))
	mustReg("cook_weight",         builtin.ContextValFactory[float64](cookWeightKey{}))
}

// ─── Predicates ────────────────────────────────────────────────────────────

const (
	easyMax = 20.0
	hardMin = 50.0
)

func registerPredicates() {
	mustReg := func(name string, fn func(map[string]any) bool) {
		if err := predicate.Register(name, fn); err != nil {
			log.Fatalf("register predicate %s: %v", name, err)
		}
	}
	score := func(inputs map[string]any) (float64, bool) {
		v, ok := inputs["difficulty_score"].(*float64)
		if !ok || v == nil {
			return 0, false
		}
		return *v, true
	}
	mustReg("score_is_easy", func(in map[string]any) bool {
		s, ok := score(in)
		return ok && s < easyMax
	})
	mustReg("score_is_medium", func(in map[string]any) bool {
		s, ok := score(in)
		return ok && s >= easyMax && s < hardMin
	})
	mustReg("score_is_hard", func(in map[string]any) bool {
		s, ok := score(in)
		return ok && s >= hardMin
	})
}

// ─── Graph ─────────────────────────────────────────────────────────────────

type sourceMode int

const (
	sourceLive sourceMode = iota
	sourceFixture
)

func buildGraph(mode sourceMode) (*graph.Graph, error) {
	b := graph.NewBuilder("recipe_analyzer")

	// Stage 1 — produce a single wire `raw_json` containing the API response.
	var vb *graph.VertexBuilder
	switch mode {
	case sourceFixture:
		vb = b.
			Vertex("body_const").Op("body_const").
			Output("Result", "raw_json")
	case sourceLive:
		vb = b.
			Vertex("url_const").Op("url_const").
			Output("Result", "url").

			Vertex("fetch").Op("HTTPGetOp").
			Input("URL", "url").
			Output("Body", "raw_json").
			Output("StatusCode", "http_status")
	}

	// Stage 2 — pull instructions and meal name out of the JSON.
	vb = vb.
		Vertex("path_instructions").Op("path_instructions").
		Output("Result", "path_instructions").

		Vertex("path_mealname").Op("path_mealname").
		Output("Result", "path_mealname").

		Vertex("extract_instructions").Op("JSONExtractOp").
		Params(map[string]bool{"required": true}).
		Input("JSON", "raw_json").
		Input("Path", "path_instructions").
		Output("Value", "instructions_text").

		Vertex("extract_mealname").Op("JSONExtractOp").
		Input("JSON", "raw_json").
		Input("Path", "path_mealname").
		Output("Value", "meal_name").

		// Stage 3 — three parallel AI extractors over the instructions text.
		Vertex("ingredients").Op("AIExtractStringSliceOp").
		Params(map[string]string{"operation": "extract every distinct ingredient name from this recipe as a flat list (one ingredient per item; no quantities or units)", "provider": "gemini", "model": "gemini-3-flash-preview"}).
		Input("Input", "instructions_text").
		Output("Result", "ingredients").

		Vertex("steps").Op("AIExtractStringSliceOp").
		Params(map[string]string{"operation": "extract every discrete step required to prepare and cook the meal as a flat list (one step per item); exclude optional storage (like freezing) and serving suggestions", "provider": "gemini", "model": "gemini-3-flash-preview"}).
		Input("Input", "instructions_text").
		Output("Result", "steps").

		Vertex("cook_minutes").Op("AIParseNumberOp").
		Params(map[string]string{"operation": "estimate total active and passive cooking time in minutes; if a step is optional or provides a time range, use the minimum time; respond with a single integer", "provider": "gemini", "model": "gemini-3-flash-preview"}).
		Input("Input", "instructions_text").
		Output("Result", "cook_minutes").

		// Stage 4 — count via SliceLenOp, widen to float64, then score.
		Vertex("ingredient_count_int").Op("SliceLenOp").
		Input("Input", "ingredients").
		Output("Result", "ingredient_count_int").

		Vertex("step_count_int").Op("SliceLenOp").
		Input("Input", "steps").
		Output("Result", "step_count_int").

		Vertex("ingredient_count").Op("IntToFloat64Op").
		Input("Value", "ingredient_count_int").
		Output("Result", "ingredient_count_f").

		Vertex("step_count").Op("IntToFloat64Op").
		Input("Value", "step_count_int").
		Output("Result", "step_count_f").

		Vertex("step_weight").Op("step_weight").
		Output("Result", "step_weight").

		Vertex("cook_weight").Op("cook_weight").
		Output("Result", "cook_weight").

		Vertex("step_term").Op("MulFloatOp").
		Input("A", "step_count_f").
		Input("B", "step_weight").
		Output("Result", "step_term").

		Vertex("cook_term").Op("MulFloatOp").
		Input("A", "cook_minutes").
		Input("B", "cook_weight").
		Output("Result", "cook_term").

		Vertex("partial_score").Op("AddFloatOp").
		Input("A", "ingredient_count_f").
		Input("B", "step_term").
		Output("Result", "partial_score").

		Vertex("difficulty_score").Op("AddFloatOp").
		Input("A", "partial_score").
		Input("B", "cook_term").
		Output("Result", "difficulty_score")
	_ = vb

	// Stage 5 — three difficulty lanes, each gated by a predicate over
	// `difficulty_score` and feeding a difficulty-specific AI advice op.
	lanes := []struct {
		name      string
		condition string
		operation string
	}{
		{
			name:      "easy",
			condition: "score_is_easy",
			operation: "write a one-sentence encouraging tip for a beginner cook making this recipe; reference the recipe by name",
		},
		{
			name:      "medium",
			condition: "score_is_medium",
			operation: "write a one-sentence intermediate tip for a home cook attempting this recipe; reference the recipe by name",
		},
		{
			name:      "hard",
			condition: "score_is_hard",
			operation: "write a one-sentence pro-level tip for an experienced cook tackling this recipe; reference the recipe by name",
		},
	}
	// MULTI-VERTEX LANE RULE (prompts/codegen.md): the predicate goes on the
	// branch op itself; no per-lane gate vertex.
	for _, lane := range lanes {
		b.
			Vertex(lane.name+"_advice").Op("AIComputeStringToStringOp").
			Condition(lane.condition).
			ConditionInput("difficulty_score").
			Params(map[string]string{"operation": lane.operation, "provider": "gemini", "model": "gemini-3-flash-preview"}).
			Input("Input", "meal_name").
			Output("Result", lane.name+"_advice")
	}

	// Stage 6 — coalesce the three lanes into a single advice wire.
	return b.
		Vertex("advice").Op("CoalesceNStringOp").
		Params(map[string]int{"n": 3}).
		Merge(config.MergeCoalesce).
		Input("Input0", "easy_advice").
		Input("Input1", "medium_advice").
		Input("Input2", "hard_advice").
		Output("Result", "advice").
		Build()
}

// ─── Driver ────────────────────────────────────────────────────────────────

// UserInput is the external boundary of the workflow. In CLI mode it is filled
// from --meal; in MCP mode the SDK derives the tool input schema from this
// struct and deserializes + validates every tools/call request into it. The
// MCP path always queries TheMealDB live — the --fixture offline mode is a
// CLI-only convenience resolved before the shared path.
type UserInput struct {
	Meal string `json:"meal" jsonschema:"the meal name to look up on TheMealDB and analyze for difficulty; required"`
}

// Result is the workflow's structured output: the MCP tool's typed Out (the
// SDK emits it as structured content + a JSON text block) and the CLI's stdout.
type Result struct {
	Meal            string   `json:"meal"`
	IngredientCount int      `json:"ingredient_count"`
	StepCount       int      `json:"step_count"`
	CookMinutes     float64  `json:"cook_minutes"`
	DifficultyScore float64  `json:"difficulty_score"`
	Difficulty      string   `json:"difficulty"`
	Advice          string   `json:"advice"`
	AINodes         []string `json:"ai_nodes"`
}

// runConfig is the resolved data source for one execution: either a live
// TheMealDB query URL or a pre-read fixture body.
type runConfig struct {
	mode    sourceMode
	body    string
	fullURL string
	meal    string
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

	ctx = context.WithValue(ctx, pathInstructionsKey{}, "meals.0.strInstructions")
	ctx = context.WithValue(ctx, pathMealnameKey{}, "meals.0.strMeal")
	ctx = context.WithValue(ctx, stepWeightKey{}, 1.0)
	ctx = context.WithValue(ctx, cookWeightKey{}, 0.1)
	if cfg.mode == sourceFixture {
		ctx = context.WithValue(ctx, bodyKey{}, cfg.body)
	} else {
		ctx = context.WithValue(ctx, urlKey{}, cfg.fullURL)
	}

	if err := eng.Run(ctx); err != nil {
		if errors.Is(err, library.ErrRequiredPathMissing) {
			if cfg.mode == sourceLive {
				return Result{}, fmt.Errorf("no results found for %q on TheMealDB", cfg.meal)
			}
			return Result{}, fmt.Errorf("fixture contains no results")
		}
		return Result{}, fmt.Errorf("run graph: %w", err)
	}

	// Live mode: surface a non-200 HTTP status as a hard failure rather than
	// silently feeding an error page into the AI extractors.
	if cfg.mode == sourceLive {
		if statusRaw, ok := eng.GetOutput("http_status"); ok {
			if status, ok := statusRaw.(*int); ok && status != nil && *status != 200 {
				return Result{}, fmt.Errorf("HTTP fetch returned status %d", *status)
			}
		}
	}

	// Pull each computed value out of the engine.
	out := Result{}

	if v, ok := getString(eng, "meal_name"); ok {
		out.Meal = v
	}
	if v, ok := getInt(eng, "ingredient_count_int"); ok {
		out.IngredientCount = v
	}
	if v, ok := getInt(eng, "step_count_int"); ok {
		out.StepCount = v
	}
	if v, ok := getFloat(eng, "cook_minutes"); ok {
		out.CookMinutes = v
	}
	if v, ok := getFloat(eng, "difficulty_score"); ok {
		out.DifficultyScore = v
	}
	if v, ok := getString(eng, "advice"); ok {
		out.Advice = v
	}

	// Determine which lane fired by inspecting the advice vertex skip state.
	for _, lane := range []string{"easy", "medium", "hard"} {
		if !eng.VertexSkipped(lane + "_advice") {
			out.Difficulty = lane
			break
		}
	}
	if out.Difficulty == "" {
		out.Difficulty = "unknown"
	}

	// Record which AI vertices actually fired (skipped lanes are pruned).
	candidates := []struct{ op, vertex string }{
		{"AIExtractStringSliceOp(ingredients)", "ingredients"},
		{"AIExtractStringSliceOp(steps)", "steps"},
		{"AIParseNumberOp(cook_minutes)", "cook_minutes"},
		{"AIComputeStringToStringOp(easy.advice)", "easy_advice"},
		{"AIComputeStringToStringOp(medium.advice)", "medium_advice"},
		{"AIComputeStringToStringOp(hard.advice)", "hard_advice"},
	}
	for _, c := range candidates {
		if !eng.VertexSkipped(c.vertex) {
			out.AINodes = append(out.AINodes, c.op)
		}
	}
	return out, nil
}

// runWorkflow is the live path shared by the MCP tool and CLI live mode: it
// builds the TheMealDB query URL for the meal, then runs the graph.
func runWorkflow(ctx context.Context, pool *ants.Pool, in UserInput) (Result, error) {
	cfg := runConfig{
		mode:    sourceLive,
		meal:    in.Meal,
		fullURL: "https://www.themealdb.com/api/json/v1/1/search.php?s=" + url.QueryEscape(in.Meal),
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
		Name:    "recipe-analyzer",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "analyze_recipe",
		Description: "Look up a meal on TheMealDB, extract ingredients/steps/cook time, compute a difficulty score, and return difficulty-tailored cooking advice.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in UserInput) (*mcp.CallToolResult, Result, error) {
		if strings.TrimSpace(in.Meal) == "" {
			return nil, Result{}, fmt.Errorf("meal is required")
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
		meal    = flag.String("meal", "", "meal name to query TheMealDB for, live API (CLI mode)")
		fixture = flag.String("fixture", "", "path to a captured TheMealDB JSON response, offline (CLI mode)")
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

	if *meal == "" && *fixture == "" {
		fmt.Fprintln(os.Stderr, "usage: 02-recipe-analyzer --meal <name>  |  --fixture <path>  |  --mcp")
		os.Exit(2)
	}
	if *meal != "" && *fixture != "" {
		fmt.Fprintln(os.Stderr, "specify exactly one of --meal or --fixture")
		os.Exit(2)
	}

	// CLI keeps the offline convenience: resolve the data source here
	// (live meal or --fixture) and feed the shared execution path.
	var cfg runConfig
	if *fixture != "" {
		raw, err := os.ReadFile(*fixture)
		if err != nil {
			log.Fatalf("read fixture: %v", err)
		}
		cfg = runConfig{mode: sourceFixture, body: string(raw)}
	} else {
		cfg = runConfig{
			mode:    sourceLive,
			meal:    *meal,
			fullURL: "https://www.themealdb.com/api/json/v1/1/search.php?s=" + url.QueryEscape(*meal),
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

func getInt(eng *dagor.Engine, name string) (int, bool) {
	raw, ok := eng.GetOutput(name)
	if !ok {
		return 0, false
	}
	p, ok := raw.(*int)
	if !ok || p == nil {
		return 0, false
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
