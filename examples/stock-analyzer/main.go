package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/akennis/sparsi-go/library"
	"github.com/panjf2000/ants/v2"
	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/graph"
	"github.com/wwz16/dagor/operator"
	builtin "github.com/wwz16/dagor/operator/builtin"
	"github.com/wwz16/dagor/reporter"
)

type tickerKey struct{}

func init() {
	// Register the ticker input op.
	if err := operator.RegisterOpFactory("ticker_src", builtin.ContextValFactory[string](tickerKey{})); err != nil {
		log.Fatalf("register ticker_src: %v", err)
	}

	// Constants for Yahoo URL building.
	library.RegisterConst("quote_prefix", "https://query2.finance.yahoo.com/v8/finance/chart/")
	library.RegisterConst("quote_suffix", "?interval=1d&range=1d")
	library.RegisterConst("news_prefix", "https://query2.finance.yahoo.com/v1/finance/search?q=")
	library.RegisterConst("news_suffix", "&quotesCount=0&newsCount=1")

	// Paths for JSON extraction.
	library.RegisterConst("path_price", "chart.result.0.meta.regularMarketPrice")
	library.RegisterConst("path_prev_close", "chart.result.0.meta.chartPreviousClose")
	library.RegisterConst("path_news_title", "news.0.title")

	// Prompt fragments for final analysis.
	library.RegisterConst("prompt_header", "Analysis for stock ticker: ")
	library.RegisterConst("prompt_price", "\nCurrent Price: ")
	library.RegisterConst("prompt_change", "\nPrice Change (since prev close): ")
	library.RegisterConst("prompt_headline", "\nLatest Headline: ")
	library.RegisterConst("prompt_sentiment", "\nSentiment Score (0.0=bearish, 1.0=bullish): ")
	library.RegisterConst("prompt_footer", "\n\nBased on these data points, provide a concise Buy/Hold/Sell recommendation with a one-sentence rationale.")
}

func buildGraph() (*graph.Graph, error) {
	b := graph.NewBuilder("stock_analyzer")

	// 1. Ticker input
	b.Vertex("ticker_src").Op("ticker_src").Output("Result", "ticker")

	// 2. Build Quote URL: prefix + ticker + suffix
	b.Vertex("quote_prefix").Op("quote_prefix").Output("Result", "q_pre")
	b.Vertex("quote_suffix").Op("quote_suffix").Output("Result", "q_suf")
	b.Vertex("q_join_1").Op("StringConcatOp").
		Input("A", "q_pre").Input("B", "ticker").Output("Result", "q_mid")
	b.Vertex("quote_url").Op("StringConcatOp").
		Input("A", "q_mid").Input("B", "q_suf").Output("Result", "quote_url")

	// 3. Build News URL: prefix + ticker + suffix
	b.Vertex("news_prefix").Op("news_prefix").Output("Result", "n_pre")
	b.Vertex("news_suffix").Op("news_suffix").Output("Result", "n_suf")
	b.Vertex("n_join_1").Op("StringConcatOp").
		Input("A", "n_pre").Input("B", "ticker").Output("Result", "n_mid")
	b.Vertex("news_url").Op("StringConcatOp").
		Input("A", "n_mid").Input("B", "n_suf").Output("Result", "news_url")

	// 4. Fetch data in parallel
	b.Vertex("fetch_quote").Op("HTTPGetOp").Input("URL", "quote_url").Output("Body", "quote_json")
	b.Vertex("fetch_news").Op("HTTPGetOp").Input("URL", "news_url").Output("Body", "news_json")

	// 5. Extract fields
	b.Vertex("path_price").Op("path_price").Output("Result", "p_path")
	b.Vertex("extract_price").Op("JSONExtractOp").
		Input("JSON", "quote_json").Input("Path", "p_path").Output("Value", "price_raw")

	b.Vertex("path_prev").Op("path_prev_close").Output("Result", "pc_path")
	b.Vertex("extract_prev").Op("JSONExtractOp").
		Input("JSON", "quote_json").Input("Path", "pc_path").Output("Value", "prev_raw")

	b.Vertex("path_news").Op("path_news_title").Output("Result", "n_path")
	b.Vertex("extract_news").Op("JSONExtractOp").
		Input("JSON", "news_json").Input("Path", "n_path").Output("Value", "headline")

	// 6. Parse numbers (AI fallback for string->float)
	b.Vertex("parse_price").Op("AIParseNumberOp").
		Params(map[string]string{
			"provider": "gemini",
			"model":    "gemini-3-flash-preview",
		}).
		Input("Input", "price_raw").Output("Result", "price")

	b.Vertex("parse_prev").Op("AIParseNumberOp").
		Params(map[string]string{
			"provider": "gemini",
			"model":    "gemini-3-flash-preview",
		}).
		Input("Input", "prev_raw").Output("Result", "prev_close")

	// 7. Deterministic Math
	b.Vertex("calc_change").Op("SubFloatOp").
		Input("A", "price").Input("B", "prev_close").Output("Result", "change")

	// 8. AI Sentiment
	b.Vertex("sentiment").Op("AIScoreOp").
		Params(map[string]string{
			"provider":  "gemini",
			"model":     "gemini-3-flash-preview",
			"criterion": "The headline indicates a positive/bullish outlook for the company",
		}).
		Input("Input", "headline").Output("Result", "sentiment_score")

	// 9. Convert Sentiment to String for prompt building
	b.Vertex("sentiment_str").Op("Float64ToStringOp").
		Input("Value", "sentiment_score").Output("Result", "sentiment_txt")

	// 10. Convert Change to String
	b.Vertex("change_str").Op("Float64ToStringOp").
		Input("Value", "change").Output("Result", "change_txt")

	// 11. Build final prompt (chain of StringConcatOp)
	b.Vertex("p_header").Op("prompt_header").Output("Result", "ph")
	b.Vertex("p_price").Op("prompt_price").Output("Result", "pp")
	b.Vertex("p_change").Op("prompt_change").Output("Result", "pc")
	b.Vertex("p_headline").Op("prompt_headline").Output("Result", "phl")
	b.Vertex("p_sentiment").Op("prompt_sentiment").Output("Result", "ps")
	b.Vertex("p_footer").Op("prompt_footer").Output("Result", "pf")

	// Concat sequence:
	b.Vertex("c1").Op("StringConcatOp").Input("A", "ph").Input("B", "ticker").Output("Result", "s1")
	b.Vertex("c2").Op("StringConcatOp").Input("A", "s1").Input("B", "pp").Output("Result", "s2")
	b.Vertex("c3").Op("StringConcatOp").Input("A", "s2").Input("B", "price_raw").Output("Result", "s3")
	b.Vertex("c4").Op("StringConcatOp").Input("A", "s3").Input("B", "pc").Output("Result", "s4")
	b.Vertex("c5").Op("StringConcatOp").Input("A", "s4").Input("B", "change_txt").Output("Result", "s5")
	b.Vertex("c6").Op("StringConcatOp").Input("A", "s5").Input("B", "phl").Output("Result", "s6")
	b.Vertex("c7").Op("StringConcatOp").Input("A", "s6").Input("B", "headline").Output("Result", "s7")
	b.Vertex("c8").Op("StringConcatOp").Input("A", "s7").Input("B", "ps").Output("Result", "s8")
	b.Vertex("c9").Op("StringConcatOp").Input("A", "s8").Input("B", "sentiment_txt").Output("Result", "s9")
	b.Vertex("c10").Op("StringConcatOp").Input("A", "s9").Input("B", "pf").Output("Result", "final_prompt")

	// 12. Final Recommendation
	b.Vertex("recommend").Op("AIComputeStringToStringOp").
		Params(map[string]string{
			"provider":  "gemini",
			"model":     "gemini-3-flash-preview",
			"operation": "Analyze the given stock data and sentiment to provide a Buy/Hold/Sell recommendation.",
		}).
		Input("Input", "final_prompt").
		Output("Result", "recommendation")

	return b.Build()
}

func main() {
	ticker := flag.String("ticker", "AAPL", "Stock ticker to analyze")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

	if os.Getenv("GEMINI_API_KEY") == "" {
		fmt.Fprintln(os.Stderr, "GEMINI_API_KEY is required")
		os.Exit(1)
	}

	var lvl slog.LevelVar
	switch strings.ToLower(*logLevel) {
	case "debug":
		lvl.Set(slog.LevelDebug)
	case "warn":
		lvl.Set(slog.LevelWarn)
	case "error":
		lvl.Set(slog.LevelError)
	default:
		lvl.Set(slog.LevelInfo)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: &lvl})))

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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ctx = context.WithValue(ctx, tickerKey{}, strings.ToUpper(*ticker))

	if err := eng.Run(ctx); err != nil {
		log.Fatalf("run graph: %v", err)
	}

	raw, ok := eng.GetOutput("recommendation")
	if !ok {
		log.Fatalf("missing output: recommendation")
	}
	rec, ok := raw.(*string)
	if !ok || rec == nil {
		log.Fatalf("unexpected output type: %T", raw)
	}

	fmt.Printf("\nAnalysis for %s:\n", strings.ToUpper(*ticker))
	fmt.Println("-----------------------------------")
	fmt.Println(*rec)
}
