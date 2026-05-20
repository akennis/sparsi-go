// Package main demonstrates sparsi's remote (HTTP) MCP transport against the
// public Cloudflare docs MCP server at https://docs.mcp.cloudflare.com, which
// exposes the search_cloudflare_documentation tool over streamable HTTP. No
// subprocess, no API keys.
//
// Reference: https://github.com/cloudflare/mcp-server-cloudflare/tree/main/apps/docs-vectorize
//
// DAG:
//
//	search_input ─► cf_search ─► search_results (string)
//	  (ContextVal,    (MCPCloudflareDocsSearchOp,
//	   SearchInput     transport=http,
//	   from --query)   url=https://docs.mcp.cloudflare.com/mcp,
//	                   tool_name=search_cloudflare_documentation)
//
// Prerequisites:
//   - Network access to docs.mcp.cloudflare.com.
//   - No CLAUDE_API_KEY required.
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

// SearchInput is the typed argument shape passed to the
// search_cloudflare_documentation tool. The json tag aligns with the tool's
// input schema.
type SearchInput struct {
	Query string `json:"query"`
}

// MCPCloudflareDocsSearchOp is a concrete MCPCallOp variant pinned to the
// Cloudflare docs vector-search tool. The vertex's params (transport=http,
// url, tool_name) are wired in buildGraph below.
type MCPCloudflareDocsSearchOp struct {
	library.MCPCallOp[SearchInput, string]
}

type searchInputKey struct{}

func init() {
	if err := operator.RegisterOpFactory(
		"search_input_const",
		builtin.ContextValFactory[SearchInput](searchInputKey{}),
	); err != nil {
		log.Fatalf("register search_input_const: %v", err)
	}
	operator.RegisterOp[MCPCloudflareDocsSearchOp]()
}

func buildGraph() (*graph.Graph, error) {
	cfParams := map[string]string{
		"transport":       "http",
		"url":             "https://docs.mcp.cloudflare.com/mcp",
		"tool_name":       "search_cloudflare_documentation",
		"init_timeout_ms": "30000",
		"call_timeout_ms": "60000",
		"max_retries":     "2",
		// For private/authenticated remote MCP servers, add a Bearer token (or
		// any other static header) via the headers param. The Cloudflare docs
		// endpoint is public, so this stays commented out:
		//   "headers": "Authorization=Bearer ${TOKEN}",
	}

	return graph.NewBuilder("mcp_cloudflare_docs_search").
		Vertex("search_input").Op("search_input_const").
		Output("Result", "search_input").
		Vertex("cf_search").Op("MCPCloudflareDocsSearchOp").
		Params(cfParams).
		Input("Input", "search_input").
		Output("Result", "search_results").
		Build()
}

func main() {
	query := flag.String("query", "", "Search query for Cloudflare documentation (required)")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

	if *query == "" {
		fmt.Fprintln(os.Stderr, "--query is required")
		os.Exit(2)
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

	pool, err := ants.NewPool(2)
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
	ctx = context.WithValue(ctx, searchInputKey{}, SearchInput{Query: *query})

	if err := eng.Run(ctx); err != nil {
		log.Fatalf("run graph: %v", err)
	}

	raw, ok := eng.GetOutput("search_results")
	if !ok {
		log.Fatalf("missing output: search_results")
	}
	sp, ok := raw.(*string)
	if !ok || sp == nil {
		log.Fatalf("unexpected output type for search_results: %T", raw)
	}

	fmt.Fprintf(os.Stderr, "query: %q\n--- result ---\n", *query)
	fmt.Println(*sp)
}
