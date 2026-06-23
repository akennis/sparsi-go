package main

import (
	"context"
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
	"github.com/panjf2000/ants/v2"
	"google.golang.org/genai"
	_ "github.com/akennis/sparsi-go/library"
)

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

// -------------------------------------------------------------------
// CUSTOM OPS
// -------------------------------------------------------------------

type FormatContextOp struct {
	Utterance *string `dag:"input"`
	Context   *map[string]string `dag:"input"`
	Result    string  `dag:"output"`
}

func (op *FormatContextOp) Setup(_ *config.Params) error { return nil }
func (op *FormatContextOp) Reset() error                 { return nil }
func (op *FormatContextOp) Run(_ context.Context) error {
	ctxStr := ""
	if op.Context != nil {
		b, _ := json.Marshal(*op.Context)
		ctxStr = string(b)
	}
	op.Result = fmt.Sprintf("UTTERANCE: %s\nCONTEXT: %s", *op.Utterance, ctxStr)
	return nil
}

func (op *FormatContextOp) InputFields() map[string]any {
	return map[string]any{"Utterance": &op.Utterance, "Context": &op.Context}
}

func (op *FormatContextOp) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}

func (op *FormatContextOp) SetInputField(field string, value any) error {
	switch field {
	case "Utterance": op.Utterance = value.(*string)
	case "Context": op.Context = value.(*map[string]string)
	default: return fmt.Errorf("unknown field")
	}
	return nil
}

func (op *FormatContextOp) ResetFields() {
	op.Utterance = nil
	op.Context = nil
	op.Result = ""
}

type SumTokensOp struct {
	In1 *int64 `dag:"input"`
	Out1 *int64 `dag:"input"`
	In2 *int64 `dag:"input"`
	Out2 *int64 `dag:"input"`
	TotalIn  int64 `dag:"output"`
	TotalOut int64 `dag:"output"`
}

func (op *SumTokensOp) Setup(_ *config.Params) error { return nil }
func (op *SumTokensOp) Reset() error                 { return nil }
func (op *SumTokensOp) Run(_ context.Context) error {
	var in, out int64
	if op.In1 != nil { in += *op.In1 }
	if op.In2 != nil { in += *op.In2 }
	if op.Out1 != nil { out += *op.Out1 }
	if op.Out2 != nil { out += *op.Out2 }
	op.TotalIn = in
	op.TotalOut = out
	return nil
}

func (op *SumTokensOp) InputFields() map[string]any {
	return map[string]any{"In1": &op.In1, "In2": &op.In2, "Out1": &op.Out1, "Out2": &op.Out2}
}

func (op *SumTokensOp) OutputFields() map[string]any {
	return map[string]any{"TotalIn": &op.TotalIn, "TotalOut": &op.TotalOut}
}

func (op *SumTokensOp) SetInputField(field string, value any) error {
	switch field {
	case "In1": op.In1 = value.(*int64)
	case "In2": op.In2 = value.(*int64)
	case "Out1": op.Out1 = value.(*int64)
	case "Out2": op.Out2 = value.(*int64)
	default: return fmt.Errorf("unknown field")
	}
	return nil
}

func (op *SumTokensOp) ResetFields() {
	op.In1 = nil
	op.In2 = nil
	op.Out1 = nil
	op.Out2 = nil
	op.TotalIn = 0
	op.TotalOut = 0
}

type ticketUtteranceKey struct{}

func init() {
	mustReg := func(name string, f func() operator.IOperator) {
		if err := operator.RegisterOpFactory(name, f); err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}
	mustReg("body_const", builtin.ContextValFactory[string](ticketUtteranceKey{}))
	operator.RegisterOp[FormatContextOp]()
	operator.RegisterOp[SumTokensOp]()
}

// -------------------------------------------------------------------
// SPARSI WORKFLOW
// -------------------------------------------------------------------

func buildSparsiGraph() *graph.Graph {
	g, err := graph.NewBuilder("intent_classifier").
		Vertex("input").Op("body_const").
		Output("Result", "raw_utterance").

		// Node 1: Extract Context
		Vertex("extract_ctx").Op("AIExtractMapOp").
		Params(map[string]string{
			"provider": "gemini",
			"model": "gemini-3.1-flash-lite",
			"operation": "1. 'tone': string. 2. 'language': string. 3. 'key_entities': space-separated string. CRITICAL: DO NOT use commas inside any values, as commas are used to separate the key=value pairs.",
		}).
		Input("Input", "raw_utterance").
		Output("Result", "ctx_json").
		Output("UsageInputTokens", "in1").
		Output("UsageOutputTokens", "out1").

		// Helper Node: Format
		Vertex("format").Op("FormatContextOp").
		Input("Utterance", "raw_utterance").
		Input("Context", "ctx_json").
		Output("Result", "formatted_input").

		// Node 2: Classify Intent
		Vertex("classify").Op("AIExtractMapOp").
		Params(map[string]string{
			"provider": "gemini",
			"model": "gemini-3.1-flash-lite",
			"operation": fmt.Sprintf("Using the provided UTTERANCE and CONTEXT, classify the intent. 1. 'intent': EXACTLY one of %s", INTENT_LIST_STR),
		}).
		Input("Input", "formatted_input").
		Output("Result", "llm_json").
		Output("UsageInputTokens", "in2").
		Output("UsageOutputTokens", "out2").

		// Helper Node: Tokens
		Vertex("sum_tokens").Op("SumTokensOp").
		Input("In1", "in1").
		Input("Out1", "out1").
		Input("In2", "in2").
		Input("Out2", "out2").
		Output("TotalIn", "in_toks").
		Output("TotalOut", "out_toks").
		Build()
		
	if err != nil {
		log.Fatalf("failed to build graph: %v", err)
	}
	return g
}

func runSparsi(pool *ants.Pool, g *graph.Graph, utterance string) (string, int64, error) {
	ctx := context.WithValue(context.Background(), ticketUtteranceKey{}, utterance)
	
	eng, err := dagor.NewEngine(g, pool)
	if err != nil {
		return "", 0, err
	}
	
	if err := eng.Run(ctx); err != nil {
		return "", 0, err
	}
	
	var predictedIntent string
	if llmJson, ok := eng.GetOutput("llm_json"); ok {
		if m, ok := llmJson.(*map[string]string); ok && m != nil {
			predictedIntent = (*m)["intent"]
		}
	}
	
	var inToks, outToks int64
	if inRaw, ok := eng.GetOutput("in_toks"); ok {
		if v, ok := inRaw.(*int64); ok && v != nil { inToks = *v }
	}
	if outRaw, ok := eng.GetOutput("out_toks"); ok {
		if v, ok := outRaw.(*int64); ok && v != nil { outToks = *v }
	}
	
	return predictedIntent, inToks + outToks, nil
}

// -------------------------------------------------------------------
// LANGCHAIN AGENT (Multi-Step Baseline)
// -------------------------------------------------------------------

func runReActAgent(ctx context.Context, client *genai.Client, utterance string) (string, int64, error) {
	var totalTokens int64
	
	// Tool definitions
	tools := []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "extract_context",
					Description: "Always use this tool FIRST to extract the tone, language, and key entities of the utterance.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"utterance": {Type: genai.TypeString, Description: "The raw utterance to analyze"},
						},
						Required: []string{"utterance"},
					},
				},
				{
					Name:        "classify_intent",
					Description: "Use this tool SECOND. Provide the original utterance and the context you extracted to classify the intent.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"utterance": {Type: genai.TypeString, Description: "The raw utterance"},
							"context":   {Type: genai.TypeString, Description: "The extracted context from extract_context tool"},
						},
						Required: []string{"utterance", "context"},
					},
				},
			},
		},
	}

	config := &genai.GenerateContentConfig{
		Temperature: genai.Ptr[float32](0),
		Tools:       tools,
	}

	prompt := fmt.Sprintf("You must FIRST extract the context, and THEN classify the intent. Utterance: %s", utterance)
	history := []*genai.Content{
		genai.NewContentFromText(prompt, genai.RoleUser),
	}

	var predictedIntent string

	// Agent loop (max 5 steps)
	for step := 0; step < 5; step++ {
		res, err := client.Models.GenerateContent(ctx, "gemini-3.1-flash-lite", history, config)
		if err != nil {
			return predictedIntent, totalTokens, err
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
				
				if fc.Name == "extract_context" {
					argUtt, _ := fc.Args["utterance"].(string)
					exPrompt := "Analyze tone, language, and key entities of: " + argUtt
					exRes, exToks, err := callLLM(ctx, client, exPrompt)
					if err == nil {
						totalTokens += exToks
						toolResult = exRes
					} else {
						toolResult = "Error extracting context: " + err.Error()
					}
				} else if fc.Name == "classify_intent" {
					argUtt, _ := fc.Args["utterance"].(string)
					argCtx, _ := fc.Args["context"].(string)
					clsPrompt := fmt.Sprintf("Given UTTERANCE: '%s' and CONTEXT: '%s', classify intent into EXACTLY one of: %s", argUtt, argCtx, INTENT_LIST_STR)
					clsRes, clsToks, err := callLLM(ctx, client, clsPrompt)
					if err == nil {
						totalTokens += clsToks
						toolResult = clsRes
						predictedIntent = clsRes // Track for final output
					} else {
						toolResult = "Error classifying intent: " + err.Error()
					}
				} else {
					toolResult = "Unknown function"
				}
				
				funcRespPart := genai.NewPartFromFunctionResponse(fc.Name, map[string]any{"result": toolResult})
				funcResponses = append(funcResponses, funcRespPart)
			}
			
			history = append(history, &genai.Content{
				Parts: funcResponses,
				Role:  "tool", // using generic tool role for function responses
			})
		} else {
			// No more function calls, extract text from final response if needed
			if predictedIntent == "" && cand.Content != nil {
				for _, p := range cand.Content.Parts {
					if p.Text != "" {
						predictedIntent = p.Text
						break
					}
				}
			}
			break
		}
	}
	
	pred := predictedIntent
	pred = strings.ReplaceAll(pred, "**", "")
	pred = strings.ReplaceAll(pred, "`", "")
	for _, intent := range AVAILABLE_INTENTS {
		if strings.Contains(pred, intent) {
			return intent, totalTokens, nil
		}
	}
	
	return pred, totalTokens, nil
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

func main() {
	samples := flag.Int("samples", 500, "Number of samples to process")
	flag.Parse()
	
	// Disable noisy logs unless there's an error
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

	pool, err := ants.NewPool(60)
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
	
	sparsiSem := make(chan struct{}, 60)
	
	for _, item := range testBatch {
		wg.Add(1)
		go func(item map[string]string) {
			defer wg.Done()
			sparsiSem <- struct{}{}
			defer func() { <-sparsiSem }()
			
			start := time.Now()
			pred, toks, err := runSparsi(pool, sparsiGraph, item["utterance"])
			elapsed := time.Since(start).Seconds()
			
			sparsiMu.Lock()
			defer sparsiMu.Unlock()
			sparsiRes.TotalTime += elapsed
			if err != nil {
				fmt.Println("Sparsi err:", err)
				sparsiRes.Failures++
			} else {
				sparsiRes.Tokens += toks
				if pred == item["true_intent"] {
					sparsiRes.Correct++
				}
			}
		}(item)
	}
	wg.Wait()
	sparsiRes.WallTime = time.Since(sparsiWallStart).Seconds()

	// LANGCHAIN (BASELINE)
	fmt.Println("\n--- Running LangChain (Baseline) Benchmark ---")
	lcWallStart := time.Now()
	var lcWg sync.WaitGroup
	var lcMu sync.Mutex
	
	sem := make(chan struct{}, 60)
	
	for _, item := range testBatch {
		lcWg.Add(1)
		go func(item map[string]string) {
			defer lcWg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			
			start := time.Now()
			pred, toks, err := runReActAgent(context.Background(), genaiClient, item["utterance"])
			elapsed := time.Since(start).Seconds()
			
			lcMu.Lock()
			defer lcMu.Unlock()
			lcRes.TotalTime += elapsed
			if err != nil {
				lcRes.Failures++
			} else {
				lcRes.Tokens += toks
				if pred == item["true_intent"] {
					lcRes.Correct++
				}
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
	fmt.Printf("%-30s %-10.2f%% %-15.2f %-15.2f %-15d %-10d\n", "Baseline (ReAct Equiv)", lAcc, lLat, lcRes.WallTime, lcRes.Tokens, lcRes.Failures)
	fmt.Println("==========================================")
}
