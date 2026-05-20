// Package main demonstrates mixing Claude and Gemini in a single dagor workflow
// to implement a summarization faithfulness check.
//
// Claude produces a 3–5 sentence summary of a source document; a deterministic
// formatting op assembles the source and summary into a single verification
// prompt; Gemini then checks whether every factual claim in the summary is
// grounded in the source text, returning a boolean verdict.
//
// This cross-model pattern is motivated by the fact that a model which
// generated a summary has already committed to its framing — a second,
// independent model is more likely to surface unsupported claims.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	_ "github.com/akennis/sparsi-go/library"

	"github.com/panjf2000/ants/v2"
	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/graph"
	"github.com/wwz16/dagor/operator"
	builtin "github.com/wwz16/dagor/operator/builtin"
	"github.com/wwz16/dagor/reporter"
)

// ─── Context keys ──────────────────────────────────────────────────────────

type sourceKey struct{}

// ─── Custom ops ────────────────────────────────────────────────────────────

// FormatFaithfulnessCheckOp combines the source document and the
// Claude-generated summary into a single string for AIBoolOp to evaluate.
type FormatFaithfulnessCheckOp struct {
	Source  *string `dag:"input"`
	Summary *string `dag:"input"`
	Query   string  `dag:"output"`
}

func (op *FormatFaithfulnessCheckOp) Setup(_ *config.Params) error { return nil }
func (op *FormatFaithfulnessCheckOp) Reset() error                 { return nil }
func (op *FormatFaithfulnessCheckOp) Run(_ context.Context) error {
	op.Query = fmt.Sprintf("Source document:\n%s\n\nSummary to verify:\n%s", *op.Source, *op.Summary)
	return nil
}
func (op *FormatFaithfulnessCheckOp) InputFields() map[string]any {
	return map[string]any{"Source": &op.Source, "Summary": &op.Summary}
}
func (op *FormatFaithfulnessCheckOp) OutputFields() map[string]any {
	return map[string]any{"Query": &op.Query}
}
func (op *FormatFaithfulnessCheckOp) SetInputField(field string, value any) error {
	switch field {
	case "Source":
		v, ok := value.(*string)
		if !ok {
			return fmt.Errorf("FormatFaithfulnessCheckOp: Source: expected *string, got %T", value)
		}
		op.Source = v
	case "Summary":
		v, ok := value.(*string)
		if !ok {
			return fmt.Errorf("FormatFaithfulnessCheckOp: Summary: expected *string, got %T", value)
		}
		op.Summary = v
	default:
		return fmt.Errorf("FormatFaithfulnessCheckOp: unknown field %q", field)
	}
	return nil
}
func (op *FormatFaithfulnessCheckOp) ResetFields() {
	op.Source = nil
	op.Summary = nil
	op.Query = ""
}

func init() {
	mustReg := func(name string, f func() operator.IOperator) {
		if err := operator.RegisterOpFactory(name, f); err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}
	mustReg("source_const", builtin.ContextValFactory[string](sourceKey{}))

	if err := operator.RegisterOp[FormatFaithfulnessCheckOp](); err != nil {
		log.Fatalf("register FormatFaithfulnessCheckOp: %v", err)
	}
}

// ─── Graph ─────────────────────────────────────────────────────────────────

func buildGraph() (*graph.Graph, error) {
	return graph.NewBuilder("faithful_summary").
		Vertex("source_const").Op("source_const").
		Output("Result", "source").
		Vertex("summarize").Op("AIComputeStringToStringOp").
		Params(map[string]string{
			"operation": "summarize this article in 3–5 concise sentences; include only information explicitly stated in the text, do not add context or draw inferences",
			"provider":  "claude",
			"model":     "claude-sonnet-4-6",
		}).
		Input("Input", "source").
		Output("Result", "summary").
		Vertex("format_check").Op("FormatFaithfulnessCheckOp").
		Input("Source", "source").
		Input("Summary", "summary").
		Output("Query", "query").
		Vertex("verify").Op("AIBoolOp").
		Params(map[string]string{
			"predicate": "does every factual claim in the summary appear in or follow directly from the source document, with no information added or invented?",
			"provider":  "gemini",
			"model":     "gemini-3-flash-preview",
		}).
		Input("Input", "query").
		Output("Result", "faithful").
		Build()
}

// ─── Driver ────────────────────────────────────────────────────────────────

type result struct {
	SourceLength int    `json:"source_length"`
	Summary      string `json:"summary"`
	Faithful     bool   `json:"faithful"`
}

func main() {
	file := flag.String("file", "", "path to a text file to summarize")
	text := flag.String("text", "", "inline source text to summarize")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	if (*file == "") == (*text == "") {
		fmt.Fprintln(os.Stderr, "usage: 06-faithful-summary --file <path>  |  --text <text>")
		os.Exit(2)
	}

	var source string
	if *file != "" {
		raw, err := os.ReadFile(*file)
		if err != nil {
			log.Fatalf("read file: %v", err)
		}
		source = string(raw)
	} else {
		source = *text
	}

	g, err := buildGraph()
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx = context.WithValue(ctx, sourceKey{}, source)

	if err := eng.Run(ctx); err != nil {
		log.Fatalf("run graph: %v", err)
	}

	out := result{SourceLength: len(source)}

	if raw, ok := eng.GetOutput("summary"); ok {
		if p, ok := raw.(*string); ok && p != nil {
			out.Summary = *p
		}
	}
	if raw, ok := eng.GetOutput("faithful"); ok {
		if p, ok := raw.(*bool); ok && p != nil {
			out.Faithful = *p
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		log.Fatalf("encode output: %v", err)
	}
}
