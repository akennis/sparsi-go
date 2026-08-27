// Package main is a human-in-the-loop publish-approval workflow.
//
// Given a draft message, it pauses to ask a real person three questions —
// whether to proceed (bool), which delivery option to use (a choice that
// yields an index), and an optional accompanying note (free text) — then feeds
// all three human answers into a deterministic DecisionOp that composes the
// final action summary. It demonstrates that a human's mid-DAG input flows
// through the rest of the workflow exactly like any other producer's output.
//
// The same binary runs two ways:
//
//   - CLI:  go run . --draft "Ship v2 to all users"
//     A StdinPrompter asks the questions on your terminal.
//   - MCP:  go run . --mcp
//     An ElicitPrompter forwards each question to the connected MCP client as
//     an `elicitation/create` request; the human answers in their client UI.
//     (The client must support the elicitation capability.)
//
// No API key is required — every op here is deterministic.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	_ "github.com/akennis/sparsi-go/library" // registers library ops (incl. Human* ops)

	"github.com/akennis/dagor"
	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/graph"
	"github.com/akennis/dagor/operator"
	builtin "github.com/akennis/dagor/operator/builtin"
	"github.com/akennis/dagor/reporter"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/panjf2000/ants/v2"

	"github.com/akennis/sparsi-go/library"
)

// deliveryOptions are the choices offered by the HumanChoiceInputOp; its
// zero-based Result indexes into this slice. Keep in sync with the "options"
// param on the action_choice vertex below.
var deliveryOptions = []string{"send now", "schedule for later", "save as draft"}

// ─── Context keys ──────────────────────────────────────────────────────────

type draftKey struct{}

// ─── Decision op ───────────────────────────────────────────────────────────

// DecisionOp turns the three human answers into a final action summary,
// demonstrating that human input propagates downstream like any other wire.
// Inputs: Approved *bool, ActionIdx *int, Note *string, Draft *string.
// Output: Result string.
type DecisionOp struct {
	Approved  *bool
	ActionIdx *int
	Note      *string
	Draft     *string
	Result    string
}

func (op *DecisionOp) Setup(_ *config.Params) error { return nil }
func (op *DecisionOp) Reset() error                 { return nil }

func (op *DecisionOp) Run(ctx context.Context) error {
	if op.Approved == nil || !*op.Approved {
		op.Result = "Cancelled — the human did not approve the draft."
		slog.DebugContext(ctx, "DecisionOp.done", "run_id", dagor.RunID(ctx), "approved", false)
		return nil
	}
	action := "unknown"
	if op.ActionIdx != nil && *op.ActionIdx >= 0 && *op.ActionIdx < len(deliveryOptions) {
		action = deliveryOptions[*op.ActionIdx]
	}
	note := ""
	if op.Note != nil && strings.TrimSpace(*op.Note) != "" {
		note = " Note: " + strings.TrimSpace(*op.Note)
	}
	draft := ""
	if op.Draft != nil {
		draft = strings.TrimSpace(*op.Draft)
	}
	op.Result = fmt.Sprintf("Approved — %q will %s.%s", draft, action, note)
	slog.DebugContext(ctx, "DecisionOp.done", "run_id", dagor.RunID(ctx), "action", action)
	return nil
}

func (op *DecisionOp) InputFields() map[string]any {
	return map[string]any{
		"Approved":  &op.Approved,
		"ActionIdx": &op.ActionIdx,
		"Note":      &op.Note,
		"Draft":     &op.Draft,
	}
}

func (op *DecisionOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }

func (op *DecisionOp) SetInputField(field string, value any) error {
	switch field {
	case "Approved":
		v, ok := value.(*bool)
		if !ok {
			return fmt.Errorf("DecisionOp: Approved: expected *bool, got %T", value)
		}
		op.Approved = v
	case "ActionIdx":
		v, ok := value.(*int)
		if !ok {
			return fmt.Errorf("DecisionOp: ActionIdx: expected *int, got %T", value)
		}
		op.ActionIdx = v
	case "Note":
		v, ok := value.(*string)
		if !ok {
			return fmt.Errorf("DecisionOp: Note: expected *string, got %T", value)
		}
		op.Note = v
	case "Draft":
		v, ok := value.(*string)
		if !ok {
			return fmt.Errorf("DecisionOp: Draft: expected *string, got %T", value)
		}
		op.Draft = v
	default:
		return fmt.Errorf("DecisionOp: unknown field %q", field)
	}
	return nil
}

func (op *DecisionOp) ResetFields() {
	op.Approved = nil
	op.ActionIdx = nil
	op.Note = nil
	op.Draft = nil
	op.Result = ""
}

func init() {
	if err := operator.RegisterOpFactory("draft_const", builtin.ContextValFactory[string](draftKey{})); err != nil {
		log.Fatalf("register draft_const: %v", err)
	}
	if err := operator.RegisterOp[DecisionOp](); err != nil {
		log.Fatalf("register DecisionOp: %v", err)
	}
}

// ─── Graph ─────────────────────────────────────────────────────────────────

func buildGraph() (*graph.Graph, error) {
	b := graph.NewBuilder("human_in_the_loop")

	// Seed the draft onto a wire from context.
	b.Vertex("draft_const").Op("draft_const").
		Output("Result", "draft")

	// HITL 1 — approve? (bool). The draft is shown beneath the prompt.
	b.Vertex("confirm").Op("HumanBoolInputOp").
		Params(map[string]string{"prompt": "Approve this draft for delivery?"}).
		Input("Input", "draft").
		Output("Result", "approved")

	// HITL 2 — which delivery option? (choice → zero-based index).
	b.Vertex("action_choice").Op("HumanChoiceInputOp").
		Params(map[string]string{
			"prompt":  "How should it be delivered?",
			"options": strings.Join(deliveryOptions, ","),
		}).
		Input("Input", "draft").
		Output("Result", "action_idx")

	// HITL 3 — optional accompanying note (free text).
	b.Vertex("note").Op("HumanInputOp").
		Params(map[string]string{"prompt": "Add a short note to accompany it (leave blank for none):"}).
		Output("Result", "note")

	// The human answers flow into the rest of the workflow.
	return b.Vertex("decide").Op("DecisionOp").
		Input("Approved", "approved").
		Input("ActionIdx", "action_idx").
		Input("Note", "note").
		Input("Draft", "draft").
		Output("Result", "summary").
		Build()
}

// ─── Boundary types ────────────────────────────────────────────────────────

// UserInput is the external boundary. In CLI mode it is filled from --draft; in
// MCP mode the SDK derives the tool input schema from this struct.
type UserInput struct {
	Draft string `json:"draft" jsonschema:"the draft message to review and act on; required"`
}

// Result is the workflow's structured output.
type Result struct {
	Draft   string `json:"draft"`
	Summary string `json:"summary"`
}

// ─── Shared execution path ─────────────────────────────────────────────────

// runWorkflow builds a fresh graph + engine per invocation (so concurrent MCP
// tool calls never share mutable operator state), installs the HumanPrompter
// for this run, injects the draft, and extracts the structured Result. The
// prompter is the only thing that differs between CLI and MCP mode.
func runWorkflow(ctx context.Context, pool *ants.Pool, prompter library.HumanPrompter, in UserInput) (Result, error) {
	g, err := buildGraph()
	if err != nil {
		return Result{}, fmt.Errorf("build graph: %w", err)
	}
	eng, err := dagor.NewEngine(g, pool, dagor.WithReporter(reporter.New(slog.Default())))
	if err != nil {
		return Result{}, fmt.Errorf("create engine: %w", err)
	}

	// Install the human-input channel + the draft for this run.
	ctx = library.WithHumanPrompter(ctx, prompter)
	ctx = context.WithValue(ctx, draftKey{}, in.Draft)

	if err := eng.Run(ctx); err != nil {
		return Result{}, fmt.Errorf("run graph: %w", err)
	}

	out := Result{Draft: in.Draft}
	if raw, ok := eng.GetOutput("summary"); ok {
		if p, ok := raw.(*string); ok && p != nil {
			out.Summary = *p
		}
	}
	return out, nil
}

// ─── MCP server mode ───────────────────────────────────────────────────────

// runMCPServer exposes the workflow as one MCP tool over stdin/stdout. The key
// difference from a non-interactive workflow: the handler captures req.Session
// and installs an ElicitPrompter, so the Human* ops can bounce their questions
// back to the connected client mid-run.
func runMCPServer(pool *ants.Pool) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "human-in-the-loop",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "review_and_act",
		Description: "Given a draft, ask the human to approve it, choose a delivery option, and add an optional note, then return the resulting action summary.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in UserInput) (*mcp.CallToolResult, Result, error) {
		if strings.TrimSpace(in.Draft) == "" {
			return nil, Result{}, fmt.Errorf("draft is required")
		}
		// Reach the human through the live client session via MCP elicitation.
		prompter := library.ElicitPrompter(req.Session)
		res, err := runWorkflow(ctx, pool, prompter, in)
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
		draft   = flag.String("draft", "", "the draft message to review (CLI mode)")
	)
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

	if strings.TrimSpace(*draft) == "" {
		fmt.Fprintln(os.Stderr, "usage: human-in-the-loop --draft <message>  |  --mcp")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// CLI mode reaches the human on the terminal.
	res, err := runWorkflow(ctx, pool, library.StdinPrompter(), UserInput{Draft: *draft})
	if err != nil {
		log.Fatalf("workflow: %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		log.Fatalf("encode output: %v", err)
	}
}
