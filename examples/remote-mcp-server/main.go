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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/panjf2000/ants/v2"
	"github.com/akennis/dagor"
	"github.com/akennis/dagor/graph"
	"github.com/akennis/dagor/operator"
	builtin "github.com/akennis/dagor/operator/builtin"
	"github.com/akennis/dagor/reporter"
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

// ─── Workflow inputs / outputs ─────────────────────────────────────────────

// UserInput is the external boundary of the workflow. In CLI mode it is filled
// from --query; in MCP mode the SDK derives the tool input schema from this
// struct and deserializes + validates every tools/call request into it.
type UserInput struct {
	Query string `json:"query" jsonschema:"the search query to run against the Cloudflare documentation; required"`
}

// Result is the workflow's structured output: the MCP tool's typed Out (the
// SDK emits it as structured content + a JSON text block) and the CLI's stdout.
type Result struct {
	Query   string `json:"query"`
	Results string `json:"results"`
}

// ─── Shared execution path ─────────────────────────────────────────────────

// runWorkflow is the single path shared by CLI and MCP modes. It builds a
// fresh graph + engine per invocation so concurrent MCP tool calls never share
// mutable operator state; the ants.Pool is safe to share. The query is
// injected via context.WithValue exactly as in CLI mode.
func runWorkflow(ctx context.Context, pool *ants.Pool, in UserInput) (Result, error) {
	g, err := buildGraph()
	if err != nil {
		return Result{}, fmt.Errorf("build graph: %w", err)
	}

	eng, err := dagor.NewEngine(g, pool, dagor.WithReporter(reporter.New(slog.Default())))
	if err != nil {
		return Result{}, fmt.Errorf("create engine: %w", err)
	}

	ctx = context.WithValue(ctx, searchInputKey{}, SearchInput{Query: in.Query})

	if err := eng.Run(ctx); err != nil {
		return Result{}, fmt.Errorf("run graph: %w", err)
	}

	raw, ok := eng.GetOutput("search_results")
	if !ok {
		return Result{}, fmt.Errorf("missing output: search_results")
	}
	sp, ok := raw.(*string)
	if !ok || sp == nil {
		return Result{}, fmt.Errorf("unexpected output type for search_results: %T", raw)
	}
	return Result{Query: in.Query, Results: *sp}, nil
}

// ─── MCP server mode ───────────────────────────────────────────────────────

// runMCPServer exposes the workflow as one MCP tool over stdin/stdout. The SDK
// infers the tool input schema from UserInput, auto-deserializes + validates
// each request, and (because Out is non-any) emits Result as structured
// content plus a JSON text block, so the handler returns nil for the result.
//
// Note this process is then both an MCP server (to its caller) and an MCP
// client (of the remote Cloudflare docs server the graph queries).
func runMCPServer(pool *ants.Pool) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "remote-mcp-server",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_cloudflare_docs",
		Description: "Search the official Cloudflare documentation via the public remote MCP server and return the matching documentation text.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in UserInput) (*mcp.CallToolResult, Result, error) {
		if strings.TrimSpace(in.Query) == "" {
			return nil, Result{}, fmt.Errorf("query is required")
		}
		res, err := runWorkflow(ctx, pool, in)
		if err != nil {
			return nil, Result{}, err
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
	query := flag.String("query", "", "Search query for Cloudflare documentation (CLI mode)")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

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

	pool, err := ants.NewPool(2)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	defer pool.Release()

	if *mcpMode {
		runMCPServer(pool)
		return
	}

	if *query == "" {
		fmt.Fprintln(os.Stderr, "--query is required (or pass --mcp to run as an MCP server)")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	res, err := runWorkflow(ctx, pool, UserInput{Query: *query})
	if err != nil {
		log.Fatalf("workflow: %v", err)
	}

	fmt.Fprintf(os.Stderr, "query: %q\n--- result ---\n", res.Query)
	fmt.Println(res.Results)
}
