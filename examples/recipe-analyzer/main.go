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
	"time"

	"github.com/akennis/sparsi-go/library"
	_ "github.com/wwz16/dagor/operator/builtin" // registers CoalesceNStringOp

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

type result struct {
	Meal            string   `json:"meal"`
	IngredientCount int      `json:"ingredient_count"`
	StepCount       int      `json:"step_count"`
	CookMinutes     float64  `json:"cook_minutes"`
	DifficultyScore float64  `json:"difficulty_score"`
	Difficulty      string   `json:"difficulty"`
	Advice          string   `json:"advice"`
	AINodes         []string `json:"ai_nodes"`
}

func main() {
	var (
		meal    = flag.String("meal", "", "meal name to query TheMealDB for (live API)")
		fixture = flag.String("fixture", "", "path to a captured TheMealDB JSON response (offline)")
	)
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	if *meal == "" && *fixture == "" {
		fmt.Fprintln(os.Stderr, "usage: 02-recipe-analyzer --meal <name>  |  --fixture <path>")
		os.Exit(2)
	}
	if *meal != "" && *fixture != "" {
		fmt.Fprintln(os.Stderr, "specify exactly one of --meal or --fixture")
		os.Exit(2)
	}

	var mode sourceMode
	var body, fullURL string

	if *fixture != "" {
		raw, err := os.ReadFile(*fixture)
		if err != nil {
			log.Fatalf("read fixture: %v", err)
		}
		mode = sourceFixture
		body = string(raw)
	} else {
		mode = sourceLive
		fullURL = "https://www.themealdb.com/api/json/v1/1/search.php?s=" + url.QueryEscape(*meal)
	}

	registerPredicates()

	g, err := buildGraph(mode)
	if err != nil {
		log.Fatalf("build graph: %v", err)
	}

	pool, err := ants.NewPool(10)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	defer pool.Release()

	eng, err := dagor.NewEngine(g, pool, dagor.WithReporter(reporter.New(slog.Default())))
	if err != nil {
		log.Fatalf("create engine: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ctx = context.WithValue(ctx, pathInstructionsKey{}, "meals.0.strInstructions")
	ctx = context.WithValue(ctx, pathMealnameKey{}, "meals.0.strMeal")
	ctx = context.WithValue(ctx, stepWeightKey{}, 1.0)
	ctx = context.WithValue(ctx, cookWeightKey{}, 0.1)
	if mode == sourceFixture {
		ctx = context.WithValue(ctx, bodyKey{}, body)
	} else {
		ctx = context.WithValue(ctx, urlKey{}, fullURL)
	}

	if err := eng.Run(ctx); err != nil {
		if errors.Is(err, library.ErrRequiredPathMissing) {
			if mode == sourceLive {
				log.Fatalf("no results found for %q on TheMealDB", *meal)
			}
			log.Fatalf("fixture contains no results")
		}
		log.Fatalf("run graph: %v", err)
	}

	// Live mode: surface a non-200 HTTP status as a hard failure rather than
	// silently feeding an error page into the AI extractors.
	if mode == sourceLive {
		if statusRaw, ok := eng.GetOutput("http_status"); ok {
			if status, ok := statusRaw.(*int); ok && status != nil && *status != 200 {
				log.Fatalf("HTTP fetch returned status %d", *status)
			}
		}
	}

	// Pull each computed value out of the engine.
	out := result{}

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

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
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
