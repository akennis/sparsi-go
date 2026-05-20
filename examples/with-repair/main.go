// Package main demonstrates library.WithRepair — an AI-driven recovery
// wrapper around deterministic ops.
//
// The workflow ingests a raw JSON support-ticket payload and runs it through
// two repair-wrapped stages:
//
//  1. ParseTicketOp  — string-target repair. The op JSON-decodes the raw text
//     into a strict TicketInput struct. On JSON syntax errors or schema
//     violations (wrong priority code, missing required fields) it returns
//     *library.ErrRepairable carrying a self-contained prompt. The wrapper
//     forwards that prompt to the LLM, parses the response back into the
//     TicketRaw input via UnmarshalRepair, and re-runs the inner op.
//
//  2. ValidateRoutingOp — struct-target repair. The op applies business rules
//     (e.g. priority=urgent requires a non-empty escalation_contact) and on
//     violation returns *library.ErrRepairable with the offending ticket
//     rendered as XML in the prompt. The wrapper sends the prompt, parses the
//     LLM's response via TicketInput.UnmarshalRepair (which calls xml.Unmarshal),
//     and re-runs the validator.
//
// Together the two stages exercise both wire-format paths of the wrapper
// (string repair and XML-struct repair) in a single workflow.
//
// ASCII DAG (WithRepair-wrapped vertices carry a trailing [AI:WithRepair] tag
// per the sparsi-design renderer hint convention):
//
//	raw_input → parse_ticket [AI:WithRepair] → ticket
//	         → validate_routing [AI:WithRepair] → validated → render → final
//
// Run with:
//
//	go run ./examples/with-repair --input '{"id":"T-9","priority":"urgent",...}'
//	go run ./examples/with-repair --input @testdata/dirty-format.json
//	go run ./examples/with-repair --input @testdata/dirty-business-rule.json --reasoning
package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/akennis/sparsi-go/library"

	"github.com/panjf2000/ants/v2"
	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/graph"
	"github.com/wwz16/dagor/operator"
	builtin "github.com/wwz16/dagor/operator/builtin"
	"github.com/wwz16/dagor/reporter"
)

// ─── Context keys ──────────────────────────────────────────────────────────

type rawTicketKey struct{}

// ─── Domain types ──────────────────────────────────────────────────────────

// TicketRaw wraps the incoming raw text. Named (not bare string) so it can
// carry the UnmarshalRepair method that the WithRepair wrapper relies on.
type TicketRaw struct {
	Text string
}

func (t *TicketRaw) UnmarshalRepair(response string) error {
	t.Text = stripCodeFences(response)
	return nil
}

// TicketInput is the strict shape downstream nodes operate on. It carries
// both JSON and XML tags so the parse stage emits JSON and the validate stage
// can round-trip through XML during repair.
type TicketInput struct {
	XMLName           xml.Name `xml:"ticket"          json:"-"`
	ID                string   `xml:"id"              json:"id"`
	Priority          string   `xml:"priority"        json:"priority"`
	ReporterEmail     string   `xml:"reporter_email"  json:"reporter_email"`
	Summary           string   `xml:"summary"         json:"summary"`
	EscalationContact string   `xml:"escalation_contact,omitempty" json:"escalation_contact,omitempty"`
}

// UnmarshalRepair parses the LLM's XML response back into the struct. Used by
// the validate-stage WithRepair wrapper.
func (t *TicketInput) UnmarshalRepair(response string) error {
	cleaned := stripCodeFences(response)
	if err := xml.Unmarshal([]byte(cleaned), t); err != nil {
		return fmt.Errorf("xml.Unmarshal: %w (response: %q)", err, cleaned)
	}
	return nil
}

// ─── Op #1: ParseTicketOp (string-target repair) ───────────────────────────

var (
	idPattern    = regexp.MustCompile(`^T-\d+$`)
	emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	validPrios   = map[string]bool{"low": true, "medium": true, "high": true, "urgent": true}
)

const ticketSchemaSpec = `Required JSON shape:
{
  "id":              string matching ^T-\d+$,
  "priority":        one of "low" | "medium" | "high" | "urgent",
  "reporter_email":  RFC-shaped email address,
  "summary":         non-empty string,
  "escalation_contact": optional email (required when priority=="urgent")
}`

type ParseTicketOp struct {
	Raw    *TicketRaw  `dag:"input"`
	Result TicketInput `dag:"output"`
}

func (op *ParseTicketOp) Setup(_ *config.Params) error { return nil }
func (op *ParseTicketOp) Reset() error                 { return nil }

func (op *ParseTicketOp) Run(_ context.Context) error {
	raw := ""
	if op.Raw != nil {
		raw = op.Raw.Text
	}

	var parsed TicketInput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return &library.ErrRepairable{
			Prompt: fmt.Sprintf(
				"The text below should be a valid ticket JSON, but parsing failed:\n  %s\n\n%s\n\nInput:\n%s\n\nOutput corrected JSON only — the entire object, not a patch. No code fences.",
				err, ticketSchemaSpec, raw,
			),
			Cause: err,
		}
	}

	if violations := schemaViolations(&parsed); len(violations) > 0 {
		return &library.ErrRepairable{
			Prompt: fmt.Sprintf(
				"The JSON below parses but violates the ticket schema:\n  - %s\n\n%s\n\nInput:\n%s\n\nOutput corrected JSON only — the entire object, not a patch. No code fences.",
				strings.Join(violations, "\n  - "), ticketSchemaSpec, raw,
			),
			Cause: errors.New("schema: " + strings.Join(violations, "; ")),
		}
	}

	op.Result = parsed
	return nil
}

func (op *ParseTicketOp) InputFields() map[string]any  { return map[string]any{"Raw": &op.Raw} }
func (op *ParseTicketOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *ParseTicketOp) SetInputField(field string, value any) error {
	if field != "Raw" {
		return fmt.Errorf("ParseTicketOp: unknown field %q", field)
	}
	v, ok := value.(*TicketRaw)
	if !ok {
		return fmt.Errorf("ParseTicketOp: Raw: expected *TicketRaw, got %T", value)
	}
	op.Raw = v
	return nil
}
func (op *ParseTicketOp) ResetFields() {
	op.Raw = nil
	op.Result = TicketInput{}
}

func schemaViolations(t *TicketInput) []string {
	var v []string
	if !idPattern.MatchString(t.ID) {
		v = append(v, fmt.Sprintf(`field "id" must match ^T-\d+$, got %q`, t.ID))
	}
	if !validPrios[t.Priority] {
		v = append(v, fmt.Sprintf(`field "priority" must be one of low|medium|high|urgent, got %q`, t.Priority))
	}
	if !emailPattern.MatchString(t.ReporterEmail) {
		v = append(v, fmt.Sprintf(`field "reporter_email" must look like an email, got %q`, t.ReporterEmail))
	}
	if strings.TrimSpace(t.Summary) == "" {
		v = append(v, `field "summary" must be non-empty`)
	}
	return v
}

// ─── Op #2: ValidateRoutingOp (struct-target repair via XML) ───────────────

type ValidateRoutingOp struct {
	Ticket    *TicketInput `dag:"input"`
	Validated TicketInput  `dag:"output"`
}

func (op *ValidateRoutingOp) Setup(_ *config.Params) error { return nil }
func (op *ValidateRoutingOp) Reset() error                 { return nil }

func (op *ValidateRoutingOp) Run(_ context.Context) error {
	if op.Ticket == nil {
		return errors.New("ValidateRoutingOp: nil Ticket input")
	}

	if op.Ticket.Priority == "urgent" && strings.TrimSpace(op.Ticket.EscalationContact) == "" {
		rendered, _ := xml.MarshalIndent(op.Ticket, "", "  ")
		return &library.ErrRepairable{
			Prompt: fmt.Sprintf(
				"The ticket below has priority=\"urgent\" but no escalation_contact. "+
					"Routing requires an escalation_contact for urgent tickets. "+
					"Choose a sensible value based on the summary, or fall back to \"oncall@example.com\". "+
					"Output the corrected ticket as XML using the same root element <ticket> and the same child elements. "+
					"No code fences, no commentary.\n\nInput:\n%s",
				string(rendered),
			),
			Cause: errors.New("urgent ticket missing escalation_contact"),
		}
	}

	if len(op.Ticket.Summary) > 280 {
		rendered, _ := xml.MarshalIndent(op.Ticket, "", "  ")
		return &library.ErrRepairable{
			Prompt: fmt.Sprintf(
				"The ticket below has a summary longer than 280 characters (%d). "+
					"Rewrite the summary to be at most 280 characters while preserving the technical detail. "+
					"Output the corrected ticket as XML using the same root element <ticket> and the same child elements. "+
					"No code fences, no commentary.\n\nInput:\n%s",
				len(op.Ticket.Summary), string(rendered),
			),
			Cause: errors.New("summary exceeds 280 chars"),
		}
	}

	op.Validated = *op.Ticket
	return nil
}

func (op *ValidateRoutingOp) InputFields() map[string]any {
	return map[string]any{"Ticket": &op.Ticket}
}
func (op *ValidateRoutingOp) OutputFields() map[string]any {
	return map[string]any{"Validated": &op.Validated}
}
func (op *ValidateRoutingOp) SetInputField(field string, value any) error {
	if field != "Ticket" {
		return fmt.Errorf("ValidateRoutingOp: unknown field %q", field)
	}
	v, ok := value.(*TicketInput)
	if !ok {
		return fmt.Errorf("ValidateRoutingOp: Ticket: expected *TicketInput, got %T", value)
	}
	op.Ticket = v
	return nil
}
func (op *ValidateRoutingOp) ResetFields() {
	op.Ticket = nil
	op.Validated = TicketInput{}
}

// ─── Helpers ───────────────────────────────────────────────────────────────

// stripCodeFences strips ``` fences the LLM may emit despite our instructions.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}

// readInput returns the contents of --input, dereferencing the @file shorthand.
func readInput(arg string) (string, error) {
	if strings.HasPrefix(arg, "@") {
		data, err := os.ReadFile(strings.TrimPrefix(arg, "@"))
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return arg, nil
}

// ─── Registration ──────────────────────────────────────────────────────────

func init() {
	mustReg := func(name string, f func() operator.IOperator) {
		if err := operator.RegisterOpFactory(name, f); err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}
	mustErr := func(name string, err error) {
		if err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}

	mustReg("raw_ticket_const", builtin.ContextValFactory[TicketRaw](rawTicketKey{}))

	mustErr("ParseTicketRepair", library.RegisterWithRepair(
		"ParseTicketRepair",
		func() *ParseTicketOp { return &ParseTicketOp{} },
		library.RepairConfig{
			InputField:   "Raw",
			MaxAttempts:  3,
			PromptPrefix: "You are a strict JSON corrector. Output the corrected JSON only.\n\n",
		},
	))

	mustErr("ValidateRoutingRepair", library.RegisterWithRepair(
		"ValidateRoutingRepair",
		func() *ValidateRoutingOp { return &ValidateRoutingOp{} },
		library.RepairConfig{
			InputField:   "Ticket",
			MaxAttempts:  2,
			PromptPrefix: "You are a strict XML ticket corrector. Output corrected XML only.\n\n",
		},
	))
}

// ─── Graph ─────────────────────────────────────────────────────────────────

func buildGraph() (*graph.Graph, error) {
	return graph.NewBuilder("with_repair_demo").
		Vertex("source").Op("raw_ticket_const").
		Output("Result", "raw").
		Vertex("parse").Op("ParseTicketRepair").
		Input("Raw", "raw").
		Output("Result", "parsed").
		Vertex("validate").Op("ValidateRoutingRepair").
		Input("Ticket", "parsed").
		Output("Validated", "validated").
		Build()
}

// ─── Driver ────────────────────────────────────────────────────────────────

func main() {
	input := flag.String("input", "", "raw ticket JSON, or @path to read from a file")
	reasoning := flag.Bool("reasoning", false, "enable the reasoning log and print repair traces to stderr")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if *input == "" {
		fmt.Fprintln(os.Stderr, "usage: with-repair --input '<json>' | --input @path/to/file.json [--reasoning]")
		os.Exit(2)
	}
	raw, err := readInput(*input)
	if err != nil {
		log.Fatalf("read input: %v", err)
	}

	g, err := buildGraph()
	if err != nil {
		log.Fatalf("build graph: %v", err)
	}

	pool, err := ants.NewPool(4)
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
	ctx = context.WithValue(ctx, rawTicketKey{}, TicketRaw{Text: raw})

	var rlog *library.ReasoningLog
	if *reasoning {
		ctx, rlog = library.WithReasoningLog(ctx)
	}

	if err := eng.Run(ctx); err != nil {
		log.Fatalf("run graph: %v", err)
	}

	out, ok := eng.GetOutput("validated")
	if !ok {
		log.Fatalf("no \"validated\" output produced")
	}
	ticket, ok := out.(*TicketInput)
	if !ok {
		log.Fatalf("unexpected output type %T", out)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ticket); err != nil {
		log.Fatalf("encode output: %v", err)
	}

	if rlog != nil {
		entries := rlog.Entries()
		fmt.Fprintf(os.Stderr, "\n--- repair trace (%d entries) ---\n", len(entries))
		for _, e := range entries {
			fmt.Fprintf(os.Stderr, "[%s] %s — %s\n", e.Op, e.Reasoning, fmtInputs(e.Inputs))
		}
	}
}

func fmtInputs(m map[string]any) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, " ")
}
