// Package main is a HackerNews topic brief generator.
//
// Given a search query, it fetches the top stories from the HN Algolia API,
// fans out per-story AI checks (relevance filter + multi-label classifier) over
// a MapOver node, computes the dominant category, selects a brief style via
// ModeSelectOp, and produces a structured brief in one of three styles
// (technical, business, policy).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/akennis/sparsi-go/library"    // registers library ops
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

type responseJSONKey struct{}

// ─── Custom ops ────────────────────────────────────────────────────────────

// ExtractTitlesOp parses a HN Algolia API JSON response and returns the hit
// titles as a string slice.
type ExtractTitlesOp struct {
	JSON   *string
	Result []string
}

func (op *ExtractTitlesOp) Setup(_ *config.Params) error { return nil }
func (op *ExtractTitlesOp) Reset() error                 { return nil }
func (op *ExtractTitlesOp) Run(ctx context.Context) error {
	var resp struct {
		Hits []struct {
			Title string `json:"title"`
		} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(*op.JSON), &resp); err != nil {
		return fmt.Errorf("ExtractTitlesOp: parse JSON: %w", err)
	}
	op.Result = make([]string, 0, len(resp.Hits))
	for _, h := range resp.Hits {
		if h.Title != "" {
			op.Result = append(op.Result, h.Title)
		}
	}
	slog.DebugContext(ctx, "ExtractTitlesOp.done", "run_id", dagor.RunID(ctx), "title_count", len(op.Result))
	return nil
}
func (op *ExtractTitlesOp) InputFields() map[string]any  { return map[string]any{"JSON": &op.JSON} }
func (op *ExtractTitlesOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *ExtractTitlesOp) SetInputField(field string, value any) error {
	if field != "JSON" {
		return fmt.Errorf("ExtractTitlesOp: unknown field %q", field)
	}
	v, ok := value.(*string)
	if !ok {
		return fmt.Errorf("ExtractTitlesOp: JSON: expected *string, got %T", value)
	}
	op.JSON = v
	return nil
}
func (op *ExtractTitlesOp) ResetFields() { op.JSON = nil; op.Result = nil }

// FilterAndFlattenOp filters story titles by per-item relevance flags and
// flattens all per-item label lists into a single label list.
//
// RelevantFlags is *[]any where each element is a bool.
// LabelLists is *[]any where each element is a []string.
type FilterAndFlattenOp struct {
	Titles        *[]string
	RelevantFlags *[]any
	LabelLists    *[]any
	KeptTitles    []string
	AllLabels     []string
}

func (op *FilterAndFlattenOp) Setup(_ *config.Params) error { return nil }
func (op *FilterAndFlattenOp) Reset() error                 { return nil }
func (op *FilterAndFlattenOp) Run(ctx context.Context) error {
	titles := *op.Titles
	flags := *op.RelevantFlags
	lists := *op.LabelLists

	n := len(titles)
	if len(flags) < n {
		n = len(flags)
	}
	if len(lists) < n {
		n = len(lists)
	}

	for i := 0; i < n; i++ {
		flag, ok := flags[i].(bool)
		if !ok || !flag {
			continue
		}
		op.KeptTitles = append(op.KeptTitles, titles[i])
		if labels, ok := lists[i].([]string); ok {
			op.AllLabels = append(op.AllLabels, labels...)
		}
	}
	slog.DebugContext(ctx, "FilterAndFlattenOp.done", "run_id", dagor.RunID(ctx), "kept", len(op.KeptTitles), "total", len(*op.Titles), "labels", len(op.AllLabels))
	return nil
}
func (op *FilterAndFlattenOp) InputFields() map[string]any {
	return map[string]any{
		"Titles":        &op.Titles,
		"RelevantFlags": &op.RelevantFlags,
		"LabelLists":    &op.LabelLists,
	}
}
func (op *FilterAndFlattenOp) OutputFields() map[string]any {
	return map[string]any{
		"KeptTitles": &op.KeptTitles,
		"AllLabels":  &op.AllLabels,
	}
}
func (op *FilterAndFlattenOp) SetInputField(field string, value any) error {
	switch field {
	case "Titles":
		v, ok := value.(*[]string)
		if !ok {
			return fmt.Errorf("FilterAndFlattenOp: Titles: expected *[]string, got %T", value)
		}
		op.Titles = v
	case "RelevantFlags":
		v, ok := value.(*[]any)
		if !ok {
			return fmt.Errorf("FilterAndFlattenOp: RelevantFlags: expected *[]any, got %T", value)
		}
		op.RelevantFlags = v
	case "LabelLists":
		v, ok := value.(*[]any)
		if !ok {
			return fmt.Errorf("FilterAndFlattenOp: LabelLists: expected *[]any, got %T", value)
		}
		op.LabelLists = v
	default:
		return fmt.Errorf("FilterAndFlattenOp: unknown field %q", field)
	}
	return nil
}
func (op *FilterAndFlattenOp) ResetFields() {
	op.Titles = nil
	op.RelevantFlags = nil
	op.LabelLists = nil
	op.KeptTitles = nil
	op.AllLabels = nil
}

// DominantCategoryOp returns the most frequent label in AllLabels; ties are
// broken alphabetically. Falls back to "technical" when the list is empty.
type DominantCategoryOp struct {
	AllLabels *[]string
	Dominant  string
}

func (op *DominantCategoryOp) Setup(_ *config.Params) error { return nil }
func (op *DominantCategoryOp) Reset() error                 { return nil }
func (op *DominantCategoryOp) Run(ctx context.Context) error {
	counts := make(map[string]int)
	for _, label := range *op.AllLabels {
		label = strings.TrimSpace(label)
		if label != "" {
			counts[label]++
		}
	}

	best := "technical"
	bestCount := 0
	for label, count := range counts {
		if count > bestCount || (count == bestCount && label < best) {
			best = label
			bestCount = count
		}
	}
	op.Dominant = best
	slog.DebugContext(ctx, "DominantCategoryOp.done", "run_id", dagor.RunID(ctx), "dominant", op.Dominant, "count", bestCount)
	return nil
}
func (op *DominantCategoryOp) InputFields() map[string]any {
	return map[string]any{"AllLabels": &op.AllLabels}
}
func (op *DominantCategoryOp) OutputFields() map[string]any {
	return map[string]any{"Dominant": &op.Dominant}
}
func (op *DominantCategoryOp) SetInputField(field string, value any) error {
	if field != "AllLabels" {
		return fmt.Errorf("DominantCategoryOp: unknown field %q", field)
	}
	v, ok := value.(*[]string)
	if !ok {
		return fmt.Errorf("DominantCategoryOp: AllLabels: expected *[]string, got %T", value)
	}
	op.AllLabels = v
	return nil
}
func (op *DominantCategoryOp) ResetFields() { op.AllLabels = nil; op.Dominant = "" }

func init() {
	mustReg := func(name string, f func() operator.IOperator) {
		if err := operator.RegisterOpFactory(name, f); err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}
	mustReg("response_const", builtin.ContextValFactory[string](responseJSONKey{}))

	for _, reg := range []func() error{
		operator.RegisterOp[ExtractTitlesOp],
		operator.RegisterOp[FilterAndFlattenOp],
		operator.RegisterOp[DominantCategoryOp],
	} {
		if err := reg(); err != nil {
			log.Fatalf("register custom op: %v", err)
		}
	}
}

// ─── Predicates ────────────────────────────────────────────────────────────

func registerPredicates() {
	for _, style := range []string{"technical_brief", "business_brief", "policy_brief"} {
		want := style
		name := "style_is_" + style
		if err := predicate.Register(name, func(inputs map[string]any) bool {
			v, ok := inputs["brief_style"].(*string)
			return ok && v != nil && *v == want
		}); err != nil {
			log.Fatalf("register predicate %s: %v", name, err)
		}
	}
}

// ─── Graph ─────────────────────────────────────────────────────────────────

// buildGraph constructs the DAG. query is embedded in AI op params since it
// shapes the graph definition (not a per-execution runtime value).
func buildGraph(query string) (*graph.Graph, error) {
	b := graph.NewBuilder("hn_topic_brief")

	// ── Stage 1: inject API response and extract titles ───────────────────────
	b.
		Vertex("response_const").Op("response_const").
		Output("Result", "response_json").

		Vertex("extract_titles").Op("ExtractTitlesOp").
		Input("JSON", "response_json").
		Output("Result", "titles")

	// ── Stage 2a: per-story relevance check (MapOver) ─────────────────────────
	// Each element of `titles` (a string) is checked by AIBoolOp.
	// CollectInto gathers the bool results into `relevant_flags` (*[]any).
	b.Vertex("map_relevance").
		Input("Items", "titles").
		MapOver("title").
		SubVertex("relevant").
		Op("AIBoolOp").
		Params(map[string]string{
			"predicate": fmt.Sprintf(
				"Is this HackerNews story title actually about the topic %q? Respond true or false.", query),
		}).
		Input("Input", "title").
		Output("Result", "is_relevant").
		CollectInto("is_relevant", "relevant_flags")

	// ── Stage 2b: per-story label classification (MapOver) ────────────────────
	// Each element of `titles` (a string) is classified into zero or more labels.
	// CollectInto gathers the []string results into `label_lists` (*[]any).
	b.Vertex("map_classify").
		Input("Items", "titles").
		MapOver("title").
		SubVertex("classify").
		Op("AIClassifyMultiLabelOp").
		Params(map[string]string{
			"categories": "technical,business,policy,human_interest,other",
		}).
		Input("Input", "title").
		Output("Result", "labels").
		CollectInto("labels", "label_lists")

	// ── Stage 3: filter + flatten ─────────────────────────────────────────────
	b.
		Vertex("filter_flatten").Op("FilterAndFlattenOp").
		Input("Titles", "titles").
		Input("RelevantFlags", "relevant_flags").
		Input("LabelLists", "label_lists").
		Output("KeptTitles", "kept_titles").
		Output("AllLabels", "all_labels").

		// ── Stage 4: dominant category ─────────────────────────────────────────
		Vertex("dominant_cat").Op("DominantCategoryOp").
		Input("AllLabels", "all_labels").
		Output("Dominant", "dominant").

		// ── Stage 5: AI style selector ─────────────────────────────────────────
		// ModeSelectOp classifies the dominant label into one of three brief styles.
		Vertex("mode_select").Op("ModeSelectOp").
		Params(map[string]string{"categories": "technical_brief,business_brief,policy_brief"}).
		Input("Input", "dominant").
		Output("Result", "brief_style")

	// ── Stage 6: three brief-style lanes (exactly one fires) ──────────────────
	lanes := []struct {
		name      string
		condition string
		operation string
	}{
		{
			name:      "technical",
			condition: "style_is_technical_brief",
			operation: "summarize the following HackerNews story titles as a technical engineering newsletter: " +
				"write one concise bullet point per story and group related stories by sub-topic",
		},
		{
			name:      "business",
			condition: "style_is_business_brief",
			operation: "summarize the following HackerNews story titles as an executive business brief: " +
				"write a 3-sentence overview then a bulleted impact list",
		},
		{
			name:      "policy",
			condition: "style_is_policy_brief",
			operation: "summarize the following HackerNews story titles as a policy memo: " +
				"list legislative items, affected parties, and likely timeline",
		},
	}
	for _, lane := range lanes {
		b.
			Vertex(lane.name + "_lane").Op("AISummarizeOp").
			Condition(lane.condition).
			ConditionInput("brief_style").
			Params(map[string]string{"operation": lane.operation}).
			Input("Input", "kept_titles").
			Output("Result", lane.name+"_text")
	}

	// ── Stage 7: coalesce the one lane that fired ─────────────────────────────
	b.
		Vertex("final_op").Op("CoalesceNStringOp").
		Params(map[string]int{"n": 3}).
		Merge(config.MergeCoalesce).
		Input("Input0", "technical_text").
		Input("Input1", "business_text").
		Input("Input2", "policy_text").
		Output("Result", "final_brief")

	return b.Build()
}

// ─── Driver ────────────────────────────────────────────────────────────────

type outputResult struct {
	Query             string         `json:"query"`
	StoryCount        int            `json:"story_count"`
	KeptAfterFilter   int            `json:"kept_after_filter"`
	LabelDistribution map[string]int `json:"label_distribution"`
	Dominant          string         `json:"dominant"`
	BriefStyle        string         `json:"brief_style"`
	Brief             string         `json:"brief"`
	AINodes           []string       `json:"ai_nodes"`
}

func main() {
	var (
		query   = flag.String("query", "", "HackerNews search query")
		cache   = flag.Bool("cache", false, "load fixture from testdata/hn/<query-slug>.json")
		fixture = flag.String("fixture", "", "path to a pre-captured HN API response JSON file")
	)
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	if *query == "" {
		fmt.Fprintln(os.Stderr, "usage: 05-hn-topic-brief --query <term> [--cache | --fixture <path>]")
		os.Exit(2)
	}

	responseJSON, err := fetchOrLoad(*query, *cache, *fixture)
	if err != nil {
		log.Fatalf("get HN data: %v", err)
	}

	registerPredicates()

	g, err := buildGraph(*query)
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	ctx = context.WithValue(ctx, responseJSONKey{}, responseJSON)

	if err := eng.Run(ctx); err != nil {
		log.Fatalf("run graph: %v", err)
	}

	out := outputResult{Query: *query}

	// total story count (from titles wire)
	if raw, ok := eng.GetOutput("titles"); ok {
		if p, ok := raw.(*[]string); ok && p != nil {
			out.StoryCount = len(*p)
		}
	}

	// kept titles
	var keptTitles []string
	if raw, ok := eng.GetOutput("kept_titles"); ok {
		if p, ok := raw.(*[]string); ok && p != nil {
			keptTitles = *p
		}
	}
	out.KeptAfterFilter = len(keptTitles)

	// label distribution from all_labels wire
	if raw, ok := eng.GetOutput("all_labels"); ok {
		if p, ok := raw.(*[]string); ok && p != nil {
			dist := make(map[string]int)
			for _, label := range *p {
				if label != "" {
					dist[label]++
				}
			}
			out.LabelDistribution = dist
		}
	}

	if v, ok := getString(eng, "dominant"); ok {
		out.Dominant = v
	}
	if v, ok := getString(eng, "brief_style"); ok {
		out.BriefStyle = v
	}
	if v, ok := getString(eng, "final_brief"); ok {
		out.Brief = v
	}

	// which AI vertices actually fired
	candidates := []struct{ label, vertex string }{
		{"ExtractTitlesOp", "extract_titles"},
		{"AIBoolOp(relevance/map)", "map_relevance"},
		{"AIClassifyMultiLabelOp(classify/map)", "map_classify"},
		{"FilterAndFlattenOp", "filter_flatten"},
		{"DominantCategoryOp", "dominant_cat"},
		{"ModeSelectOp", "mode_select"},
		{"AISummarizeOp(technical)", "technical_lane"},
		{"AISummarizeOp(business)", "business_lane"},
		{"AISummarizeOp(policy)", "policy_lane"},
	}
	for _, c := range candidates {
		if !eng.VertexSkipped(c.vertex) {
			out.AINodes = append(out.AINodes, c.label)
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		log.Fatalf("encode output: %v", err)
	}
}

// fetchOrLoad returns the raw HN API JSON for the query, either from a live
// API call, a named fixture file, or the auto-derived testdata path.
func fetchOrLoad(query string, useCache bool, fixturePath string) (string, error) {
	if fixturePath != "" {
		data, err := os.ReadFile(fixturePath)
		if err != nil {
			return "", fmt.Errorf("read fixture %s: %w", fixturePath, err)
		}
		return string(data), nil
	}

	if useCache {
		slug := queryToSlug(query)
		path := filepath.Join("testdata", "hn", slug+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read cache %s: %w (run without --cache to fetch live)", path, err)
		}
		slog.Info("fetchOrLoad.fixture", "path", path, "bytes", len(data))
		return string(data), nil
	}

	// Live fetch
	endpoint := "https://hn.algolia.com/api/v1/search?query=" +
		url.QueryEscape(query) + "&hitsPerPage=10"
	slog.Info("fetchOrLoad.live", "endpoint", endpoint)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http status %d", resp.StatusCode)
	}
	return string(data), nil
}

// queryToSlug converts a search query to a filename-safe slug.
func queryToSlug(query string) string {
	slug := strings.ToLower(query)
	slug = strings.ReplaceAll(slug, " ", "-")
	// keep only alphanumeric, hyphen, underscore
	var b strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
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
