# Stock Analyzer v2

This example demonstrates a more complex, real-world application of `sparsi-go` combined with the [dagor](https://github.com/wwz16/dagor) DAG engine. It performs a comprehensive stock analysis by concurrently fetching data from multiple external sources and using an LLM to synthesize a recommendation.

## Features

- **Concurrent Data Fetching**: Uses a Directed Acyclic Graph (DAG) to fetch stock financials, macro-economic indicators, and recent news in parallel.
- **Multi-Source Integration**:
    - **Alpha Vantage**: Stock price, market cap, PE ratio, SMA, and RSI.
    - **FRED (Federal Reserve Economic Data)**: Interest rates (Fed Funds Rate), Inflation (CPI), and GDP.
    - **NewsAPI**: Latest news articles related to the ticker.
- **AI-Powered Synthesis**: Uses `sparsi-go`'s `AIComputeOp` to analyze the gathered context and provide a structured recommendation (Buy/Hold/Sell) with rationale.
- **Structured Output**: Demonstrates how to define and use custom data types for AI responses with automatic parsing.

## Prerequisites

You will need API keys for the following services:

1.  **Gemini**: [Google AI Studio](https://aistudio.google.com/)
2.  **Alpha Vantage**: [Get your free API key](https://www.alphavantage.co/support/#api-key)
3.  **FRED**: [Request an API key](https://fred.stlouisfed.org/docs/api/api_key.html)
4.  **NewsAPI**: [Get an API key](https://newsapi.org/register)

Set them as environment variables:

```bash
export GEMINI_API_KEY="your-gemini-key"
export ALPHA_VANTAGE_API_KEY="your-alpha-vantage-key"
export FRED_API_KEY="your-fred-key"
export NEWS_API_KEY="your-news-api-key"
```

## Usage

Run the analyzer with a specific ticker:

```bash
go run main.go --ticker AAPL
```

Use the `-v` flag for verbose logging to see the DAG execution details:

```bash
go run main.go --ticker TSLA -v
```

## How it Works

The application executes the following graph structure:

```text
[ticker_input]
      │
      ├───────────────────┬───────────────────┐
      │                   │                   │
      ▼                   ▼                   ▼
[AVFetchOp]        [FetchMacroOp]      [FetchNewsOp]
(Stock Data)       (Economic Data)     (Recent News)
      │                   │                   │
      └───────────────────┼───────────────────┘
                          │
                          ▼
              [FormatStockContextOp]
               (Aggregates Context)
                          │
                          ▼
                    [AnalyzeOp]
                (Gemini AI Analysis)
                          │
                          ▼
                   [Final Result]
```

1.  **Input**: The ticker symbol is provided via CLI and injected into the DAG.
2.  **Parallel Execution**:
    - `AVFetchOp`: Calls Alpha Vantage APIs for price and technical indicators.
    - `FetchMacroOp`: Calls FRED APIs for interest rates, inflation, and GDP.
    - `FetchNewsOp`: Gathers recent news articles.
3.  **Context Construction**: `FormatStockContextOp` aggregates all fetched data into a structured prompt.
4.  **AI Analysis**: `AnalyzeOp` sends the context to Gemini with instructions to provide a recommendation and rationale in JSON format.
5.  **Output**: The application parses the JSON response and prints a formatted summary to the console.
