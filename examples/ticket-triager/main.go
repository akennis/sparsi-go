// Package main is a customer-support ticket triager.
//
// It reads a free-text ticket — from a file (--ticket) or inline on the CLI
// (--ticket-text), or from the `ticket` field of an MCP request — classifies
// it via ModeSelectOp into
// one of {billing, bug, feature, other}, and routes the ticket through a
// category-specific extraction lane.  The billing, bug, and feature lanes run
// in parallel as DAG branches; only the lane whose predicate matches actually
// fires, and its per-lane JSON summary coalesces into a single final brief.
// Tickets that classify as "other" are intentionally unsupported: the "other"
// lane fails the run with an error instead of producing a brief, so the CLI
// exits non-zero and the MCP tool returns an error result.
//
// AIClientFactory demo. Every AI vertex carries a `credential_ref` param
// naming a "cost center" (triage, billing, bug, feature). A custom
// library.AIClientFactory — costCenterFactory below — maps that ref onto an
// env var (CLAUDE_API_KEY_<COSTCENTER>) so each team can be billed on its
// own Anthropic API key. When a per-cost-center key is unset, the factory
// falls back to CLAUDE_API_KEY so the demo still runs with the default
// single-key setup.
//
// Env vars are used here purely to keep the example self-contained. A
// production factory would resolve `credential_ref` against a real
// credential store — AWS Secrets Manager, GCP Secret Manager, HashiCorp
// Vault, Azure Key Vault, a KMS-decrypted blob, etc. — instead of reading
// process environment.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"google.golang.org/genai"

	"github.com/akennis/sparsi-go/library"      // registers library ops
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

type ticketBodyKey struct{}

// ─── Custom ops ────────────────────────────────────────────────────────────

// LaneGateOp passes a body string through unchanged.  Its job is to host a
// Condition predicate that gates the lane: when the predicate returns false
// this vertex is skipped and skip-propagation prunes the rest of the lane.
type LaneGateOp struct {
	Body    *string
	BodyOut string
}

func (op *LaneGateOp) Setup(_ *config.Params) error { return nil }
func (op *LaneGateOp) Reset() error                 { return nil }
func (op *LaneGateOp) Run(_ context.Context) error {
	op.BodyOut = *op.Body
	return nil
}
func (op *LaneGateOp) InputFields() map[string]any  { return map[string]any{"Body": &op.Body} }
func (op *LaneGateOp) OutputFields() map[string]any { return map[string]any{"BodyOut": &op.BodyOut} }
func (op *LaneGateOp) SetInputField(field string, value any) error {
	if field != "Body" {
		return fmt.Errorf("LaneGateOp: unknown field %q", field)
	}
	v, ok := value.(*string)
	if !ok {
		return fmt.Errorf("LaneGateOp: Body: expected *string, got %T", value)
	}
	op.Body = v
	return nil
}
func (op *LaneGateOp) ResetFields() { op.Body = nil; op.BodyOut = "" }

// EncodeBillingOp serializes the billing lane's extracted fields into a
// JSON-encoded summary.
type EncodeBillingOp struct {
	Details      *map[string]string
	RefundAmount *float64
	Result       string
}

func (op *EncodeBillingOp) Setup(_ *config.Params) error { return nil }
func (op *EncodeBillingOp) Reset() error                 { return nil }
func (op *EncodeBillingOp) Run(_ context.Context) error {
	out := map[string]any{
		"category": "billing",
		"details":  *op.Details,
	}
	if op.RefundAmount != nil {
		out["refund_amount_usd"] = *op.RefundAmount
	}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("EncodeBillingOp: %w", err)
	}
	op.Result = string(b)
	return nil
}
func (op *EncodeBillingOp) InputFields() map[string]any {
	return map[string]any{"Details": &op.Details, "RefundAmount": &op.RefundAmount}
}
func (op *EncodeBillingOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}
func (op *EncodeBillingOp) SetInputField(field string, value any) error {
	switch field {
	case "Details":
		v, ok := value.(*map[string]string)
		if !ok {
			return fmt.Errorf("EncodeBillingOp: Details: expected *map[string]string, got %T", value)
		}
		op.Details = v
	case "RefundAmount":
		v, ok := value.(*float64)
		if !ok {
			return fmt.Errorf("EncodeBillingOp: RefundAmount: expected *float64, got %T", value)
		}
		op.RefundAmount = v
	default:
		return fmt.Errorf("EncodeBillingOp: unknown field %q", field)
	}
	return nil
}
func (op *EncodeBillingOp) ResetFields() {
	op.Details = nil
	op.RefundAmount = nil
	op.Result = ""
}

// EncodeBugOp serializes the bug lane's outputs into a JSON-encoded summary.
type EncodeBugOp struct {
	Steps        *[]string
	Severity     *float64
	IsRegression *bool
	Result       string
}

func (op *EncodeBugOp) Setup(_ *config.Params) error { return nil }
func (op *EncodeBugOp) Reset() error                 { return nil }
func (op *EncodeBugOp) Run(_ context.Context) error {
	details := map[string]any{}
	if op.Steps != nil {
		details["reproduction_steps"] = *op.Steps
	}
	if op.Severity != nil {
		details["severity"] = *op.Severity
	}
	if op.IsRegression != nil {
		details["is_regression"] = *op.IsRegression
	}
	out := map[string]any{
		"category": "bug",
		"details":  details,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("EncodeBugOp: %w", err)
	}
	op.Result = string(b)
	return nil
}
func (op *EncodeBugOp) InputFields() map[string]any {
	return map[string]any{
		"Steps":        &op.Steps,
		"Severity":     &op.Severity,
		"IsRegression": &op.IsRegression,
	}
}
func (op *EncodeBugOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}
func (op *EncodeBugOp) SetInputField(field string, value any) error {
	switch field {
	case "Steps":
		v, ok := value.(*[]string)
		if !ok {
			return fmt.Errorf("EncodeBugOp: Steps: expected *[]string, got %T", value)
		}
		op.Steps = v
	case "Severity":
		v, ok := value.(*float64)
		if !ok {
			return fmt.Errorf("EncodeBugOp: Severity: expected *float64, got %T", value)
		}
		op.Severity = v
	case "IsRegression":
		v, ok := value.(*bool)
		if !ok {
			return fmt.Errorf("EncodeBugOp: IsRegression: expected *bool, got %T", value)
		}
		op.IsRegression = v
	default:
		return fmt.Errorf("EncodeBugOp: unknown field %q", field)
	}
	return nil
}
func (op *EncodeBugOp) ResetFields() {
	op.Steps = nil
	op.Severity = nil
	op.IsRegression = nil
	op.Result = ""
}

// EncodeFeatureOp serializes the feature lane's outputs.
type EncodeFeatureOp struct {
	Description    *string
	BusinessImpact *float64
	Result         string
}

func (op *EncodeFeatureOp) Setup(_ *config.Params) error { return nil }
func (op *EncodeFeatureOp) Reset() error                 { return nil }
func (op *EncodeFeatureOp) Run(_ context.Context) error {
	details := map[string]any{}
	if op.Description != nil {
		details["description"] = *op.Description
	}
	if op.BusinessImpact != nil {
		details["business_impact"] = *op.BusinessImpact
	}
	out := map[string]any{
		"category": "feature",
		"details":  details,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("EncodeFeatureOp: %w", err)
	}
	op.Result = string(b)
	return nil
}
func (op *EncodeFeatureOp) InputFields() map[string]any {
	return map[string]any{
		"Description":    &op.Description,
		"BusinessImpact": &op.BusinessImpact,
	}
}
func (op *EncodeFeatureOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}
func (op *EncodeFeatureOp) SetInputField(field string, value any) error {
	switch field {
	case "Description":
		v, ok := value.(*string)
		if !ok {
			return fmt.Errorf("EncodeFeatureOp: Description: expected *string, got %T", value)
		}
		op.Description = v
	case "BusinessImpact":
		v, ok := value.(*float64)
		if !ok {
			return fmt.Errorf("EncodeFeatureOp: BusinessImpact: expected *float64, got %T", value)
		}
		op.BusinessImpact = v
	default:
		return fmt.Errorf("EncodeFeatureOp: unknown field %q", field)
	}
	return nil
}
func (op *EncodeFeatureOp) ResetFields() {
	op.Description = nil
	op.BusinessImpact = nil
	op.Result = ""
}

// errOtherUnsupported is returned by RejectOtherOp when a ticket classifies as
// "other".  The "other" lane intentionally fails the run instead of producing a
// brief; runWorkflow wraps this with %w, so callers can errors.Is against it to
// surface a clean rejection (the MCP handler does this).
var errOtherUnsupported = errors.New(`ticket classified as "other": unsupported category — the triager only handles billing, bug, and feature tickets`)

// RejectOtherOp fails the run when the "other" lane fires.  It sits downstream
// of gate_other, so skip-propagation prunes it for every supported category;
// when a ticket actually classifies as "other" the gate fires, this op runs,
// and returning errOtherUnsupported aborts the whole graph.
type RejectOtherOp struct {
	Body *string
}

func (op *RejectOtherOp) Setup(_ *config.Params) error { return nil }
func (op *RejectOtherOp) Reset() error                 { return nil }
func (op *RejectOtherOp) Run(_ context.Context) error  { return errOtherUnsupported }
func (op *RejectOtherOp) InputFields() map[string]any  { return map[string]any{"Body": &op.Body} }
func (op *RejectOtherOp) OutputFields() map[string]any { return map[string]any{} }
func (op *RejectOtherOp) SetInputField(field string, value any) error {
	if field != "Body" {
		return fmt.Errorf("RejectOtherOp: unknown field %q", field)
	}
	v, ok := value.(*string)
	if !ok {
		return fmt.Errorf("RejectOtherOp: Body: expected *string, got %T", value)
	}
	op.Body = v
	return nil
}
func (op *RejectOtherOp) ResetFields() { op.Body = nil }

// ─── AIClientFactory: per-cost-center billing ──────────────────────────────

// costCenterFactory routes Anthropic traffic to a different API key based on
// the credential_ref the AI vertex declares. The ref names a cost center; the
// factory looks up CLAUDE_API_KEY_<COSTCENTER> and constructs the SDK client
// with that key, so each business unit is billed on its own Anthropic account.
//
// Falls back to CLAUDE_API_KEY when the per-cost-center var is unset, so this
// example still runs end-to-end with a single shared key.
//
// The env-var lookup is just what this demo uses. In a real deployment the
// same factory shape would fetch the key from AWS Secrets Manager, GCP
// Secret Manager, HashiCorp Vault, Azure Key Vault, a KMS-decrypted blob,
// or any other credential store — `credential_ref` is the lookup key, and
// what's behind it is up to the factory implementation.
type costCenterFactory struct {
	mu     sync.Mutex
	cached map[string]*anthropic.Client
}

func (f *costCenterFactory) Anthropic(_ context.Context, ref string) (*anthropic.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.cached[ref]; ok {
		return c, nil
	}

	primary := "CLAUDE_API_KEY"
	if ref != "" {
		primary = "CLAUDE_API_KEY_" + strings.ToUpper(ref)
	}
	source := primary
	key := os.Getenv(primary)
	if key == "" && primary != "CLAUDE_API_KEY" {
		source = "CLAUDE_API_KEY"
		key = os.Getenv("CLAUDE_API_KEY")
	}
	if key == "" {
		return nil, fmt.Errorf("costCenterFactory: no API key for cost center %q (looked at %s, then CLAUDE_API_KEY)", ref, primary)
	}

	slog.Info("ticket-triager.factory.resolve", "cost_center", ref, "env_var", source)

	c := anthropic.NewClient(option.WithAPIKey(key))
	if f.cached == nil {
		f.cached = map[string]*anthropic.Client{}
	}
	f.cached[ref] = &c
	return &c, nil
}

// Gemini is required by the AIClientFactory interface but unused here — this
// example is Claude-only. Reaching this path means a misconfigured vertex
// asked for Gemini against this factory; fail loud.
func (f *costCenterFactory) Gemini(_ context.Context, _ string) (*genai.Client, error) {
	return nil, fmt.Errorf("costCenterFactory: Gemini is not configured for ticket-triager")
}

func init() {
	mustReg := func(name string, f func() operator.IOperator) {
		if err := operator.RegisterOpFactory(name, f); err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}
	mustReg("body_const", builtin.ContextValFactory[string](ticketBodyKey{}))

	for _, reg := range []func() error{
		operator.RegisterOp[LaneGateOp],
		operator.RegisterOp[EncodeBillingOp],
		operator.RegisterOp[EncodeBugOp],
		operator.RegisterOp[EncodeFeatureOp],
		operator.RegisterOp[RejectOtherOp],
	} {
		if err := reg(); err != nil {
			log.Fatalf("register custom op: %v", err)
		}
	}
}

// ─── Predicates ────────────────────────────────────────────────────────────

func registerPredicates() {
	for _, lane := range []string{"billing", "bug", "feature", "other"} {
		want := lane
		name := "lane_is_" + lane
		if err := predicate.Register(name, func(inputs map[string]any) bool {
			v, ok := inputs["ticket_category"].(*string)
			return ok && v != nil && *v == want
		}); err != nil {
			log.Fatalf("register predicate %s: %v", name, err)
		}
	}
}

// ─── Graph ─────────────────────────────────────────────────────────────────

func buildGraph() (*graph.Graph, error) {
	return graph.NewBuilder("ticket_triage").

		// Inject the ticket body from the run context.
		Vertex("body_const").Op("body_const").
		Output("Result", "ticket_body").

		// Classify into one of 4 categories via a single AI call.
		// credential_ref="triage" → billed against CLAUDE_API_KEY_TRIAGE.
		Vertex("classify").Op("ModeSelectOp").
		Params(map[string]string{
			"categories":     "billing,bug,feature,other",
			"credential_ref": "triage",
		}).
		Input("Input", "ticket_body").
		Output("Result", "ticket_category").

		// ── Billing lane ────────────────────────────────────────────────
		Vertex("gate_billing").Op("LaneGateOp").
		Condition("lane_is_billing").
		ConditionInput("ticket_category").
		Input("Body", "ticket_body").
		Output("BodyOut", "billing_body").

		Vertex("billing_extract").Op("AIExtractMapOp").
		Params(map[string]string{
			"operation":      "extract these fields from the customer support email and return key=value pairs only: name, email, account_id, total_amount, charge_count",
			"credential_ref": "billing",
		}).
		Input("Input", "billing_body").
		Output("Result", "billing_map").

		Vertex("billing_refund").Op("AIParseNumberOp").
		Params(map[string]string{
			"operation":      "the refund amount the customer is requesting in US dollars (a single number, no currency symbol)",
			"credential_ref": "billing",
		}).
		Input("Input", "billing_body").
		Output("Result", "billing_refund_amount").

		Vertex("billing_encode").Op("EncodeBillingOp").
		Input("Details", "billing_map").
		Input("RefundAmount", "billing_refund_amount").
		Output("Result", "billing_json").

		// ── Bug lane ────────────────────────────────────────────────────
		Vertex("gate_bug").Op("LaneGateOp").
		Condition("lane_is_bug").
		ConditionInput("ticket_category").
		Input("Body", "ticket_body").
		Output("BodyOut", "bug_body").

		Vertex("bug_steps").Op("AIExtractStringSliceOp").
		Params(map[string]string{
			"operation":      "extract the reproduction steps from this bug report as a flat comma-separated list (one step per item)",
			"credential_ref": "bug",
		}).
		Input("Input", "bug_body").
		Output("Result", "bug_repro_steps").

		Vertex("bug_severity").Op("AIScoreOp").
		Params(map[string]string{
			"criterion":      "severity and urgency of the reported bug, where 1.0 means production-blocking and 0.0 means cosmetic",
			"credential_ref": "bug",
		}).
		Input("Input", "bug_body").
		Output("Result", "bug_severity_score").

		Vertex("bug_regression").Op("AIBoolOp").
		Params(map[string]string{
			"predicate":      "does the report indicate this bug is a regression — that this functionality previously worked and recently broke?",
			"credential_ref": "bug",
		}).
		Input("Input", "bug_body").
		Output("Result", "bug_is_regression").

		Vertex("bug_encode").Op("EncodeBugOp").
		Input("Steps", "bug_repro_steps").
		Input("Severity", "bug_severity_score").
		Input("IsRegression", "bug_is_regression").
		Output("Result", "bug_json").

		// ── Feature lane ────────────────────────────────────────────────
		Vertex("gate_feature").Op("LaneGateOp").
		Condition("lane_is_feature").
		ConditionInput("ticket_category").
		Input("Body", "ticket_body").
		Output("BodyOut", "feature_body").

		Vertex("feature_summary").Op("AIComputeStringToStringOp").
		Params(map[string]string{
			"operation":      "summarize the feature being requested in one concise sentence",
			"credential_ref": "feature",
		}).
		Input("Input", "feature_body").
		Output("Result", "feature_description").

		Vertex("feature_impact").Op("AIScoreOp").
		Params(map[string]string{
			"criterion":      "business impact of building this feature, where 1.0 is critical to many users and 0.0 is purely cosmetic",
			"credential_ref": "feature",
		}).
		Input("Input", "feature_body").
		Output("Result", "feature_business_impact").

		Vertex("feature_encode").Op("EncodeFeatureOp").
		Input("Description", "feature_description").
		Input("BusinessImpact", "feature_business_impact").
		Output("Result", "feature_json").

		// ── Other lane: unsupported → fail the run ───────────────────────
		// Gated on lane_is_other.  When a ticket classifies as "other" the
		// gate fires and RejectOtherOp errors out, aborting the whole run;
		// for every supported category the gate is skipped and
		// skip-propagation prunes RejectOtherOp so it never fires.
		Vertex("gate_other").Op("LaneGateOp").
		Condition("lane_is_other").
		ConditionInput("ticket_category").
		Input("Body", "ticket_body").
		Output("BodyOut", "other_body").

		Vertex("other_reject").Op("RejectOtherOp").
		Input("Body", "other_body").

		// ── Coalesce: the one lane that ran wins ────────────────────────
		// CoalesceNStringOp is the N-input variant (the library variant
		// silently loses a name clash with dagor's 2-input builtin during
		// init).  Only the three supported lanes feed it; "other" never
		// reaches here because RejectOtherOp fails the run first.
		Vertex("final").Op("CoalesceNStringOp").
		Params(map[string]int{"n": 3}).
		Merge(config.MergeCoalesce).
		Input("Input0", "billing_json").
		Input("Input1", "bug_json").
		Input("Input2", "feature_json").
		Output("Result", "final_brief").
		Build()
}

// ─── Workflow inputs / outputs ─────────────────────────────────────────────

// UserInput is the external boundary of the workflow. In CLI mode it is filled
// from --ticket (a file path); in MCP mode the SDK derives the tool input
// schema from this struct and deserializes + validates every tools/call
// request into it.
type UserInput struct {
	Ticket string `json:"ticket" jsonschema:"the full free-text customer-support ticket body to triage; required"`
}

// Result is the workflow's structured output: the coalesced lane brief plus
// the resolved category and the AI vertices that fired. Its shape varies by
// lane (billing/bug/feature/other), so it is an open object. It is the MCP
// tool's typed Out (the SDK emits it as structured content + a JSON text
// block) and the CLI's stdout.
type Result = map[string]any

// ─── Shared execution path ─────────────────────────────────────────────────

// runWorkflow is the single path shared by CLI and MCP modes. It builds a
// fresh graph + engine per invocation so concurrent MCP tool calls never share
// mutable operator state; the ants.Pool is safe to share. The ticket body is
// injected via context.WithValue exactly as in CLI mode.
func runWorkflow(ctx context.Context, pool *ants.Pool, in UserInput) (Result, error) {
	g, err := buildGraph()
	if err != nil {
		return nil, fmt.Errorf("build graph: %w", err)
	}

	eng, err := dagor.NewEngine(g, pool, dagor.WithReporter(reporter.New(slog.Default())))
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	ctx = context.WithValue(ctx, ticketBodyKey{}, strings.TrimSpace(in.Ticket))

	if err := eng.Run(ctx); err != nil {
		return nil, fmt.Errorf("run graph: %w", err)
	}

	briefRaw, ok := eng.GetOutput("final_brief")
	if !ok {
		return nil, fmt.Errorf("final_brief wire missing from graph output")
	}
	briefPtr, ok := briefRaw.(*string)
	if !ok || briefPtr == nil {
		return nil, fmt.Errorf("unexpected final_brief type: %T", briefRaw)
	}

	var result Result
	if err := json.Unmarshal([]byte(*briefPtr), &result); err != nil {
		return nil, fmt.Errorf("parse final_brief: %w\nraw: %s", err, *briefPtr)
	}

	if catRaw, ok := eng.GetOutput("ticket_category"); ok {
		if cat, ok := catRaw.(*string); ok && cat != nil {
			result["category"] = *cat
		}
	}

	// Record which AI vertices actually fired (skipped lanes are pruned).
	candidates := []struct {
		op, vertex string
	}{
		{"ModeSelectOp", "classify"},
		{"AIExtractMapOp(billing.extract)", "billing_extract"},
		{"AIParseNumberOp(billing.refund)", "billing_refund"},
		{"AIExtractStringSliceOp(bug.steps)", "bug_steps"},
		{"AIScoreOp(bug.severity)", "bug_severity"},
		{"AIBoolOp(bug.regression)", "bug_regression"},
		{"AIComputeStringToStringOp(feature.summary)", "feature_summary"},
		{"AIScoreOp(feature.impact)", "feature_impact"},
	}
	fired := []string{}
	for _, c := range candidates {
		if !eng.VertexSkipped(c.vertex) {
			fired = append(fired, c.op)
		}
	}
	result["ai_nodes"] = fired
	return result, nil
}

// ─── MCP server mode ───────────────────────────────────────────────────────

// runMCPServer exposes the workflow as one MCP tool over stdin/stdout. The SDK
// infers the tool input schema from UserInput, auto-deserializes + validates
// each request, and (because Out is non-any) emits Result as structured
// content plus a JSON text block, so the handler returns nil for the result.
func runMCPServer(pool *ants.Pool) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "ticket-triager",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "triage_ticket",
		Description: "Classify a customer-support ticket (billing/bug/feature/other) and run the matching extraction lane, returning a structured brief plus the resolved category.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in UserInput) (*mcp.CallToolResult, Result, error) {
		if strings.TrimSpace(in.Ticket) == "" {
			return nil, nil, fmt.Errorf("ticket is required")
		}
		res, err := runWorkflow(ctx, pool, in)
		if err != nil {
			if errors.Is(err, errOtherUnsupported) {
				return nil, nil, fmt.Errorf("this ticket was classified as \"other\" and cannot be triaged: the triager supports only billing, bug, and feature tickets — please resubmit a ticket that falls into one of those categories")
			}
			return nil, nil, err
		}
		return nil, res, nil
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("mcp server: %v", err)
	}
}

// ─── Entrypoint ────────────────────────────────────────────────────────────

func main() {
	mcpMode := flag.Bool("mcp", false, "run as a stdio MCP server instead of a one-shot CLI")
	var ticketPath string
	flag.StringVar(&ticketPath, "ticket", "", "path to a ticket text file (CLI mode)")
	var ticketText string
	flag.StringVar(&ticketText, "ticket-text", "", "the ticket body as inline text (CLI mode; alternative to --ticket)")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	registerPredicates()

	// Route every AI op through the per-cost-center factory so each vertex's
	// credential_ref param maps onto its own CLAUDE_API_KEY_<COSTCENTER> env
	// var. Must happen before the engine runs Setup() on any AI op.
	library.SetDefaultAIClientFactory(&costCenterFactory{})

	pool, err := ants.NewPool(10)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	defer pool.Release()

	if *mcpMode {
		runMCPServer(pool)
		return
	}

	var ticketBody string
	switch {
	case ticketText != "" && ticketPath != "":
		fmt.Fprintln(os.Stderr, "provide either --ticket or --ticket-text, not both")
		os.Exit(2)
	case ticketText != "":
		ticketBody = strings.TrimSpace(ticketText)
	case ticketPath != "":
		raw, err := os.ReadFile(ticketPath)
		if err != nil {
			log.Fatalf("read ticket: %v", err)
		}
		ticketBody = strings.TrimSpace(string(raw))
	default:
		fmt.Fprintln(os.Stderr, "usage: 01-ticket-triager (--ticket <file> | --ticket-text <body>) | --mcp")
		os.Exit(2)
	}
	if ticketBody == "" {
		log.Fatal("ticket is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ctx, reasonLog := library.WithReasoningLog(ctx)

	result, err := runWorkflow(ctx, pool, UserInput{Ticket: ticketBody})
	if err != nil {
		log.Fatalf("workflow: %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		log.Fatalf("encode output: %v", err)
	}

	dumpReasoning(reasonLog.Entries())
}

// dumpReasoning prints reasoning entries to stderr after the primary JSON
// output.  Input values longer than 120 characters are truncated for
// readability.
func dumpReasoning(entries []library.ReasoningEntry) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "\n─── AI Reasoning ────────────────────────────────────────────────────────────")
	for i, e := range entries {
		fmt.Fprintf(os.Stderr, "[%d] %s\n", i+1, e.Op)
		for k, v := range e.Inputs {
			s := strings.ReplaceAll(fmt.Sprintf("%v", v), "\n", " ")
			if len(s) > 120 {
				s = s[:117] + "..."
			}
			fmt.Fprintf(os.Stderr, "    %-12s %s\n", k+":", s)
		}
		fmt.Fprintf(os.Stderr, "    → %s\n", e.Reasoning)
		if i < len(entries)-1 {
			fmt.Fprintln(os.Stderr)
		}
	}
	fmt.Fprintln(os.Stderr, "─────────────────────────────────────────────────────────────────────────────")
}
