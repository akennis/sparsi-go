# Stock Analyzer V2 Example

This is an advanced version of the Stock Analyzer example, demonstrating a complex multi-source workflow. This entire example was autonomously designed and implemented using the `sparsi-design` and `sparsi-codegen` skills.

## Overview

Stock Analyzer V2 integrates data from three different financial and economic providers to provide a comprehensive Buy/Hold/Sell recommendation for any US stock ticker.

### Data Sources

- **Polygon.io**: Fetches company details, daily snapshots (price, volume), and the latest financial statements (revenue, net income).
- **NewsAPI**: Retrieves the latest five news articles relevant to the ticker.
- **FRED (Federal Reserve Economic Data)**: Provides macroeconomic context including the latest GDP, CPI (Inflation), and Federal Funds Rate.

### Key Features

- **Skill-Generated**: Showcases the power of `sparsi` skills in designing and generating production-ready Go workflows.
- **Custom Pruning Operators**: Implements specialized operators to parse and summarize raw JSON responses from multiple APIs, reducing token usage in the final AI prompt.
- **Parallel Pipeline**: Executes data fetching for all sources concurrently.
- **Dual Mode**: Operates as a standard CLI tool or as a **Model Context Protocol (MCP)** server.
- **Rich Context**: Feeds the AI a structured summary of financial metrics, news sentiment, and macroeconomic trends for high-quality reasoning.

## Prerequisites

To run this example, you will need the following API keys:

- `GEMINI_API_KEY`: For the AI recommendation engine.
- `POLYGON_API_KEY`: [Get one here](https://polygon.io/).
- `NEWSAPI_API_KEY`: [Get one here](https://newsapi.org/).
- `FRED_API_KEY`: [Get one here](https://fred.stlouisfed.org/docs/api/api_key.html).

Set them as environment variables:

```bash
export GEMINI_API_KEY=your_key
export POLYGON_API_KEY=your_key
export NEWSAPI_API_KEY=your_key
export FRED_API_KEY=your_key
```

## Usage

Since this example is a self-contained module, navigate to its directory before running:

```bash
cd examples/stock_analyzer_v2
```

### CLI Mode

Analyze a stock directly from your terminal:

```bash
# Analyze Apple (AAPL)
go run . --ticker AAPL

# Verbose mode to see DAG execution
go run . --ticker MSFT -v
```

### MCP Mode

Run the analyzer as an MCP server to use it with MCP-compatible clients (like Claude Desktop):

```bash
go run . --mcp
```

## Workflow Structure

1. **Input**: Receives ticker via context.
2. **Auth**: Retrieves API keys from environment variables.
3. **Fetch (Parallel)**:
   - **Polygon**: Details, Snapshot, Financials.
   - **NewsAPI**: Top headlines.
   - **FRED**: GDP, CPI, Rates.
4. **Prune**: Custom operators clean and summarize the JSON data.
5. **Build Prompt**: A specialized operator assembles the "Master Prompt".
6. **Recommend**: `AIComputeOp` (Gemini 3.5 Flash) generates the final verdict.
