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

	"github.com/akennis/dagor"
	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/graph"
	"github.com/akennis/dagor/operator"
	"github.com/akennis/dagor/operator/builtin"
	"github.com/akennis/dagor/reporter"
	"github.com/akennis/sparsi-go/library"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/panjf2000/ants/v2"
)

// ─── Types and Keys ────────────────────────────────────────────────────────

type ctxKey string

const tickerKey ctxKey = "ticker"

type UserInput struct {
	Ticker string `json:"ticker" jsonschema:"the stock ticker symbol to analyze, e.g. AAPL; required"`
}

type Result struct {
	Recommendation string `json:"recommendation"`
}

// ─── Custom Ops ────────────────────────────────────────────────────────────

type BuildPolygonUrlsOp struct {
	Ticker *string `dag:"input"`
	Key    *string `dag:"input"`
	DetailsUrl string `dag:"output"`
	SnapshotUrl string `dag:"output"`
	FinancialsUrl string `dag:"output"`
}

func (op *BuildPolygonUrlsOp) Setup(_ *config.Params) error { return nil }
func (op *BuildPolygonUrlsOp) Reset() error                 { return nil }
func (op *BuildPolygonUrlsOp) Run(_ context.Context) error {
	op.DetailsUrl = fmt.Sprintf("https://api.polygon.io/v3/reference/tickers/%s?apiKey=%s", *op.Ticker, *op.Key)
	op.SnapshotUrl = fmt.Sprintf("https://api.polygon.io/v2/snapshot/locale/us/markets/stocks/tickers/%s?apiKey=%s", *op.Ticker, *op.Key)
	op.FinancialsUrl = fmt.Sprintf("https://api.polygon.io/vX/reference/financials?ticker=%s&limit=1&apiKey=%s", *op.Ticker, *op.Key)
	return nil
}
func (op *BuildPolygonUrlsOp) InputFields() map[string]any {
	return map[string]any{"Ticker": &op.Ticker, "Key": &op.Key}
}
func (op *BuildPolygonUrlsOp) OutputFields() map[string]any {
	return map[string]any{"DetailsUrl": &op.DetailsUrl, "SnapshotUrl": &op.SnapshotUrl, "FinancialsUrl": &op.FinancialsUrl}
}
func (op *BuildPolygonUrlsOp) SetInputField(f string, v any) error {
	switch f {
	case "Ticker": op.Ticker = v.(*string)
	case "Key": op.Key = v.(*string)
	default: return fmt.Errorf("unknown field %s", f)
	}
	return nil
}
func (op *BuildPolygonUrlsOp) ResetFields() { op.Ticker = nil; op.Key = nil; op.DetailsUrl = ""; op.SnapshotUrl = ""; op.FinancialsUrl = "" }

type BuildNewsUrlOp struct {
	Ticker *string `dag:"input"`
	Key    *string `dag:"input"`
	URL    string  `dag:"output"`
}

func (op *BuildNewsUrlOp) Setup(_ *config.Params) error { return nil }
func (op *BuildNewsUrlOp) Reset() error                 { return nil }
func (op *BuildNewsUrlOp) Run(_ context.Context) error {
	op.URL = fmt.Sprintf("https://newsapi.org/v2/everything?pageSize=5&q=%s&apiKey=%s", *op.Ticker, *op.Key)
	return nil
}
func (op *BuildNewsUrlOp) InputFields() map[string]any {
	return map[string]any{"Ticker": &op.Ticker, "Key": &op.Key}
}
func (op *BuildNewsUrlOp) OutputFields() map[string]any { return map[string]any{"URL": &op.URL} }
func (op *BuildNewsUrlOp) SetInputField(f string, v any) error {
	switch f {
	case "Ticker": op.Ticker = v.(*string)
	case "Key": op.Key = v.(*string)
	default: return fmt.Errorf("unknown field %s", f)
	}
	return nil
}
func (op *BuildNewsUrlOp) ResetFields() { op.Ticker = nil; op.Key = nil; op.URL = "" }

type BuildFredUrlsOp struct {
	Key      *string `dag:"input"`
	GDPUrl   string  `dag:"output"`
	CPIUrl   string  `dag:"output"`
	RatesUrl string  `dag:"output"`
}

func (op *BuildFredUrlsOp) Setup(_ *config.Params) error { return nil }
func (op *BuildFredUrlsOp) Reset() error                 { return nil }
func (op *BuildFredUrlsOp) Run(_ context.Context) error {
	op.GDPUrl = fmt.Sprintf("https://api.stlouisfed.org/fred/series/observations?series_id=GDP&api_key=%s&file_type=json&limit=1&sort_order=desc", *op.Key)
	op.CPIUrl = fmt.Sprintf("https://api.stlouisfed.org/fred/series/observations?series_id=CPIAUCSL&api_key=%s&file_type=json&limit=1&sort_order=desc", *op.Key)
	op.RatesUrl = fmt.Sprintf("https://api.stlouisfed.org/fred/series/observations?series_id=FEDFUNDS&api_key=%s&file_type=json&limit=1&sort_order=desc", *op.Key)
	return nil
}
func (op *BuildFredUrlsOp) InputFields() map[string]any { return map[string]any{"Key": &op.Key} }
func (op *BuildFredUrlsOp) OutputFields() map[string]any {
	return map[string]any{"GDPUrl": &op.GDPUrl, "CPIUrl": &op.CPIUrl, "RatesUrl": &op.RatesUrl}
}
func (op *BuildFredUrlsOp) SetInputField(f string, v any) error {
	if f == "Key" { op.Key = v.(*string); return nil }
	return fmt.Errorf("unknown field %s", f)
}
func (op *BuildFredUrlsOp) ResetFields() { op.Key = nil; op.GDPUrl = ""; op.CPIUrl = ""; op.RatesUrl = "" }

type PolygonFinancialPrunerOp struct {
	Details   *string `dag:"input"`
	Snapshot  *string `dag:"input"`
	Financials *string `dag:"input"`
	Summary   string  `dag:"output"`
}

func (op *PolygonFinancialPrunerOp) Setup(_ *config.Params) error { return nil }
func (op *PolygonFinancialPrunerOp) Reset() error                 { return nil }
func (op *PolygonFinancialPrunerOp) Run(_ context.Context) error {
	var sb strings.Builder
	sb.WriteString("Financial Metrics Summary (via Polygon.io):\n")

	if op.Details != nil {
		var data struct {
			Results struct {
				Name string `json:"name"`
				SicSectorDescription string `json:"sic_sector_description"`
				SicDescription string `json:"sic_description"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(*op.Details), &data); err == nil {
			fmt.Fprintf(&sb, "- Name: %s, Sector: %s, Industry: %s\n", data.Results.Name, data.Results.SicSectorDescription, data.Results.SicDescription)
		}
	}

	if op.Snapshot != nil {
		var data struct {
			Ticker struct {
				LastTrade struct {
					Price float64 `json:"p"`
				} `json:"lastTrade"`
				TodaysChange float64 `json:"todaysChange"`
				TodaysChangePerc float64 `json:"todaysChangePerc"`
				Day struct {
					Volume float64 `json:"v"`
				} `json:"day"`
			} `json:"ticker"`
		}
		if err := json.Unmarshal([]byte(*op.Snapshot), &data); err == nil {
			fmt.Fprintf(&sb, "- Current Price: %v, Volume: %v, Today's Change: %v (%v%%)\n", data.Ticker.LastTrade.Price, data.Ticker.Day.Volume, data.Ticker.TodaysChange, data.Ticker.TodaysChangePerc)
		}
	}

	if op.Financials != nil {
		var data struct {
			Results []struct {
				Financials struct {
					IncomeStatement struct {
						Revenues struct { Value float64 `json:"value"` } `json:"revenues"`
						NetIncome struct { Value float64 `json:"value"` } `json:"net_income_loss"`
					} `json:"income_statement"`
				} `json:"financials"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(*op.Financials), &data); err == nil && len(data.Results) > 0 {
			f := data.Results[0].Financials.IncomeStatement
			fmt.Fprintf(&sb, "- Latest Revenue: %v, Latest Net Income: %v\n", f.Revenues.Value, f.NetIncome.Value)
		}
	}

	op.Summary = sb.String()
	return nil
}
func (op *PolygonFinancialPrunerOp) InputFields() map[string]any {
	return map[string]any{"Details": &op.Details, "Snapshot": &op.Snapshot, "Financials": &op.Financials}
}
func (op *PolygonFinancialPrunerOp) OutputFields() map[string]any { return map[string]any{"Summary": &op.Summary} }
func (op *PolygonFinancialPrunerOp) SetInputField(f string, v any) error {
	switch f {
	case "Details": op.Details = v.(*string)
	case "Snapshot": op.Snapshot = v.(*string)
	case "Financials": op.Financials = v.(*string)
	default: return fmt.Errorf("unknown field %s", f)
	}
	return nil
}
func (op *PolygonFinancialPrunerOp) ResetFields() { op.Details = nil; op.Snapshot = nil; op.Financials = nil; op.Summary = "" }

type NewsPrunerOp struct {
	JSON    *string `dag:"input"`
	Summary string  `dag:"output"`
}

func (op *NewsPrunerOp) Setup(_ *config.Params) error { return nil }
func (op *NewsPrunerOp) Reset() error                 { return nil }
func (op *NewsPrunerOp) Run(_ context.Context) error {
	var data struct {
		Articles []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"articles"`
	}
	if err := json.Unmarshal([]byte(*op.JSON), &data); err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString("Latest News Headlines:\n")
	for i, art := range data.Articles {
		if i >= 5 { break }
		fmt.Fprintf(&sb, "- %s: %s\n", art.Title, art.Description)
	}
	op.Summary = sb.String()
	return nil
}
func (op *NewsPrunerOp) InputFields() map[string]any  { return map[string]any{"JSON": &op.JSON} }
func (op *NewsPrunerOp) OutputFields() map[string]any { return map[string]any{"Summary": &op.Summary} }
func (op *NewsPrunerOp) SetInputField(f string, v any) error {
	if f == "JSON" { op.JSON = v.(*string); return nil }
	return fmt.Errorf("unknown field %s", f)
}
func (op *NewsPrunerOp) ResetFields() { op.JSON = nil; op.Summary = "" }

type FREDMacroPrunerOp struct {
	GDP     *string `dag:"input"`
	CPI     *string `dag:"input"`
	Rates   *string `dag:"input"`
	Summary string  `dag:"output"`
}

func (op *FREDMacroPrunerOp) Setup(_ *config.Params) error { return nil }
func (op *FREDMacroPrunerOp) Reset() error                 { return nil }
func (op *FREDMacroPrunerOp) Run(_ context.Context) error {
	prune := func(name, raw string) string {
		var data struct {
			Observations []struct {
				Value string `json:"value"`
				Date  string `json:"date"`
			} `json:"observations"`
		}
		if err := json.Unmarshal([]byte(raw), &data); err != nil || len(data.Observations) == 0 {
			return name + ": N/A"
		}
		return fmt.Sprintf("%s: %s (as of %s)", name, data.Observations[0].Value, data.Observations[0].Date)
	}

	op.Summary = fmt.Sprintf("Macroeconomic Context:\n- %s\n- %s\n- %s",
		prune("GDP", *op.GDP), prune("Inflation (CPI)", *op.CPI), prune("Interest Rate (Fed Funds)", *op.Rates))
	return nil
}
func (op *FREDMacroPrunerOp) InputFields() map[string]any {
	return map[string]any{"GDP": &op.GDP, "CPI": &op.CPI, "Rates": &op.Rates}
}
func (op *FREDMacroPrunerOp) OutputFields() map[string]any { return map[string]any{"Summary": &op.Summary} }
func (op *FREDMacroPrunerOp) SetInputField(f string, v any) error {
	switch f {
	case "GDP": op.GDP = v.(*string)
	case "CPI": op.CPI = v.(*string)
	case "Rates": op.Rates = v.(*string)
	default: return fmt.Errorf("unknown field %s", f)
	}
	return nil
}
func (op *FREDMacroPrunerOp) ResetFields() { op.GDP = nil; op.CPI = nil; op.Rates = nil; op.Summary = "" }

type StockPromptBuilderOp struct {
	Ticker     *string `dag:"input"`
	Financials *string `dag:"input"`
	News       *string `dag:"input"`
	Macro      *string `dag:"input"`
	Prompt     string  `dag:"output"`
}

func (op *StockPromptBuilderOp) Setup(_ *config.Params) error { return nil }
func (op *StockPromptBuilderOp) Reset() error                 { return nil }
func (op *StockPromptBuilderOp) Run(_ context.Context) error {
	op.Prompt = fmt.Sprintf(`Analyze the following data for stock ticker %s and provide a Buy/Hold/Sell recommendation.

%s

%s

%s

The response must include a concise Buy/Hold/Sell verdict followed by a multi-factor rationale covering growth, valuation, debt, technicals, and macro factors.`,
		*op.Ticker, *op.Financials, *op.News, *op.Macro)
	return nil
}
func (op *StockPromptBuilderOp) InputFields() map[string]any {
	return map[string]any{"Ticker": &op.Ticker, "Financials": &op.Financials, "News": &op.News, "Macro": &op.Macro}
}
func (op *StockPromptBuilderOp) OutputFields() map[string]any { return map[string]any{"Prompt": &op.Prompt} }
func (op *StockPromptBuilderOp) SetInputField(f string, v any) error {
	switch f {
	case "Ticker": op.Ticker = v.(*string)
	case "Financials": op.Financials = v.(*string)
	case "News": op.News = v.(*string)
	case "Macro": op.Macro = v.(*string)
	default: return fmt.Errorf("unknown field %s", f)
	}
	return nil
}
func (op *StockPromptBuilderOp) ResetFields() {
	op.Ticker = nil; op.Financials = nil; op.News = nil; op.Macro = nil; op.Prompt = ""
}

type RecommendationOp struct {
	library.AIComputeOp[string, string]
}
func (op *RecommendationOp) InputFields() map[string]any { return map[string]any{"Input": &op.Input} }
func (op *RecommendationOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *RecommendationOp) SetInputField(f string, v any) error {
	if f == "Input" { op.Input = v.(*string); return nil }
	return fmt.Errorf("unknown field %s", f)
}
func (op *RecommendationOp) ResetFields() { op.Input = nil; op.Result = "" }

func init() {
	operator.RegisterOpFactory("ticker_src", builtin.ContextValFactory[string](tickerKey))
	library.RegisterConst("news_api_key_name", "NEWSAPI_API_KEY")
	library.RegisterConst("fred_api_key_name", "FRED_API_KEY")
	library.RegisterConst("polygon_api_key_name", "POLYGON_API_KEY")
	operator.RegisterOp[BuildPolygonUrlsOp]()
	operator.RegisterOp[BuildNewsUrlOp]()
	operator.RegisterOp[BuildFredUrlsOp]()
	operator.RegisterOp[PolygonFinancialPrunerOp]()
	operator.RegisterOp[NewsPrunerOp]()
	operator.RegisterOp[FREDMacroPrunerOp]()
	operator.RegisterOp[StockPromptBuilderOp]()
	operator.RegisterOp[RecommendationOp]()
}

// ─── Graph Builder ─────────────────────────────────────────────────────────

func buildGraph() (*graph.Graph, error) {
	b := graph.NewBuilder("stock_analyzer")

	b.Vertex("ticker_src").Op("ticker_src").Output("Result", "ticker")

	// Polygon Pipeline
	b.Vertex("poly_key_name").Op("polygon_api_key_name").Output("Result", "p_key_name")
	b.Vertex("poly_key").Op("EnvOp").Input("Name", "p_key_name").Output("Value", "p_key")
	b.Vertex("build_poly_urls").Op("BuildPolygonUrlsOp").
		Input("Ticker", "ticker").Input("Key", "p_key").
		Output("DetailsUrl", "p_details_url").Output("SnapshotUrl", "p_snapshot_url").Output("FinancialsUrl", "p_financials_url")
	b.Vertex("fetch_poly_details").Op("HTTPGetOp").Input("URL", "p_details_url").Output("Body", "p_details_json")
	b.Vertex("fetch_poly_snapshot").Op("HTTPGetOp").Input("URL", "p_snapshot_url").Output("Body", "p_snapshot_json")
	b.Vertex("fetch_poly_financials").Op("HTTPGetOp").Input("URL", "p_financials_url").Output("Body", "p_financials_json")

	// NewsAPI Pipeline
	b.Vertex("news_key_name").Op("news_api_key_name").Output("Result", "n_key_name")
	b.Vertex("news_key").Op("EnvOp").Input("Name", "n_key_name").Output("Value", "n_key")
	b.Vertex("build_news_url").Op("BuildNewsUrlOp").
		Input("Ticker", "ticker").Input("Key", "n_key").Output("URL", "news_url")
	b.Vertex("fetch_news").Op("HTTPGetOp").Input("URL", "news_url").Output("Body", "news_json")

	// FRED Pipeline
	b.Vertex("fred_key_name").Op("fred_api_key_name").Output("Result", "f_key_name")
	b.Vertex("fred_key").Op("EnvOp").Input("Name", "f_key_name").Output("Value", "f_key")
	b.Vertex("build_fred_urls").Op("BuildFredUrlsOp").
		Input("Key", "f_key").
		Output("GDPUrl", "gdp_url").Output("CPIUrl", "cpi_url").Output("RatesUrl", "rates_url")
	b.Vertex("fetch_gdp").Op("HTTPGetOp").Input("URL", "gdp_url").Output("Body", "gdp_json")
	b.Vertex("fetch_cpi").Op("HTTPGetOp").Input("URL", "cpi_url").Output("Body", "cpi_json")
	b.Vertex("fetch_rates").Op("HTTPGetOp").Input("URL", "rates_url").Output("Body", "rates_json")

	// Pruning
	b.Vertex("poly_pruner").Op("PolygonFinancialPrunerOp").
		Input("Details", "p_details_json").
		Input("Snapshot", "p_snapshot_json").
		Input("Financials", "p_financials_json").
		Output("Summary", "fin_summary")

	b.Vertex("news_pruner").Op("NewsPrunerOp").
		Input("JSON", "news_json").Output("Summary", "news_summary")

	b.Vertex("fred_pruner").Op("FREDMacroPrunerOp").
		Input("GDP", "gdp_json").Input("CPI", "cpi_json").Input("Rates", "rates_json").
		Output("Summary", "macro_summary")

	// Prompt & Recommendation
	b.Vertex("final_prompt").Op("StockPromptBuilderOp").
		Input("Ticker", "ticker").
		Input("Financials", "fin_summary").
		Input("News", "news_summary").
		Input("Macro", "macro_summary").
		Output("Prompt", "stock_prompt")

	b.Vertex("recommend").Op("RecommendationOp").
		Params(map[string]string{
			"provider":  "gemini",
			"model":     "gemini-3.5-flash",
			"operation": "Analyze the stock and provide a Buy/Hold/Sell recommendation with rationale.",
		}).
		Input("Input", "stock_prompt").Output("Result", "recommendation")

	return b.Build()
}

func runWorkflow(ctx context.Context, pool *ants.Pool, in UserInput) (Result, error) {
	g, err := buildGraph()
	if err != nil { return Result{}, fmt.Errorf("build graph: %w", err) }
	eng, err := dagor.NewEngine(g, pool, dagor.WithReporter(reporter.New(slog.Default())))
	if err != nil { return Result{}, fmt.Errorf("create engine: %w", err) }
	ctx = context.WithValue(ctx, tickerKey, in.Ticker)
	if err := eng.Run(ctx); err != nil { return Result{}, fmt.Errorf("run graph: %w", err) }
	var out Result
	if raw, ok := eng.GetOutput("recommendation"); ok {
		if p, ok := raw.(*string); ok && p != nil { out.Recommendation = *p }
	}
	return out, nil
}

func runMCPServer(pool *ants.Pool) {
	server := mcp.NewServer(&mcp.Implementation{Name: "stock_analyzer", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: "analyze_stock",
		Description: "Provides a Buy/Hold/Sell recommendation based on financial, технический, news, and macroeconomic data.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in UserInput) (*mcp.CallToolResult, Result, error) {
		if in.Ticker == "" { return nil, Result{}, fmt.Errorf("ticker is required") }
		res, err := runWorkflow(ctx, pool, in)
		if err != nil { return nil, Result{}, err }
		return nil, res, nil
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil { log.Fatalf("mcp server: %v", err) }
}

func main() {
	mcpMode := flag.Bool("mcp", false, "run as a stdio MCP server")
	ticker := flag.String("ticker", "AAPL", "Stock ticker (CLI mode)")
	verbose := flag.Bool("v", false, "enable verbose logging")
	flag.Parse()
	logLevel := slog.LevelInfo
	if *verbose { logLevel = slog.LevelDebug }
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))
	pool, err := ants.NewPool(20)
	if err != nil { log.Fatalf("create pool: %v", err) }
	defer pool.Release()
	if *mcpMode {
		runMCPServer(pool)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res, err := runWorkflow(ctx, pool, UserInput{Ticker: *ticker})
	if err != nil { log.Fatalf("workflow failed: %v", err) }
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil { log.Fatalf("encode output: %v", err) }
}
