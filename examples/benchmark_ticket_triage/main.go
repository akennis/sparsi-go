package main

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/akennis/dagor"
	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/graph"
	"github.com/akennis/dagor/operator"
	builtin "github.com/akennis/dagor/operator/builtin"
	"github.com/akennis/sparsi-go/library"
	"github.com/panjf2000/ants/v2"
	"google.golang.org/genai"
)

var PROMPTS map[string]string

func init() {
	b, err := os.ReadFile("prompts.json")
	if err != nil {
		log.Fatalf("failed to read prompts.json: %v", err)
	}
	if err := json.Unmarshal(b, &PROMPTS); err != nil {
		log.Fatalf("failed to parse prompts.json: %v", err)
	}
}

var AVAILABLE_INTENTS = []string{
	"cancel_order", "change_order", "change_shipping_address",
	"check_cancellation_fee", "check_invoice", "check_payment_methods",
	"check_refund_policy", "complaint", "contact_customer_service",
	"contact_human_agent", "create_account", "delete_account",
	"delivery_options", "delivery_period", "edit_account", "get_invoice",
	"get_refund", "newsletter_subscription", "payment_issue",
	"place_order", "recover_password", "registration_problems",
	"review", "set_up_shipping_address", "switch_account",
	"track_order", "track_refund",
}

var INTENT_LIST_STR = strings.Join(AVAILABLE_INTENTS, ", ")

func getMockUserProfile(utterance string) map[string]any {
	hash := fmt.Sprintf("%x", md5.Sum([]byte(utterance)))
	token := hash[:16]
	return map[string]any{
		"user_email":     "customer@example.com",
		"account_status": "active",
		"metadata": map[string]any{
			"security_token": token,
			"session_id":     "sess_" + token[:8],
		},
		"loyalty":     map[string]any{"tier": "gold", "points": 1450},
		"preferences": map[string]any{"language": "en", "notifications": true},
	}
}

func getTruePolicy(intent string) string {
	escalate := map[string]bool{"complaint": true, "payment_issue": true, "contact_human_agent": true}
	reject := map[string]bool{"cancel_order": true, "get_refund": true}

	if escalate[intent] {
		return "escalate"
	} else if reject[intent] {
		return "reject"
	}
	return "standard_process"
}

func evaluateResponse(ctx context.Context, client *genai.Client, utterance, intent, policy, email, expectedToken string) bool {
	if email == "" || !strings.Contains(email, expectedToken) {
		return false
	}

	prompt := fmt.Sprintf(`You are a strict Judge evaluating a customer support email.
UTTERANCE: '%s'
EXPECTED INTENT: '%s'
EXPECTED POLICY ACTION: '%s'
DRAFT EMAIL TO EVALUATE: '%s'

Does the draft email politely address the user's utterance, reflect the expected intent, and follow the expected policy action?
Respond with exactly one word: PASS or FAIL.`, utterance, intent, policy, email)

	config := &genai.GenerateContentConfig{Temperature: genai.Ptr[float32](0)}
	res, err := client.Models.GenerateContent(ctx, "gemini-3.5-flash", []*genai.Content{genai.NewContentFromText(prompt, genai.RoleUser)}, config)
	if err != nil {
		return false
	}
	text := strings.ToUpper(strings.TrimSpace(res.Text()))
	return strings.Contains(text, "PASS")
}

// -------------------------------------------------------------------
// CUSTOM STRUCTS & OPS
// -------------------------------------------------------------------

type SentimentResult struct {
	Sentiment    string `json:"sentiment"`
	UrgencyScore int    `json:"urgency_score"`
}

func (r *SentimentResult) ExpectedFormat() string {
	return `Respond with a JSON object containing "sentiment" (string) and "urgency_score" (int).`
}

type IntentResult struct {
	Intent string `json:"intent"`
}

func (r *IntentResult) ExpectedFormat() string {
	return `Respond with a JSON object containing "intent" (string).`
}

type PolicyResult struct {
	PolicyAction string `json:"policy_action"`
}

func (r *PolicyResult) ExpectedFormat() string {
	return `Respond with a JSON object containing "policy_action" (string).`
}

type DraftResult struct {
	DraftEmail string `json:"draft_email"`
}

func (r *DraftResult) ExpectedFormat() string {
	return `Respond with a JSON object containing "draft_email" (string).`
}

type AnalyzeSentimentOp struct {
	library.AIComputeOp[string, SentimentResult]
}

type ClassifyIntentOp struct {
	library.AIComputeOp[string, IntentResult]
}

type CheckPolicyOp struct {
	library.AIComputeOp[string, PolicyResult]
}

type DraftResponseOp struct {
	library.AIComputeOp[string, DraftResult]
}

type FetchUserContextOp struct {
	Utterance *string        `dag:"input"`
	Result    map[string]any `dag:"output"`
}

func (op *FetchUserContextOp) Setup(_ *config.Params) error { return nil }
func (op *FetchUserContextOp) Reset() error                 { return nil }
func (op *FetchUserContextOp) Run(_ context.Context) error {
	op.Result = getMockUserProfile(*op.Utterance)
	return nil
}
func (op *FetchUserContextOp) InputFields() map[string]any {
	return map[string]any{"Utterance": &op.Utterance}
}
func (op *FetchUserContextOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}
func (op *FetchUserContextOp) SetInputField(field string, value any) error {
	if field == "Utterance" {
		op.Utterance = value.(*string)
		return nil
	}
	return fmt.Errorf("unknown field")
}
func (op *FetchUserContextOp) ResetFields() {
	op.Utterance = nil
	op.Result = nil
}

type SendEmailOp struct {
	IntentResult *IntentResult   `dag:"input"`
	PolicyResult *PolicyResult   `dag:"input"`
	DraftResult  *DraftResult    `dag:"input"`
	UserProfile  *map[string]any `dag:"input"`
	Result       map[string]any  `dag:"output"`
}

func (op *SendEmailOp) Setup(_ *config.Params) error { return nil }
func (op *SendEmailOp) Reset() error                 { return nil }
func (op *SendEmailOp) Run(_ context.Context) error {
	res := make(map[string]any)
	if op.IntentResult != nil {
		res["intent"] = op.IntentResult.Intent
	} else {
		res["intent"] = ""
	}
	if op.PolicyResult != nil {
		res["policy_action"] = op.PolicyResult.PolicyAction
	} else {
		res["policy_action"] = ""
	}
	if op.DraftResult != nil {
		res["draft_email"] = op.DraftResult.DraftEmail
	} else {
		res["draft_email"] = ""
	}
	if op.UserProfile != nil {
		res["user_profile"] = *op.UserProfile
	}
	op.Result = res
	return nil
}
func (op *SendEmailOp) InputFields() map[string]any {
	return map[string]any{"IntentResult": &op.IntentResult, "PolicyResult": &op.PolicyResult, "DraftResult": &op.DraftResult, "UserProfile": &op.UserProfile}
}
func (op *SendEmailOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}
func (op *SendEmailOp) SetInputField(field string, value any) error {
	switch field {
	case "IntentResult":
		op.IntentResult = value.(*IntentResult)
	case "PolicyResult":
		op.PolicyResult = value.(*PolicyResult)
	case "DraftResult":
		op.DraftResult = value.(*DraftResult)
	case "UserProfile":
		op.UserProfile = value.(*map[string]any)
	default:
		return fmt.Errorf("unknown field")
	}
	return nil
}
func (op *SendEmailOp) ResetFields() {
	op.IntentResult = nil
	op.PolicyResult = nil
	op.DraftResult = nil
	op.UserProfile = nil
	op.Result = nil
}

type FormatIntentContextOp struct {
	Utterance *string          `dag:"input"`
	Profile   *map[string]any  `dag:"input"`
	Sentiment *SentimentResult `dag:"input"`
	Result    string           `dag:"output"`
}

func (op *FormatIntentContextOp) Setup(_ *config.Params) error { return nil }
func (op *FormatIntentContextOp) Reset() error                 { return nil }
func (op *FormatIntentContextOp) Run(_ context.Context) error {
	profStr, sentStr := "{}", "{}"
	if op.Profile != nil {
		b, _ := json.Marshal(*op.Profile)
		profStr = string(b)
	}
	if op.Sentiment != nil {
		b, _ := json.Marshal(*op.Sentiment)
		sentStr = string(b)
	}
	op.Result = fmt.Sprintf("UTTERANCE: %s\nPROFILE: %s\nSENTIMENT: %s", *op.Utterance, profStr, sentStr)
	return nil
}
func (op *FormatIntentContextOp) InputFields() map[string]any {
	return map[string]any{"Utterance": &op.Utterance, "Profile": &op.Profile, "Sentiment": &op.Sentiment}
}
func (op *FormatIntentContextOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}
func (op *FormatIntentContextOp) SetInputField(field string, value any) error {
	switch field {
	case "Utterance":
		op.Utterance = value.(*string)
	case "Profile":
		op.Profile = value.(*map[string]any)
	case "Sentiment":
		op.Sentiment = value.(*SentimentResult)
	default:
		return fmt.Errorf("unknown field")
	}
	return nil
}
func (op *FormatIntentContextOp) ResetFields() {
	op.Utterance, op.Profile, op.Sentiment, op.Result = nil, nil, nil, ""
}

type FormatPolicyContextOp struct {
	Intent    *IntentResult    `dag:"input"`
	Sentiment *SentimentResult `dag:"input"`
	Result    string           `dag:"output"`
}

func (op *FormatPolicyContextOp) Setup(_ *config.Params) error { return nil }
func (op *FormatPolicyContextOp) Reset() error                 { return nil }
func (op *FormatPolicyContextOp) Run(_ context.Context) error {
	intentStr, sentStr := "{}", "{}"
	if op.Intent != nil {
		b, _ := json.Marshal(*op.Intent)
		intentStr = string(b)
	}
	if op.Sentiment != nil {
		b, _ := json.Marshal(*op.Sentiment)
		sentStr = string(b)
	}
	op.Result = fmt.Sprintf("INTENT: %s\nSENTIMENT: %s", intentStr, sentStr)
	return nil
}
func (op *FormatPolicyContextOp) InputFields() map[string]any {
	return map[string]any{"Intent": &op.Intent, "Sentiment": &op.Sentiment}
}
func (op *FormatPolicyContextOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}
func (op *FormatPolicyContextOp) SetInputField(field string, value any) error {
	switch field {
	case "Intent":
		op.Intent = value.(*IntentResult)
	case "Sentiment":
		op.Sentiment = value.(*SentimentResult)
	default:
		return fmt.Errorf("unknown field")
	}
	return nil
}
func (op *FormatPolicyContextOp) ResetFields() {
	op.Intent, op.Sentiment, op.Result = nil, nil, ""
}

type FormatDraftContextOp struct {
	Utterance *string         `dag:"input"`
	Intent    *IntentResult   `dag:"input"`
	Policy    *PolicyResult   `dag:"input"`
	Profile   *map[string]any `dag:"input"`
	Result    string          `dag:"output"`
}

func (op *FormatDraftContextOp) Setup(_ *config.Params) error { return nil }
func (op *FormatDraftContextOp) Reset() error                 { return nil }
func (op *FormatDraftContextOp) Run(_ context.Context) error {
	intentStr, polStr, profStr := "{}", "{}", "{}"
	if op.Intent != nil {
		b, _ := json.Marshal(*op.Intent)
		intentStr = string(b)
	}
	if op.Policy != nil {
		b, _ := json.Marshal(*op.Policy)
		polStr = string(b)
	}
	if op.Profile != nil {
		b, _ := json.Marshal(*op.Profile)
		profStr = string(b)
	}
	op.Result = fmt.Sprintf("UTTERANCE: %s\nINTENT: %s\nPOLICY: %s\nPROFILE: %s", *op.Utterance, intentStr, polStr, profStr)
	return nil
}
func (op *FormatDraftContextOp) InputFields() map[string]any {
	return map[string]any{"Utterance": &op.Utterance, "Intent": &op.Intent, "Policy": &op.Policy, "Profile": &op.Profile}
}
func (op *FormatDraftContextOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}
func (op *FormatDraftContextOp) SetInputField(field string, value any) error {
	switch field {
	case "Utterance":
		op.Utterance = value.(*string)
	case "Intent":
		op.Intent = value.(*IntentResult)
	case "Policy":
		op.Policy = value.(*PolicyResult)
	case "Profile":
		op.Profile = value.(*map[string]any)
	default:
		return fmt.Errorf("unknown field")
	}
	return nil
}
func (op *FormatDraftContextOp) ResetFields() {
	op.Utterance, op.Intent, op.Policy, op.Profile, op.Result = nil, nil, nil, nil, ""
}

type Sum4TokensOp struct {
	In2      *int64 `dag:"input"`
	Out2     *int64 `dag:"input"`
	In3      *int64 `dag:"input"`
	Out3     *int64 `dag:"input"`
	In4      *int64 `dag:"input"`
	Out4     *int64 `dag:"input"`
	In5      *int64 `dag:"input"`
	Out5     *int64 `dag:"input"`
	TotalIn  int64  `dag:"output"`
	TotalOut int64  `dag:"output"`
}

func (op *Sum4TokensOp) Setup(_ *config.Params) error { return nil }
func (op *Sum4TokensOp) Reset() error                 { return nil }
func (op *Sum4TokensOp) Run(_ context.Context) error {
	var in, out int64
	if op.In2 != nil {
		in += *op.In2
	}
	if op.In3 != nil {
		in += *op.In3
	}
	if op.In4 != nil {
		in += *op.In4
	}
	if op.In5 != nil {
		in += *op.In5
	}

	if op.Out2 != nil {
		out += *op.Out2
	}
	if op.Out3 != nil {
		out += *op.Out3
	}
	if op.Out4 != nil {
		out += *op.Out4
	}
	if op.Out5 != nil {
		out += *op.Out5
	}

	op.TotalIn = in
	op.TotalOut = out
	return nil
}
func (op *Sum4TokensOp) InputFields() map[string]any {
	return map[string]any{"In2": &op.In2, "In3": &op.In3, "In4": &op.In4, "In5": &op.In5, "Out2": &op.Out2, "Out3": &op.Out3, "Out4": &op.Out4, "Out5": &op.Out5}
}
func (op *Sum4TokensOp) OutputFields() map[string]any {
	return map[string]any{"TotalIn": &op.TotalIn, "TotalOut": &op.TotalOut}
}
func (op *Sum4TokensOp) SetInputField(field string, value any) error {
	switch field {
	case "In2":
		op.In2 = value.(*int64)
	case "In3":
		op.In3 = value.(*int64)
	case "In4":
		op.In4 = value.(*int64)
	case "In5":
		op.In5 = value.(*int64)
	case "Out2":
		op.Out2 = value.(*int64)
	case "Out3":
		op.Out3 = value.(*int64)
	case "Out4":
		op.Out4 = value.(*int64)
	case "Out5":
		op.Out5 = value.(*int64)
	default:
		return fmt.Errorf("unknown field")
	}
	return nil
}
func (op *Sum4TokensOp) ResetFields() {
	op.In2, op.In3, op.In4, op.In5 = nil, nil, nil, nil
	op.Out2, op.Out3, op.Out4, op.Out5 = nil, nil, nil, nil
	op.TotalIn, op.TotalOut = 0, 0
}

type ticketUtteranceKey struct{}

func init() {
	mustReg := func(name string, f func() operator.IOperator) {
		if err := operator.RegisterOpFactory(name, f); err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}
	mustReg("body_const", builtin.ContextValFactory[string](ticketUtteranceKey{}))
	operator.RegisterOp[FetchUserContextOp]()
	operator.RegisterOp[SendEmailOp]()
	operator.RegisterOp[FormatIntentContextOp]()
	operator.RegisterOp[FormatPolicyContextOp]()
	operator.RegisterOp[FormatDraftContextOp]()
	operator.RegisterOp[Sum4TokensOp]()

	operator.RegisterOp[AnalyzeSentimentOp]()
	operator.RegisterOp[ClassifyIntentOp]()
	operator.RegisterOp[CheckPolicyOp]()
	operator.RegisterOp[DraftResponseOp]()
}

// -------------------------------------------------------------------
// SPARSI WORKFLOW
// -------------------------------------------------------------------

func buildSparsiGraph() *graph.Graph {
	g, err := graph.NewBuilder("advanced_triage").
		Vertex("input").Op("body_const").
		Output("Result", "raw_utterance").

		// 1. Fetch Context
		Vertex("fetch_user_context").Op("FetchUserContextOp").
		Input("Utterance", "raw_utterance").
		Output("Result", "user_profile").

		// 2. Analyze Sentiment
		Vertex("analyze_sentiment").Op("AnalyzeSentimentOp").
		Params(map[string]string{
			"provider":  "gemini",
			"model":     "gemini-3.1-flash-lite",
			"operation": PROMPTS["sparsi_sentiment"],
		}).
		Input("Input", "raw_utterance").
		Output("Result", "sentiment_json").
		Output("UsageInputTokens", "in2").
		Output("UsageOutputTokens", "out2").

		// Helper: Format Intent Context
		Vertex("format_intent_ctx").Op("FormatIntentContextOp").
		Input("Utterance", "raw_utterance").
		Input("Profile", "user_profile").
		Input("Sentiment", "sentiment_json").
		Output("Result", "intent_ctx").

		// 3. Classify Intent
		Vertex("classify_intent").Op("ClassifyIntentOp").
		Params(map[string]string{
			"provider":  "gemini",
			"model":     "gemini-3.1-flash-lite",
			"operation": strings.ReplaceAll(PROMPTS["sparsi_intent"], "{INTENT_LIST_STR}", INTENT_LIST_STR),
		}).
		Input("Input", "intent_ctx").
		Output("Result", "intent_json").
		Output("UsageInputTokens", "in3").
		Output("UsageOutputTokens", "out3").

		// Helper: Format Policy Context
		Vertex("format_policy_ctx").Op("FormatPolicyContextOp").
		Input("Intent", "intent_json").
		Input("Sentiment", "sentiment_json").
		Output("Result", "policy_ctx").

		// 4. Check Policy
		Vertex("check_policy").Op("CheckPolicyOp").
		Params(map[string]string{
			"provider":  "gemini",
			"model":     "gemini-3.1-flash-lite",
			"operation": PROMPTS["sparsi_policy"],
		}).
		Input("Input", "policy_ctx").
		Output("Result", "policy_json").
		Output("UsageInputTokens", "in4").
		Output("UsageOutputTokens", "out4").

		// Helper: Format Draft Context
		Vertex("format_draft_ctx").Op("FormatDraftContextOp").
		Input("Utterance", "raw_utterance").
		Input("Intent", "intent_json").
		Input("Policy", "policy_json").
		Input("Profile", "user_profile").
		Output("Result", "draft_ctx").

		// 5. Draft Response
		Vertex("draft_response").Op("DraftResponseOp").
		Params(map[string]string{
			"provider":  "gemini",
			"model":     "gemini-3.1-flash-lite",
			"operation": PROMPTS["sparsi_draft"],
		}).
		Input("Input", "draft_ctx").
		Output("Result", "draft_json").
		Output("UsageInputTokens", "in5").
		Output("UsageOutputTokens", "out5").

		// Helper: Combine Results
		Vertex("combine_results").Op("SendEmailOp").
		Input("IntentResult", "intent_json").
		Input("PolicyResult", "policy_json").
		Input("DraftResult", "draft_json").
		Input("UserProfile", "user_profile").
		Output("Result", "final_result").

		// Helper: Sum Tokens
		Vertex("sum_tokens").Op("Sum4TokensOp").
		Input("In2", "in2").Input("Out2", "out2").
		Input("In3", "in3").Input("Out3", "out3").
		Input("In4", "in4").Input("Out4", "out4").
		Input("In5", "in5").Input("Out5", "out5").
		Output("TotalIn", "in_toks").Output("TotalOut", "out_toks").
		Build()

	if err != nil {
		log.Fatalf("failed to build graph: %v", err)
	}
	return g
}

func runSparsi(pool *ants.Pool, g *graph.Graph, utterance string) (map[string]any, int64, error) {
	ctx := context.WithValue(context.Background(), ticketUtteranceKey{}, utterance)

	eng, err := dagor.NewEngine(g, pool)
	if err != nil {
		return nil, 0, err
	}

	if err := eng.Run(ctx); err != nil {
		return nil, 0, err
	}

	var finalResult map[string]any
	if frRaw, ok := eng.GetOutput("final_result"); ok {
		if v, ok := frRaw.(*map[string]any); ok && v != nil {
			finalResult = *v
		}
	}

	var inToks, outToks int64
	if inRaw, ok := eng.GetOutput("in_toks"); ok {
		if v, ok := inRaw.(*int64); ok && v != nil {
			inToks = *v
		}
	}
	if outRaw, ok := eng.GetOutput("out_toks"); ok {
		if v, ok := outRaw.(*int64); ok && v != nil {
			outToks = *v
		}
	}

	return finalResult, inToks + outToks, nil
}

// -------------------------------------------------------------------
// LANGCHAIN AGENT (Multi-Step Baseline)
// -------------------------------------------------------------------

func runReActAgent(ctx context.Context, client *genai.Client, utterance string) (map[string]any, map[string]any, int64, error) {
	var totalTokens int64
	var lcProfile map[string]any

	tools := []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "fetch_user_context",
					Description: "Fetch the complex JSON user profile for the customer using their utterance.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"utterance": {Type: genai.TypeString, Description: "The raw utterance"},
						},
						Required: []string{"utterance"},
					},
				},
				{
					Name:        "send_email",
					Description: "Send the drafted email to the customer. You MUST pass the exact user_profile dictionary you fetched.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"body":         {Type: genai.TypeString, Description: "The drafted email"},
							"user_profile": {Type: genai.TypeObject, Description: "The user profile fetched"},
						},
						Required: []string{"body", "user_profile"},
					},
				},
			},
		},
	}

	config := &genai.GenerateContentConfig{
		Temperature: genai.Ptr[float32](0),
		Tools:       tools,
	}

	promptStr := strings.ReplaceAll(PROMPTS["langchain_prompt"], "{utterance}", utterance)
	promptStr = strings.ReplaceAll(promptStr, "{INTENT_LIST_STR}", INTENT_LIST_STR)

	history := []*genai.Content{
		genai.NewContentFromText(promptStr, genai.RoleUser),
	}

	var finalResponse string

	for step := 0; step < 7; step++ {
		res, err := client.Models.GenerateContent(ctx, "gemini-3.1-flash-lite", history, config)
		if err != nil {
			return nil, nil, totalTokens, err
		}

		if res.UsageMetadata != nil {
			totalTokens += int64(res.UsageMetadata.PromptTokenCount + res.UsageMetadata.CandidatesTokenCount)
		}

		if len(res.Candidates) == 0 {
			break
		}

		cand := res.Candidates[0]
		if cand.Content != nil {
			history = append(history, cand.Content)
		}

		var functionCalls []*genai.FunctionCall
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				if part.FunctionCall != nil {
					functionCalls = append(functionCalls, part.FunctionCall)
				}
			}
		}

		if len(functionCalls) > 0 {
			var funcResponses []*genai.Part
			for _, fc := range functionCalls {
				var toolResult string

				switch fc.Name {
				case "fetch_user_context":
					argUtt, _ := fc.Args["utterance"].(string)
					b, _ := json.Marshal(getMockUserProfile(argUtt))
					toolResult = string(b)
				case "send_email":
					argProf, ok := fc.Args["user_profile"].(map[string]any)
					if ok {
						lcProfile = argProf
					}
					toolResult = "Email sent successfully."
				default:
					toolResult = "Unknown function"
				}

				funcRespPart := genai.NewPartFromFunctionResponse(fc.Name, map[string]any{"result": toolResult})
				funcResponses = append(funcResponses, funcRespPart)
			}

			history = append(history, &genai.Content{
				Parts: funcResponses,
				Role:  "tool",
			})
		} else {
			if cand.Content != nil {
				for _, p := range cand.Content.Parts {
					if p.Text != "" {
						finalResponse = p.Text
						break
					}
				}
			}
			break
		}
	}

	// Extract from final response
	predictedIntent := ""
	for _, intent := range AVAILABLE_INTENTS {
		if strings.Contains(finalResponse, intent) {
			predictedIntent = intent
			break
		}
	}

	predictedPolicy := ""
	for _, policy := range []string{"escalate", "standard_process", "reject"} {
		if strings.Contains(finalResponse, policy) {
			predictedPolicy = policy
			break
		}
	}

	predictedEmail := ""
	startIdx := strings.Index(finalResponse, "{")
	endIdx := strings.LastIndex(finalResponse, "}")
	if startIdx != -1 && endIdx != -1 && startIdx < endIdx {
		jsonStr := finalResponse[startIdx : endIdx+1]
		var parsed map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			if e, ok := parsed["draft_email"].(string); ok {
				predictedEmail = e
			}
		}
	} else {
		predictedEmail = finalResponse
	}

	finalResult := map[string]any{
		"intent":        predictedIntent,
		"policy_action": predictedPolicy,
		"draft_email":   predictedEmail,
	}

	return finalResult, lcProfile, totalTokens, nil
}

func callLLM(ctx context.Context, client *genai.Client, prompt string) (string, int64, error) {
	config := &genai.GenerateContentConfig{Temperature: genai.Ptr[float32](0)}
	res, err := client.Models.GenerateContent(ctx, "gemini-3.1-flash-lite", []*genai.Content{genai.NewContentFromText(prompt, genai.RoleUser)}, config)
	if err != nil {
		return "", 0, err
	}

	var toks int64
	if res.UsageMetadata != nil {
		toks = int64(res.UsageMetadata.PromptTokenCount + res.UsageMetadata.CandidatesTokenCount)
	}

	return res.Text(), toks, nil
}

// -------------------------------------------------------------------
// BENCHMARK RUNNER
// -------------------------------------------------------------------

type HFResponse struct {
	Rows []struct {
		Row struct {
			Instruction string `json:"instruction"`
			Intent      string `json:"intent"`
		} `json:"row"`
	} `json:"rows"`
}

func fetchDataset(samples int) ([]map[string]string, error) {
	var batch []map[string]string
	offset := 0
	for len(batch) < samples {
		length := 100
		if samples-len(batch) < 100 {
			length = samples - len(batch)
		}
		url := fmt.Sprintf("https://datasets-server.huggingface.co/rows?dataset=bitext%%2FBitext-customer-support-llm-chatbot-training-dataset&config=default&split=train&offset=%d&length=%d", offset, length)
		resp, err := http.Get(url)
		if err != nil {
			return nil, err
		}
		var data HFResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()
		for _, r := range data.Rows {
			batch = append(batch, map[string]string{
				"utterance":   r.Row.Instruction,
				"true_intent": r.Row.Intent,
			})
		}
		offset += 100
	}
	return batch, nil
}

type Results struct {
	Correct   int64
	TotalTime float64
	Failures  int64
	WallTime  float64
	Tokens    int64
}

// Map comparison helper
func isMapEqual(a, b map[string]any) bool {
	aBytes, _ := json.Marshal(a)
	bBytes, _ := json.Marshal(b)
	return string(aBytes) == string(bBytes)
}

func main() {
	samples := flag.Int("samples", 10, "Number of samples to process")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	fmt.Printf("Loading %d samples from bitext/Bitext-customer-support-llm-chatbot-training-dataset...\n", *samples)
	testBatch, err := fetchDataset(*samples)
	if err != nil {
		log.Fatalf("Failed to fetch dataset: %v", err)
	}

	fmt.Println("Initializing systems...")
	sparsiGraph := buildSparsiGraph()

	ctx := context.Background()
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	var genaiClient *genai.Client
	if apiKey != "" {
		genaiClient, _ = genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	} else {
		genaiClient, _ = genai.NewClient(ctx, nil)
	}

	pool, err := ants.NewPool(1000)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	defer pool.Release()

	sparsiRes := Results{}
	lcRes := Results{}

	// SPARSI
	fmt.Println("\n--- Running Sparsi Benchmark ---")
	sparsiWallStart := time.Now()
	var wg sync.WaitGroup
	var sparsiMu sync.Mutex

	sparsiSem := make(chan struct{}, 20)

	for _, item := range testBatch {
		wg.Add(1)
		go func(item map[string]string) {
			defer wg.Done()
			sparsiSem <- struct{}{}
			defer func() { <-sparsiSem }()

			start := time.Now()
			finalResult, toks, err := runSparsi(pool, sparsiGraph, item["utterance"])
			elapsed := time.Since(start).Seconds()

			sparsiMu.Lock()
			defer sparsiMu.Unlock()
			sparsiRes.TotalTime += elapsed
			if err != nil {
				fmt.Println("Sparsi err:", err)
				sparsiRes.Failures++
			} else {
				sparsiRes.Tokens += toks

				predictedIntent, _ := finalResult["intent"].(string)
				predictedPolicy, _ := finalResult["policy_action"].(string)
				predictedEmail, _ := finalResult["draft_email"].(string)
				predictedProfile, _ := finalResult["user_profile"].(map[string]any)

				trueIntent := item["true_intent"]
				truePolicy := getTruePolicy(trueIntent)
				trueProfile := getMockUserProfile(item["utterance"])

				intentCorrect := predictedIntent == trueIntent
				policyCorrect := predictedPolicy == truePolicy
				profileCorrect := isMapEqual(predictedProfile, trueProfile)

				meta, _ := trueProfile["metadata"].(map[string]any)
				expectedToken, _ := meta["security_token"].(string)

				responseCorrect := evaluateResponse(ctx, genaiClient, item["utterance"], trueIntent, truePolicy, predictedEmail, expectedToken)

				pipelineCorrect := intentCorrect && policyCorrect && responseCorrect && profileCorrect
				if pipelineCorrect {
					sparsiRes.Correct++
				}

				fmt.Printf("Sparsi [Intent: %v] [Policy: %v] [Profile: %v] [Response: %v]\n", intentCorrect, policyCorrect, profileCorrect, responseCorrect)
			}
		}(item)
	}
	wg.Wait()
	sparsiRes.WallTime = time.Since(sparsiWallStart).Seconds()

	// LANGCHAIN
	fmt.Println("\n--- Running LangChain Benchmark ---")
	lcWallStart := time.Now()
	var lcWg sync.WaitGroup
	var lcMu sync.Mutex

	lcSem := make(chan struct{}, 20)

	for _, item := range testBatch {
		lcWg.Add(1)
		go func(item map[string]string) {
			defer lcWg.Done()
			lcSem <- struct{}{}
			defer func() { <-lcSem }()

			start := time.Now()
			finalResult, lcProfile, toks, err := runReActAgent(ctx, genaiClient, item["utterance"])
			elapsed := time.Since(start).Seconds()

			lcMu.Lock()
			defer lcMu.Unlock()
			lcRes.TotalTime += elapsed
			if err != nil {
				lcRes.Failures++
			} else {
				lcRes.Tokens += toks

				predictedIntent, _ := finalResult["intent"].(string)
				predictedPolicy, _ := finalResult["policy_action"].(string)
				predictedEmail, _ := finalResult["draft_email"].(string)

				trueIntent := item["true_intent"]
				truePolicy := getTruePolicy(trueIntent)
				trueProfile := getMockUserProfile(item["utterance"])

				intentCorrect := predictedIntent == trueIntent
				policyCorrect := predictedPolicy == truePolicy
				profileCorrect := isMapEqual(lcProfile, trueProfile)

				meta, _ := trueProfile["metadata"].(map[string]any)
				expectedToken, _ := meta["security_token"].(string)

				responseCorrect := evaluateResponse(ctx, genaiClient, item["utterance"], trueIntent, truePolicy, predictedEmail, expectedToken)

				pipelineCorrect := intentCorrect && policyCorrect && responseCorrect && profileCorrect
				if pipelineCorrect {
					lcRes.Correct++
				}

				fmt.Printf("LC [Intent: %v] [Policy: %v] [Profile: %v] [Response: %v]\n", intentCorrect, policyCorrect, profileCorrect, responseCorrect)
			}
		}(item)
	}
	lcWg.Wait()
	lcRes.WallTime = time.Since(lcWallStart).Seconds()

	// OUTPUT RESULTS
	fmt.Println("\n\n==========================================")
	fmt.Println("             BENCHMARK RESULTS            ")
	fmt.Println("==========================================")
	fmt.Printf("%-30s %-10s %-15s %-15s %-15s %-10s\n", "System", "Accuracy", "Avg Latency(s)", "Wall Time(s)", "Total Tokens", "Failures")
	fmt.Println(strings.Repeat("-", 100))

	sAcc := float64(sparsiRes.Correct) / float64(*samples) * 100
	sLat := sparsiRes.TotalTime / float64(*samples)
	fmt.Printf("%-30s %-10.2f%% %-15.2f %-15.2f %-15d %-10d\n", "Sparsi (Multi-Step DAG)", sAcc, sLat, sparsiRes.WallTime, sparsiRes.Tokens, sparsiRes.Failures)

	lAcc := float64(lcRes.Correct) / float64(*samples) * 100
	lLat := lcRes.TotalTime / float64(*samples)
	fmt.Printf("%-30s %-10.2f%% %-15.2f %-15.2f %-15d %-10d\n", "LangChain (ReAct Agent)", lAcc, lLat, lcRes.WallTime, lcRes.Tokens, lcRes.Failures)
	fmt.Println("==========================================")
}
