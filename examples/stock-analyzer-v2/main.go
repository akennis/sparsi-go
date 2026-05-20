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
	"time"

	sparsi "github.com/akennis/sparsi-go/library"
	"github.com/panjf2000/ants/v2"
	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/graph"
	"github.com/wwz16/dagor/operator"
	builtin "github.com/wwz16/dagor/operator/builtin"
	"github.com/wwz16/dagor/reporter"
)

// ─── Data Types ──────────────────────────────────────────────────────────

type AVRawData struct {
	Overview map[string]any `json:"overview"`
	Quote    map[string]any `json:"quote"`
	Earnings map[string]any `json:"earnings"`
	CashFlow map[string]any `json:"cash_flow"`
	SMA      map[string]any `json:"sma"`
	RSI      map[string]any `json:"rsi"`
}

type MacroData struct {
	InterestRate float64 `json:"interest_rate"`
	Inflation    float64 `json:"inflation"`
	GDP          float64 `json:"gdp"`
}

type NewsData struct {
	Articles []string `json:"articles"`
}

type AnalyzeOut struct {
	Recommendation string `json:"recommendation"`
	Rationale      string `json:"rationale"`
}

func (o *AnalyzeOut) ExpectedFormat() string {
	return `You MUST reply ONLY with a valid JSON object. No other text.
{"recommendation": "Buy/Hold/Sell", "rationale": "bullet points"}`
}

func (o *AnalyzeOut) ParseAIResponse(s string) error {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return json.Unmarshal([]byte(s), o)
}

// ─── Custom Ops ──────────────────────────────────────────────────────────

type FetchMacroOp struct {
	Result MacroData `dag:"output"`
	apiKey string
}

func (op *FetchMacroOp) Setup(p *config.Params) error {
	op.apiKey = p.GetString("fred_api_key", "")
	return nil
}
func (op *FetchMacroOp) Reset() error { return nil }
func (op *FetchMacroOp) Run(_ context.Context) error {
	fetch := func(seriesID string) float64 {
		url := fmt.Sprintf("https://api.stlouisfed.org/fred/series/observations?series_id=%s&api_key=%s&file_type=json&sort_order=desc&limit=1", seriesID, op.apiKey)
		resp, err := http.Get(url)
		if err != nil {
			return 0
		}
		defer resp.Body.Close()
		var data struct {
			Observations []struct {
				Value string `json:"value"`
			} `json:"observations"`
		}
		json.NewDecoder(resp.Body).Decode(&data)
		if len(data.Observations) == 0 {
			return 0
		}
		var val float64
		fmt.Sscanf(data.Observations[0].Value, "%f", &val)
		return val
	}
	op.Result = MacroData{InterestRate: fetch("FEDFUNDS"), Inflation: fetch("CPIAUCSL"), GDP: fetch("GDP")}
	return nil
}
func (op *FetchMacroOp) InputFields() map[string]any  { return map[string]any{} }
func (op *FetchMacroOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *FetchMacroOp) SetInputField(_ string, _ any) error { return nil }
func (op *FetchMacroOp) ResetFields()                { op.Result = MacroData{} }

type FetchNewsOp struct {
	Ticker *string  `dag:"input"`
	Result NewsData `dag:"output"`
	apiKey string
}

func (op *FetchNewsOp) Setup(p *config.Params) error {
	op.apiKey = p.GetString("news_api_key", "")
	return nil
}
func (op *FetchNewsOp) Reset() error { return nil }
func (op *FetchNewsOp) Run(_ context.Context) error {
	if op.Ticker == nil {
		return nil
	}
	url := fmt.Sprintf("https://newsapi.org/v2/everything?q=%s&apiKey=%s&pageSize=5", *op.Ticker, op.apiKey)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var data struct {
		Articles []struct{ Title, Description string } `json:"articles"`
	}
	json.NewDecoder(resp.Body).Decode(&data)
	for _, a := range data.Articles {
		op.Result.Articles = append(op.Result.Articles, fmt.Sprintf("%s: %s", a.Title, a.Description))
	}
	return nil
}
func (op *FetchNewsOp) InputFields() map[string]any  { return map[string]any{"Ticker": &op.Ticker} }
func (op *FetchNewsOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *FetchNewsOp) SetInputField(f string, v any) error {
	if f == "Ticker" {
		op.Ticker = v.(*string)
	}
	return nil
}
func (op *FetchNewsOp) ResetFields() { op.Ticker = nil; op.Result = NewsData{} }

type AVFetchOp struct {
	Ticker *string   `dag:"input"`
	Result AVRawData `dag:"output"`
	apiKey string
}

func (op *AVFetchOp) Setup(p *config.Params) error {
	op.apiKey = p.GetString("api_key", "")
	return nil
}
func (op *AVFetchOp) Reset() error { return nil }
func (op *AVFetchOp) Run(_ context.Context) error {
	if op.Ticker == nil {
		return nil
	}
	ticker := *op.Ticker
	call := func(fn string) map[string]any {
		url := fmt.Sprintf("https://www.alphavantage.co/query?function=%s&symbol=%s&apikey=%s", fn, ticker, op.apiKey)
		resp, err := http.Get(url)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		var res map[string]any
		json.NewDecoder(resp.Body).Decode(&res)
		return res
	}
	op.Result.Overview = call("OVERVIEW")
	time.Sleep(time.Second)
	op.Result.Quote = call("GLOBAL_QUOTE")
	time.Sleep(time.Second)
	op.Result.Earnings = call("EARNINGS")
	time.Sleep(time.Second)
	op.Result.CashFlow = call("CASH_FLOW")
	time.Sleep(time.Second)

	callTech := func(fn string, period int) map[string]any {
		url := fmt.Sprintf("https://www.alphavantage.co/query?function=%s&symbol=%s&interval=daily&time_period=%d&series_type=close&apikey=%s", fn, ticker, period, op.apiKey)
		resp, err := http.Get(url)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		var res map[string]any
		json.NewDecoder(resp.Body).Decode(&res)
		return res
	}
	op.Result.SMA = callTech("SMA", 50)
	time.Sleep(time.Second)
	op.Result.RSI = callTech("RSI", 14)
	return nil
}
func (op *AVFetchOp) InputFields() map[string]any  { return map[string]any{"Ticker": &op.Ticker} }
func (op *AVFetchOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *AVFetchOp) SetInputField(f string, v any) error {
	if f == "Ticker" {
		op.Ticker = v.(*string)
	}
	return nil
}
func (op *AVFetchOp) ResetFields() { op.Ticker = nil; op.Result = AVRawData{} }

type FormatStockContextOp struct {
	Ticker    *string    `dag:"input"`
	AVData    *AVRawData `dag:"input"`
	MacroData *MacroData `dag:"input"`
	NewsData  *NewsData  `dag:"input"`
	Context   string     `dag:"output"`
}

func (op *FormatStockContextOp) Setup(_ *config.Params) error { return nil }
func (op *FormatStockContextOp) Reset() error                 { return nil }
func (op *FormatStockContextOp) Run(_ context.Context) error {
	if op.Ticker == nil {
		return nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Stock Analysis for %s\n\n", *op.Ticker)
	if op.AVData != nil {
		fmt.Fprintln(&sb, "--- Financials & Technicals ---")
		if len(op.AVData.Overview) > 0 {
			fmt.Fprintf(&sb, "Market Cap: %v, PE: %v, EPS: %v\n", op.AVData.Overview["MarketCapitalization"], op.AVData.Overview["PERatio"], op.AVData.Overview["EPS"])
		}
		if len(op.AVData.Quote) > 0 {
			fmt.Fprintf(&sb, "Price: %v, Change: %v\n", op.AVData.Quote["05. price"], op.AVData.Quote["10. change percent"])
		}
		if len(op.AVData.SMA) > 0 {
			fmt.Fprintf(&sb, "SMA (50): %v\n", op.AVData.SMA["SMA"])
		}
		if len(op.AVData.RSI) > 0 {
			fmt.Fprintf(&sb, "RSI: %v\n", op.AVData.RSI["RSI"])
		}
	}
	if op.MacroData != nil {
		fmt.Fprintf(&sb, "\n--- Macro Indicators ---\nFed Funds Rate: %.2f%%, CPI: %.2f, GDP: %.2f\n", op.MacroData.InterestRate, op.MacroData.Inflation, op.MacroData.GDP)
	}
	if op.NewsData != nil {
		fmt.Fprintln(&sb, "\n--- Recent News ---")
		for _, a := range op.NewsData.Articles {
			fmt.Fprintf(&sb, "- %s\n", a)
		}
	}
	op.Context = sb.String()
	return nil
}
func (op *FormatStockContextOp) InputFields() map[string]any {
	return map[string]any{"Ticker": &op.Ticker, "AVData": &op.AVData, "MacroData": &op.MacroData, "NewsData": &op.NewsData}
}
func (op *FormatStockContextOp) OutputFields() map[string]any { return map[string]any{"Context": &op.Context} }
func (op *FormatStockContextOp) SetInputField(f string, v any) error {
	switch f {
	case "Ticker":
		op.Ticker = v.(*string)
	case "AVData":
		op.AVData = v.(*AVRawData)
	case "MacroData":
		op.MacroData = v.(*MacroData)
	case "NewsData":
		op.NewsData = v.(*NewsData)
	}
	return nil
}
func (op *FormatStockContextOp) ResetFields() {
	op.Ticker = nil
	op.AVData = nil
	op.MacroData = nil
	op.NewsData = nil
	op.Context = ""
}

type AnalyzeOp struct{ sparsi.AIComputeOp[string, AnalyzeOut] }

func (op *AnalyzeOp) Setup(p *config.Params) error { return op.AIComputeOp.Setup(p) }
func (op *AnalyzeOp) Reset() error                 { return op.AIComputeOp.Reset() }
func (op *AnalyzeOp) Run(ctx context.Context) error { return op.AIComputeOp.Run(ctx) }
func (op *AnalyzeOp) InputFields() map[string]any { return map[string]any{"Input": &op.Input} }
func (op *AnalyzeOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *AnalyzeOp) SetInputField(f string, v any) error {
	if f == "Input" {
		op.Input = v.(*string)
	}
	return nil
}
func (op *AnalyzeOp) ResetFields() { op.Input = nil; op.Result = AnalyzeOut{} }

type tickerKey string

const tickerInputKey tickerKey = "ticker"

func init() {
	operator.RegisterOpFactory("ticker_input", builtin.ContextValFactory[string](tickerInputKey))
	operator.RegisterOpFactory("AVFetchOp", func() operator.IOperator { return &AVFetchOp{} })
	operator.RegisterOpFactory("FetchMacroOp", func() operator.IOperator { return &FetchMacroOp{} })
	operator.RegisterOpFactory("FetchNewsOp", func() operator.IOperator { return &FetchNewsOp{} })
	operator.RegisterOpFactory("FormatStockContextOp", func() operator.IOperator { return &FormatStockContextOp{} })
	operator.RegisterOpFactory("AnalyzeOp", func() operator.IOperator { return &AnalyzeOp{} })
}

func buildGraph(avAPIKey, fredAPIKey, newsAPIKey string) (*graph.Graph, error) {
	return graph.NewBuilder("stock_analyzer").
		Vertex("ticker").Op("ticker_input").Output("Result", "ticker_wire").
		Vertex("av_fetch").Op("AVFetchOp").Params(map[string]string{"api_key": avAPIKey}).Input("Ticker", "ticker_wire").Output("Result", "av_wire").
		Vertex("macro_fetch").Op("FetchMacroOp").Params(map[string]string{"fred_api_key": fredAPIKey}).Output("Result", "macro_wire").
		Vertex("news_fetch").Op("FetchNewsOp").Params(map[string]string{"news_api_key": newsAPIKey}).Input("Ticker", "ticker_wire").Output("Result", "news_wire").
		Vertex("context_builder").Op("FormatStockContextOp").Input("Ticker", "ticker_wire").Input("AVData", "av_wire").Input("MacroData", "macro_wire").Input("NewsData", "news_wire").Output("Context", "context_wire").
		Vertex("analyze").Op("AnalyzeOp").Params(map[string]string{
		"operation": "Analyze stock. Return recommendation and rationale.",
		"provider":  "gemini", "model": "gemini-3-flash-preview",
	}).Input("Input", "context_wire").Output("Result", "final_result").Build()
}

func main() {
	ticker := flag.String("ticker", "", "ticker")
	verbose := flag.Bool("v", false, "verbose")
	flag.Parse()
	if *ticker == "" {
		log.Fatal("--ticker required")
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	g, err := buildGraph(os.Getenv("ALPHA_VANTAGE_API_KEY"), os.Getenv("FRED_API_KEY"), os.Getenv("NEWS_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	pool, _ := ants.NewPool(10)
	defer pool.Release()
	eng, err := dagor.NewEngine(g, pool, dagor.WithReporter(reporter.New(slog.Default())))
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), tickerInputKey, *ticker)
	if err := eng.Run(ctx); err != nil {
		log.Fatal(err)
	}
	raw, _ := eng.GetOutput("final_result")
	out := raw.(*AnalyzeOut)
	fmt.Printf("\nRecommendation: %s\n\nRationale:\n%s\n", out.Recommendation, out.Rationale)
}
