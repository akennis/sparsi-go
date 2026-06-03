// Package main is a weather-aware outfit advisor.
//
// Given a city name (live wttr.in API) or a captured fixture, it extracts
// temperature, precipitation, and wind; classifies conditions via AI; computes
// temperature band and boolean wet/windy flags deterministically; packs all
// signals into a single description; then calls an AI to recommend an outfit.
// An orthogonal AIBoolOp probe checks for unusual weather and appends a warning.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/akennis/sparsi-go/library"    // registers library ops
	_ "github.com/akennis/dagor/operator/builtin" // registers CoalesceNStringOp

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/panjf2000/ants/v2"
	"github.com/akennis/dagor"
	"github.com/akennis/dagor/reporter"
	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/graph"
	"github.com/akennis/dagor/operator"
	builtin "github.com/akennis/dagor/operator/builtin"
	"github.com/akennis/dagor/predicate"
)

// ─── Context keys ──────────────────────────────────────────────────────────

type (
	bodyKey        struct{} // fixture: raw wttr.in JSON
	urlKey         struct{} // live: wttr.in URL
	precipThreshKey struct{}
	windThreshKey  struct{}
	warningKey     struct{}
	emptyKey       struct{}
	tempPathKey    struct{}
	precipPathKey  struct{}
	windPathKey    struct{}
	descPathKey    struct{}
	coldBandKey    struct{}
	mildBandKey    struct{}
	hotBandKey     struct{}
)

// ─── Custom op ─────────────────────────────────────────────────────────────

// PackOutfitInputsOp formats weather signals into a single description string
// for the final AI outfit-recommendation call.
// Inputs: Band *string, Wet *bool, Windy *bool, TempC *float64, Conditions *[]string.
// Output: Result string.
type PackOutfitInputsOp struct {
	Band       *string
	Wet        *bool
	Windy      *bool
	TempC      *float64
	Conditions *[]string
	Result     string
}

func (op *PackOutfitInputsOp) Setup(_ *config.Params) error { return nil }
func (op *PackOutfitInputsOp) Reset() error                 { return nil }
func (op *PackOutfitInputsOp) Run(ctx context.Context) error {
	wet := "dry"
	if op.Wet != nil && *op.Wet {
		wet = "rainy/wet"
	}
	windy := "calm"
	if op.Windy != nil && *op.Windy {
		windy = "windy"
	}
	band := "unknown"
	if op.Band != nil {
		band = *op.Band
	}
	tempC := 0.0
	if op.TempC != nil {
		tempC = *op.TempC
	}
	conditions := "unspecified"
	if op.Conditions != nil && len(*op.Conditions) > 0 {
		conditions = strings.Join(*op.Conditions, ", ")
	}
	op.Result = fmt.Sprintf(
		"Temperature: %.1f°C (%s), precipitation: %s, wind: %s, conditions: %s",
		tempC, band, wet, windy, conditions,
	)
	slog.DebugContext(ctx, "PackOutfitInputsOp.done", "run_id", dagor.RunID(ctx), "result_len", len(op.Result))
	return nil
}

func (op *PackOutfitInputsOp) InputFields() map[string]any {
	return map[string]any{
		"Band":       &op.Band,
		"Wet":        &op.Wet,
		"Windy":      &op.Windy,
		"TempC":      &op.TempC,
		"Conditions": &op.Conditions,
	}
}

func (op *PackOutfitInputsOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}

func (op *PackOutfitInputsOp) SetInputField(field string, value any) error {
	switch field {
	case "Band":
		val, ok := value.(*string)
		if !ok {
			return fmt.Errorf("PackOutfitInputsOp: Band: expected *string, got %T", value)
		}
		op.Band = val
	case "Wet":
		val, ok := value.(*bool)
		if !ok {
			return fmt.Errorf("PackOutfitInputsOp: Wet: expected *bool, got %T", value)
		}
		op.Wet = val
	case "Windy":
		val, ok := value.(*bool)
		if !ok {
			return fmt.Errorf("PackOutfitInputsOp: Windy: expected *bool, got %T", value)
		}
		op.Windy = val
	case "TempC":
		val, ok := value.(*float64)
		if !ok {
			return fmt.Errorf("PackOutfitInputsOp: TempC: expected *float64, got %T", value)
		}
		op.TempC = val
	case "Conditions":
		val, ok := value.(*[]string)
		if !ok {
			return fmt.Errorf("PackOutfitInputsOp: Conditions: expected *[]string, got %T", value)
		}
		op.Conditions = val
	default:
		return fmt.Errorf("PackOutfitInputsOp: unknown field %q", field)
	}
	return nil
}

func (op *PackOutfitInputsOp) ResetFields() {
	op.Band = nil
	op.Wet = nil
	op.Windy = nil
	op.TempC = nil
	op.Conditions = nil
	op.Result = ""
}

func init() {
	mustReg := func(name string, f func() operator.IOperator) {
		if err := operator.RegisterOpFactory(name, f); err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}
	mustReg("body_const",      builtin.ContextValFactory[string](bodyKey{}))
	mustReg("url_const",       builtin.ContextValFactory[string](urlKey{}))
	mustReg("precip_thresh",   builtin.ContextValFactory[float64](precipThreshKey{}))
	mustReg("wind_thresh",     builtin.ContextValFactory[float64](windThreshKey{}))
	mustReg("warning_const",   builtin.ContextValFactory[string](warningKey{}))
	mustReg("empty_const",     builtin.ContextValFactory[string](emptyKey{}))
	mustReg("temp_str_path",   builtin.ContextValFactory[string](tempPathKey{}))
	mustReg("precip_str_path", builtin.ContextValFactory[string](precipPathKey{}))
	mustReg("wind_str_path",   builtin.ContextValFactory[string](windPathKey{}))
	mustReg("desc_str_path",   builtin.ContextValFactory[string](descPathKey{}))
	mustReg("cold_const",      builtin.ContextValFactory[string](coldBandKey{}))
	mustReg("mild_const",      builtin.ContextValFactory[string](mildBandKey{}))
	mustReg("hot_const",       builtin.ContextValFactory[string](hotBandKey{}))

	if err := operator.RegisterOp[PackOutfitInputsOp](); err != nil {
		log.Fatalf("register PackOutfitInputsOp: %v", err)
	}
}

// ─── Predicates ────────────────────────────────────────────────────────────

func registerPredicates() {
	must := func(name string, fn func(map[string]any) bool) {
		if err := predicate.Register(name, fn); err != nil {
			log.Fatalf("register predicate %s: %v", name, err)
		}
	}
	readF := func(in map[string]any, key string) (float64, bool) {
		v, ok := in[key].(*float64)
		return *v, ok && v != nil
	}
	must("temp_is_cold", func(in map[string]any) bool {
		v, ok := readF(in, "temp_c")
		return ok && v < 10.0
	})
	must("temp_is_mild", func(in map[string]any) bool {
		v, ok := readF(in, "temp_c")
		return ok && v >= 10.0 && v < 22.0
	})
	must("temp_is_hot", func(in map[string]any) bool {
		v, ok := readF(in, "temp_c")
		return ok && v >= 22.0
	})
}

// ─── Graph ─────────────────────────────────────────────────────────────────

type sourceMode int

const (
	sourceLive sourceMode = iota
	sourceFixture
)

func buildGraph(mode sourceMode) (*graph.Graph, error) {
	b := graph.NewBuilder("weather_advisor")

	// Stage 1 — produce a single `body` wire containing the wttr.in j1 JSON.
	switch mode {
	case sourceFixture:
		b.Vertex("body_const").Op("body_const").
			Output("Result", "body")
	case sourceLive:
		b.Vertex("url_const").Op("url_const").
			Output("Result", "url")
		b.Vertex("fetch").Op("HTTPGetOp").
			Input("URL", "url").
			Output("Body", "body").
			Output("StatusCode", "http_status")
	}

	// Stage 2 — extract the four numeric/text fields from JSON (run in parallel).
	for _, f := range []struct{ opName, wire string }{
		{"temp_str_path", "temp_str"},
		{"precip_str_path", "precip_str"},
		{"wind_str_path", "wind_str"},
		{"desc_str_path", "desc_str"},
	} {
		b.Vertex(f.opName).Op(f.opName).
			Output("Result", f.opName)
		b.Vertex("extract_"+f.wire).Op("JSONExtractOp").
			Input("JSON", "body").
			Input("Path", f.opName).
			Output("Value", f.wire)
	}

	// Stage 3 — AI parse numbers (three run concurrently).
	b.Vertex("parse_temp").Op("AIParseNumberOp").
		Params(map[string]string{"operation": "extract the temperature value as a plain number with no units"}).
		Input("Input", "temp_str").
		Output("Result", "temp_c")

	b.Vertex("parse_precip").Op("AIParseNumberOp").
		Params(map[string]string{"operation": "extract the precipitation amount as a plain number with no units"}).
		Input("Input", "precip_str").
		Output("Result", "precip_mm")

	b.Vertex("parse_wind").Op("AIParseNumberOp").
		Params(map[string]string{"operation": "extract the wind speed as a plain number with no units"}).
		Input("Input", "wind_str").
		Output("Result", "wind_kph")

	// Stage 4 — deterministic temperature band via three guarded ContextVal ops
	// and a CoalesceNStringOp merger (exactly one will run per execution).
	for _, band := range []struct{ name, cond string }{
		{"cold", "temp_is_cold"},
		{"mild", "temp_is_mild"},
		{"hot", "temp_is_hot"},
	} {
		b.Vertex(band.name+"_const").Op(band.name+"_const").
			Condition(band.cond).
			ConditionInput("temp_c").
			Output("Result", band.name+"_band")
	}
	b.Vertex("band_merge").Op("CoalesceNStringOp").
		Params(map[string]int{"n": 3}).
		Merge(config.MergeCoalesce).
		Input("Input0", "cold_band").
		Input("Input1", "mild_band").
		Input("Input2", "hot_band").
		Output("Result", "band")

	// Stage 5 — deterministic wet / windy boolean flags.
	b.Vertex("precip_thresh").Op("precip_thresh").
		Output("Result", "precip_thresh")
	b.Vertex("wet_check").Op("IfFloatGtOp").
		Input("A", "precip_mm").
		Input("B", "precip_thresh").
		Output("Match", "wet")

	b.Vertex("wind_thresh").Op("wind_thresh").
		Output("Result", "wind_thresh")
	b.Vertex("windy_check").Op("IfFloatGtOp").
		Input("A", "wind_kph").
		Input("B", "wind_thresh").
		Output("Match", "windy")

	// Stage 6 — multi-label weather condition classifier (AI).
	b.Vertex("classify_conditions").Op("AIClassifyMultiLabelOp").
		Params(map[string]string{"categories": "rain,snow,fog,sun,cloud,storm"}).
		Input("Input", "desc_str").
		Output("Result", "conditions")

	// Stage 7 — pack all signals into one description string for the final AI.
	b.Vertex("pack_outfit").Op("PackOutfitInputsOp").
		Input("Band", "band").
		Input("Wet", "wet").
		Input("Windy", "windy").
		Input("TempC", "temp_c").
		Input("Conditions", "conditions").
		Output("Result", "outfit_input")

	// Stage 8 — AI outfit advice.
	b.Vertex("outfit_advice_op").Op("AIComputeStringToStringOp").
		Params(map[string]string{
			"operation": "Given the weather conditions described, write exactly 2 sentences recommending an appropriate outfit. Be specific about clothing items.",
		}).
		Input("Input", "outfit_input").
		Output("Result", "outfit_advice")

	// Stage 9 — orthogonal unusual-weather probe + optional warning suffix.
	b.Vertex("unusual_check").Op("AIBoolOp").
		Params(map[string]string{"predicate": "Is the described weather unusual or extreme for typical human experience?"}).
		Input("Input", "desc_str").
		Output("Result", "unusual_flag")

	b.Vertex("warning_const").Op("warning_const").
		Output("Result", "warning_str")

	b.Vertex("empty_const").Op("empty_const").
		Output("Result", "empty_str")

	b.Vertex("warning_select").Op("SelectStringOp").
		Input("Cond", "unusual_flag").
		Input("IfTrue", "warning_str").
		Input("IfFalse", "empty_str").
		Output("Result", "warning_suffix")

	return b.Vertex("final_concat").Op("StringConcatOp").
		Input("A", "outfit_advice").
		Input("B", "warning_suffix").
		Output("Result", "final_advice").
		Build()
}

// ─── Output ────────────────────────────────────────────────────────────────

// UserInput is the external boundary of the workflow. In CLI mode it is filled
// from --city; in MCP mode the SDK derives the tool input schema from this
// struct and deserializes + validates every tools/call request into it. The
// MCP path always queries wttr.in live — the --fixture offline mode is a
// CLI-only convenience resolved before the shared path.
type UserInput struct {
	City string `json:"city" jsonschema:"the city name to fetch current weather for and advise an outfit; required"`
}

// Result is the workflow's structured output: the MCP tool's typed Out (the
// SDK emits it as structured content + a JSON text block) and the CLI's stdout.
type Result struct {
	City       string   `json:"city"`
	TempC      float64  `json:"temp_c"`
	PrecipMM   float64  `json:"precip_mm"`
	WindKph    float64  `json:"wind_kph"`
	Band       string   `json:"band"`
	Wet        bool     `json:"wet"`
	Windy      bool     `json:"windy"`
	Conditions []string `json:"conditions"`
	Advice     string   `json:"advice"`
	AINodes    []string `json:"ai_nodes"`
}

// runConfig is the resolved data source for one execution: either a live
// wttr.in query URL or a pre-read fixture body.
type runConfig struct {
	mode      sourceMode
	body      string
	fullURL   string
	cityLabel string
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

	ctx = context.WithValue(ctx, precipThreshKey{}, 0.1)
	ctx = context.WithValue(ctx, windThreshKey{}, 25.0)
	ctx = context.WithValue(ctx, warningKey{}, "  ⚠ unusual weather")
	ctx = context.WithValue(ctx, emptyKey{}, "")
	ctx = context.WithValue(ctx, tempPathKey{}, "current_condition.0.temp_C")
	ctx = context.WithValue(ctx, precipPathKey{}, "current_condition.0.precipMM")
	ctx = context.WithValue(ctx, windPathKey{}, "current_condition.0.windspeedKmph")
	ctx = context.WithValue(ctx, descPathKey{}, "current_condition.0.weatherDesc.0.value")
	ctx = context.WithValue(ctx, coldBandKey{}, "cold")
	ctx = context.WithValue(ctx, mildBandKey{}, "mild")
	ctx = context.WithValue(ctx, hotBandKey{}, "hot")
	if cfg.mode == sourceFixture {
		ctx = context.WithValue(ctx, bodyKey{}, cfg.body)
	} else {
		ctx = context.WithValue(ctx, urlKey{}, cfg.fullURL)
	}

	if err := eng.Run(ctx); err != nil {
		return Result{}, fmt.Errorf("run graph: %w", err)
	}

	if cfg.mode == sourceLive {
		if statusRaw, ok := eng.GetOutput("http_status"); ok {
			if status, ok2 := statusRaw.(*int); ok2 && status != nil && *status != 200 {
				return Result{}, fmt.Errorf("HTTP fetch returned status %d", *status)
			}
		}
	}

	out := Result{City: cfg.cityLabel}

	if v, ok := getFloat(eng, "temp_c"); ok {
		out.TempC = v
	}
	if v, ok := getFloat(eng, "precip_mm"); ok {
		out.PrecipMM = v
	}
	if v, ok := getFloat(eng, "wind_kph"); ok {
		out.WindKph = v
	}
	if v, ok := getString(eng, "band"); ok {
		out.Band = v
	}
	if v, ok := getBool(eng, "wet"); ok {
		out.Wet = v
	}
	if v, ok := getBool(eng, "windy"); ok {
		out.Windy = v
	}
	if v, ok := getStringSlice(eng, "conditions"); ok {
		out.Conditions = v
	}
	if v, ok := getString(eng, "final_advice"); ok {
		out.Advice = v
	}

	for _, c := range []struct{ op, vertex string }{
		{"AIParseNumberOp(temp)", "parse_temp"},
		{"AIParseNumberOp(precip)", "parse_precip"},
		{"AIParseNumberOp(wind)", "parse_wind"},
		{"AIClassifyMultiLabelOp(conditions)", "classify_conditions"},
		{"AIComputeStringToStringOp(outfit)", "outfit_advice_op"},
		{"AIBoolOp(unusual)", "unusual_check"},
	} {
		if !eng.VertexSkipped(c.vertex) {
			out.AINodes = append(out.AINodes, c.op)
		}
	}
	return out, nil
}

// runWorkflow is the live path shared by the MCP tool and CLI live mode: it
// builds the wttr.in query URL for the city, then runs the graph.
func runWorkflow(ctx context.Context, pool *ants.Pool, in UserInput) (Result, error) {
	cfg := runConfig{
		mode:      sourceLive,
		cityLabel: in.City,
		fullURL:   "https://wttr.in/" + url.PathEscape(in.City) + "?format=j1",
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
		Name:    "weather-advisor",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "advise_outfit",
		Description: "Fetch current weather for a city, derive temperature band and wet/windy flags, classify conditions, and return an AI outfit recommendation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in UserInput) (*mcp.CallToolResult, Result, error) {
		if strings.TrimSpace(in.City) == "" {
			return nil, Result{}, fmt.Errorf("city is required")
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
		city    = flag.String("city", "", "city name for live wttr.in API (CLI mode)")
		fixture = flag.String("fixture", "", "path to captured wttr.in j1 JSON fixture (CLI mode)")
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

	if *city == "" && *fixture == "" {
		fmt.Fprintln(os.Stderr, "usage: 04-weather-advisor --city <name>  |  --fixture <path>  |  --mcp")
		os.Exit(2)
	}
	if *city != "" && *fixture != "" {
		fmt.Fprintln(os.Stderr, "specify exactly one of --city or --fixture")
		os.Exit(2)
	}

	// CLI keeps the offline convenience: resolve the data source here
	// (live city or --fixture) and feed the shared execution path.
	var cfg runConfig
	if *fixture != "" {
		raw, err := os.ReadFile(*fixture)
		if err != nil {
			log.Fatalf("read fixture: %v", err)
		}
		// derive city label from filename
		base := *fixture
		if idx := strings.LastIndexAny(base, "/\\"); idx >= 0 {
			base = base[idx+1:]
		}
		cfg = runConfig{
			mode:      sourceFixture,
			body:      string(raw),
			cityLabel: strings.TrimSuffix(base, ".json"),
		}
	} else {
		cfg = runConfig{
			mode:      sourceLive,
			cityLabel: *city,
			fullURL:   "https://wttr.in/" + url.PathEscape(*city) + "?format=j1",
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

func getStringSlice(eng *dagor.Engine, name string) ([]string, bool) {
	raw, ok := eng.GetOutput(name)
	if !ok {
		return nil, false
	}
	p, ok := raw.(*[]string)
	if !ok || p == nil {
		return nil, false
	}
	return *p, true
}
