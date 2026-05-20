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
//
// The program is dual-mode: a one-shot CLI tool by default, or a local
// stdin/stdout MCP server when invoked with -mcp (exposing the workflow as a
// single MCP tool whose input is deserialized out of each tools/call request).
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

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// ─── Workflow inputs / outputs ─────────────────────────────────────────────

// UserInput is the external boundary of the workflow. In CLI mode it is filled
// from --file/--text; in MCP mode the SDK derives the tool input schema from
// this struct and deserializes + validates every tools/call request into it.
type UserInput struct {
	Text string `json:"text" jsonschema:"the full source document text to summarize and fact-check; required"`
}

// Result is the workflow's structured output: the MCP tool's typed Out (the
// SDK emits it as structured content + a JSON text block) and the CLI's stdout.
type Result struct {
	SourceLength int    `json:"source_length"`
	Summary      string `json:"summary"`
	Faithful     bool   `json:"faithful"`
}

// ─── Shared execution path ─────────────────────────────────────────────────

// runWorkflow is the single path shared by CLI and MCP modes. It builds a
// fresh graph + engine per invocation so concurrent MCP tool calls never share
// mutable operator state; the ants.Pool is safe to share. The input is
// injected via context.WithValue (NOT eng.SetInput) exactly as in CLI mode.
func runWorkflow(ctx context.Context, pool *ants.Pool, in UserInput) (Result, error) {
	g, err := buildGraph()
	if err != nil {
		return Result{}, fmt.Errorf("build graph: %w", err)
	}

	eng, err := dagor.NewEngine(g, pool, dagor.WithReporter(reporter.New(slog.Default())))
	if err != nil {
		return Result{}, fmt.Errorf("create engine: %w", err)
	}

	ctx = context.WithValue(ctx, sourceKey{}, in.Text)

	if err := eng.Run(ctx); err != nil {
		return Result{}, fmt.Errorf("run graph: %w", err)
	}

	out := Result{SourceLength: len(in.Text)}
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
	return out, nil
}

// ─── MCP server mode ───────────────────────────────────────────────────────

// runMCPServer exposes the workflow as one MCP tool over stdin/stdout. The SDK
// infers the tool input schema from UserInput, auto-deserializes + validates
// each request, and (because Out is non-any) emits Result as structured
// content plus a JSON text block, so the handler returns nil for the result.
func runMCPServer(pool *ants.Pool) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "faithful-summary",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "summarize_and_verify",
		Description: "Summarize a source document with Claude, then independently fact-check that summary with Gemini. Returns the generated summary and whether every claim in it is grounded in the source.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in UserInput) (*mcp.CallToolResult, Result, error) {
		// in is already deserialized + schema-validated from the request.
		if in.Text == "" {
			return nil, Result{}, fmt.Errorf("text is required")
		}
		res, err := runWorkflow(ctx, pool, in)
		if err != nil {
			return nil, Result{}, err // surfaced to the client as an error result
		}
		return nil, res, nil
	})

	// StdioTransport: newline-delimited JSON over this process's stdin/stdout.
	// server.Run blocks until the client disconnects or ctx is cancelled.
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("mcp server: %v", err)
	}
}

// ─── Entrypoint ────────────────────────────────────────────────────────────

func main() {
	mcpMode := flag.Bool("mcp", false, "run as a stdio MCP server instead of a one-shot CLI")
	file := flag.String("file", "", "path to a text file to summarize (CLI mode)")
	text := flag.String("text", "", "inline source text to summarize (CLI mode)")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	pool, err := ants.NewPool(10)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	defer pool.Release()

	if *mcpMode {
		runMCPServer(pool)
		return
	}

	if (*file == "") == (*text == "") {
		fmt.Fprintln(os.Stderr, "usage: 06-faithful-summary --file <path> | --text <text> | --mcp")
		os.Exit(2)
	}

	source := *text
	if *file != "" {
		raw, err := os.ReadFile(*file)
		if err != nil {
			log.Fatalf("read file: %v", err)
		}
		source = string(raw)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	res, err := runWorkflow(ctx, pool, UserInput{Text: source})
	if err != nil {
		log.Fatalf("workflow: %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		log.Fatalf("encode output: %v", err)
	}
}
