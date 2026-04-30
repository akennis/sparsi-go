// Package main is a customer-support ticket triager.
//
// It reads a free-text ticket from a file, classifies it via ModeSelectOp into
// one of {billing, bug, feature, other}, and routes the ticket through a
// category-specific extraction lane.  The four lanes run in parallel as DAG
// branches; only the lane whose predicate matches actually fires, and its
// per-lane JSON summary coalesces into a single final brief.
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

	"github.com/akennis/clawdag-go/library"      // registers library ops
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

// EncodeOtherOp wraps the polite acknowledgement in the lane JSON envelope.
type EncodeOtherOp struct {
	Acknowledgement *string
	Result          string
}

func (op *EncodeOtherOp) Setup(_ *config.Params) error { return nil }
func (op *EncodeOtherOp) Reset() error                 { return nil }
func (op *EncodeOtherOp) Run(_ context.Context) error {
	details := map[string]any{}
	if op.Acknowledgement != nil {
		details["acknowledgement"] = *op.Acknowledgement
	}
	out := map[string]any{
		"category": "other",
		"details":  details,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("EncodeOtherOp: %w", err)
	}
	op.Result = string(b)
	return nil
}
func (op *EncodeOtherOp) InputFields() map[string]any {
	return map[string]any{"Acknowledgement": &op.Acknowledgement}
}
func (op *EncodeOtherOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}
func (op *EncodeOtherOp) SetInputField(field string, value any) error {
	if field != "Acknowledgement" {
		return fmt.Errorf("EncodeOtherOp: unknown field %q", field)
	}
	v, ok := value.(*string)
	if !ok {
		return fmt.Errorf("EncodeOtherOp: Acknowledgement: expected *string, got %T", value)
	}
	op.Acknowledgement = v
	return nil
}
func (op *EncodeOtherOp) ResetFields() {
	op.Acknowledgement = nil
	op.Result = ""
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
		operator.RegisterOp[EncodeOtherOp],
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
		Vertex("classify").Op("ModeSelectOp").
		Params(map[string]string{"categories": "billing,bug,feature,other"}).
		Input("Input", "ticket_body").
		Output("Result", "ticket_category").

		// ── Billing lane ────────────────────────────────────────────────
		Vertex("gate_billing").Op("LaneGateOp").
		Condition("lane_is_billing").
		ConditionInput("ticket_category").
		Input("Body", "ticket_body").
		Output("BodyOut", "billing_body").

		Vertex("billing_extract").Op("AIExtractMapOp").
		Params(map[string]string{"operation": "extract these fields from the customer support email and return key=value pairs only: name, email, account_id, total_amount, charge_count"}).
		Input("Input", "billing_body").
		Output("Result", "billing_map").

		Vertex("billing_refund").Op("AIParseNumberOp").
		Params(map[string]string{"operation": "the refund amount the customer is requesting in US dollars (a single number, no currency symbol)"}).
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
		Params(map[string]string{"operation": "extract the reproduction steps from this bug report as a flat comma-separated list (one step per item)"}).
		Input("Input", "bug_body").
		Output("Result", "bug_repro_steps").

		Vertex("bug_severity").Op("AIScoreOp").
		Params(map[string]string{"criterion": "severity and urgency of the reported bug, where 1.0 means production-blocking and 0.0 means cosmetic"}).
		Input("Input", "bug_body").
		Output("Result", "bug_severity_score").

		Vertex("bug_regression").Op("AIBoolOp").
		Params(map[string]string{"predicate": "does the report indicate this bug is a regression — that this functionality previously worked and recently broke?"}).
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
		Params(map[string]string{"operation": "summarize the feature being requested in one concise sentence"}).
		Input("Input", "feature_body").
		Output("Result", "feature_description").

		Vertex("feature_impact").Op("AIScoreOp").
		Params(map[string]string{"criterion": "business impact of building this feature, where 1.0 is critical to many users and 0.0 is purely cosmetic"}).
		Input("Input", "feature_body").
		Output("Result", "feature_business_impact").

		Vertex("feature_encode").Op("EncodeFeatureOp").
		Input("Description", "feature_description").
		Input("BusinessImpact", "feature_business_impact").
		Output("Result", "feature_json").

		// ── Other lane ──────────────────────────────────────────────────
		Vertex("gate_other").Op("LaneGateOp").
		Condition("lane_is_other").
		ConditionInput("ticket_category").
		Input("Body", "ticket_body").
		Output("BodyOut", "other_body").

		Vertex("other_ack").Op("AIComputeStringToStringOp").
		Params(map[string]string{"operation": "write a polite, brief one-paragraph acknowledgement of this customer email"}).
		Input("Input", "other_body").
		Output("Result", "other_acknowledgement").

		Vertex("other_encode").Op("EncodeOtherOp").
		Input("Acknowledgement", "other_acknowledgement").
		Output("Result", "other_json").

		// ── Coalesce: the one lane that ran wins ────────────────────────
		// CoalesceNStringOp is the N-input variant (the 4-input library variant
		// silently loses a name clash with dagor's 2-input builtin during init).
		Vertex("final").Op("CoalesceNStringOp").
		Params(map[string]int{"n": 4}).
		Merge(config.MergeCoalesce).
		Input("Input0", "billing_json").
		Input("Input1", "bug_json").
		Input("Input2", "feature_json").
		Input("Input3", "other_json").
		Output("Result", "final_brief").
		Build()
}

// ─── Driver ────────────────────────────────────────────────────────────────

func main() {
	var ticketPath string
	flag.StringVar(&ticketPath, "ticket", "", "path to a ticket text file")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	if ticketPath == "" {
		fmt.Fprintln(os.Stderr, "usage: 01-ticket-triager --ticket <file>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(ticketPath)
	if err != nil {
		log.Fatalf("read ticket: %v", err)
	}
	ticketBody := strings.TrimSpace(string(raw))
	if ticketBody == "" {
		log.Fatal("ticket file is empty")
	}

	registerPredicates()

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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ctx, reasonLog := library.WithReasoningLog(ctx)
	ctx = context.WithValue(ctx, ticketBodyKey{}, ticketBody)

	if err := eng.Run(ctx); err != nil {
		log.Fatalf("run graph: %v", err)
	}

	briefRaw, ok := eng.GetOutput("final_brief")
	if !ok {
		log.Fatal("final_brief wire missing from graph output")
	}
	briefPtr, ok := briefRaw.(*string)
	if !ok || briefPtr == nil {
		log.Fatalf("unexpected final_brief type: %T", briefRaw)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(*briefPtr), &result); err != nil {
		log.Fatalf("parse final_brief: %v\nraw: %s", err, *briefPtr)
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
		{"AIComputeStringToStringOp(other.ack)", "other_ack"},
	}
	fired := []string{}
	for _, c := range candidates {
		if !eng.VertexSkipped(c.vertex) {
			fired = append(fired, c.op)
		}
	}
	result["ai_nodes"] = fired

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
