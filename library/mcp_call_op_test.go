package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpTestCommand returns a command guaranteed to be on PATH for the current
// platform. The stdio Setup path validates command existence with exec.LookPath
// but never invokes it (in-memory transports drive the end-to-end tests), so
// any resolvable executable suffices.
func mcpTestCommand() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}

// ---- Setup() validation ----

func TestMCPCallOp_Setup_MissingCommand(t *testing.T) {
	op := &MCPCallOp[map[string]any, string]{}
	err := op.Setup(mustParams(t, map[string]string{"tool_name": "x"}))
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Fatalf("expected 'command' error, got %v", err)
	}
}

func TestMCPCallOp_Setup_CommandNotOnPath(t *testing.T) {
	op := &MCPCallOp[map[string]any, string]{}
	err := op.Setup(mustParams(t, map[string]string{
		"command":   "definitely-not-a-real-binary-xyz-12345",
		"tool_name": "x",
	}))
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("expected 'PATH' error, got %v", err)
	}
}

func TestMCPCallOp_Setup_MissingToolName(t *testing.T) {
	op := &MCPCallOp[map[string]any, string]{}
	err := op.Setup(mustParams(t, map[string]string{"command": mcpTestCommand()}))
	if err == nil || !strings.Contains(err.Error(), "tool_name") {
		t.Fatalf("expected 'tool_name' error, got %v", err)
	}
}

func TestMCPCallOp_Setup_DefaultsAndOverrides(t *testing.T) {
	op := &MCPCallOp[map[string]any, string]{}
	err := op.Setup(mustParams(t, map[string]string{
		"command":         mcpTestCommand(),
		"tool_name":       "tool",
		"init_timeout_ms": "5000",
		"call_timeout_ms": "1500",
		"max_retries":     "7",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if op.initTimeout.Milliseconds() != 5000 {
		t.Errorf("initTimeout: want 5000ms, got %v", op.initTimeout)
	}
	if op.callTimeout.Milliseconds() != 1500 {
		t.Errorf("callTimeout: want 1500ms, got %v", op.callTimeout)
	}
	if op.maxRetries != 7 {
		t.Errorf("maxRetries: want 7, got %d", op.maxRetries)
	}
}

func TestMCPCallOp_Setup_NonNumericRetriesFallback(t *testing.T) {
	op := &MCPCallOp[map[string]any, string]{}
	err := op.Setup(mustParams(t, map[string]string{
		"command":     mcpTestCommand(),
		"tool_name":   "tool",
		"max_retries": "abc",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if op.maxRetries != 3 {
		t.Errorf("maxRetries: want 3 (default), got %d", op.maxRetries)
	}
}

func TestMCPCallOp_Setup_ArgsAndEnvParsing(t *testing.T) {
	op := &MCPCallOp[map[string]any, string]{}
	err := op.Setup(mustParams(t, map[string]string{
		"command":   mcpTestCommand(),
		"tool_name": "tool",
		"args":      "  -y , --root , /tmp/x ",
		"env":       "  FOO=bar , BAZ=qux , malformed ",
	}))
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"-y", "--root", "/tmp/x"}
	if !reflect.DeepEqual(op.spec.args, wantArgs) {
		t.Errorf("args: want %v, got %v", wantArgs, op.spec.args)
	}
	wantEnv := []string{"FOO=bar", "BAZ=qux"}
	if !reflect.DeepEqual(op.spec.env, wantEnv) {
		t.Errorf("env: want %v, got %v (malformed entry should be dropped)", wantEnv, op.spec.env)
	}
}

// ---- Field interface ----

func TestMCPCallOp_SetInputField_WrongType(t *testing.T) {
	op := &MCPCallOp[echoArgs, string]{}
	err := op.SetInputField("Input", 42)
	if err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("expected wrong-type error, got %v", err)
	}
}

func TestMCPCallOp_SetInputField_UnknownField(t *testing.T) {
	op := &MCPCallOp[echoArgs, string]{}
	args := echoArgs{Msg: "hi"}
	err := op.SetInputField("Bogus", &args)
	if err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestMCPCallOp_ResetFields(t *testing.T) {
	op := &MCPCallOp[echoArgs, string]{}
	args := echoArgs{Msg: "hi"}
	op.Input = &args
	op.Result = "hello"
	op.ResetFields()
	if op.Input != nil {
		t.Errorf("Input should be nil after ResetFields, got %v", op.Input)
	}
	if op.Result != "" {
		t.Errorf("Result should be \"\" after ResetFields, got %q", op.Result)
	}
}

// ---- parseResultText dispatch ----

func TestMCPCallOp_parseResultText_String(t *testing.T) {
	op := &MCPCallOp[any, string]{}
	if err := op.parseResultText("  hello world  "); err != nil {
		t.Fatal(err)
	}
	if op.Result != "hello world" {
		t.Errorf("expected 'hello world', got %q", op.Result)
	}
}

func TestMCPCallOp_parseResultText_Int(t *testing.T) {
	op := &MCPCallOp[any, int]{}
	if err := op.parseResultText("42"); err != nil {
		t.Fatal(err)
	}
	if op.Result != 42 {
		t.Errorf("expected 42, got %d", op.Result)
	}
}

func TestMCPCallOp_parseResultText_Float64(t *testing.T) {
	op := &MCPCallOp[any, float64]{}
	if err := op.parseResultText("3.14"); err != nil {
		t.Fatal(err)
	}
	if op.Result != 3.14 {
		t.Errorf("expected 3.14, got %v", op.Result)
	}
}

func TestMCPCallOp_parseResultText_StringSlice(t *testing.T) {
	op := &MCPCallOp[any, []string]{}
	if err := op.parseResultText("a, b, c"); err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(op.Result, want) {
		t.Errorf("expected %v, got %v", want, op.Result)
	}
}

func TestMCPCallOp_parseResultText_Bool(t *testing.T) {
	t.Run("true", func(t *testing.T) {
		op := &MCPCallOp[any, bool]{}
		if err := op.parseResultText("true"); err != nil || op.Result != true {
			t.Fatalf("got %v err=%v", op.Result, err)
		}
	})
	t.Run("nope", func(t *testing.T) {
		op := &MCPCallOp[any, bool]{}
		err := op.parseResultText("nope")
		if err == nil || !strings.Contains(err.Error(), "expected bool") {
			t.Fatalf("expected bool error, got %v", err)
		}
	})
}

// ---- decodeResult: structured prefers over text ----

func TestMCPCallOp_decodeResult_StructuredJSON(t *testing.T) {
	type S struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	op := &MCPCallOp[any, S]{}
	err := op.decodeResult(mcpCallOutcome{
		structured: json.RawMessage(`{"name":"alice","age":30}`),
		text:       "ignored fallback",
	})
	if err != nil {
		t.Fatal(err)
	}
	if op.Result.Name != "alice" || op.Result.Age != 30 {
		t.Errorf("expected {alice,30}, got %+v", op.Result)
	}
}

// ---- End-to-end via in-process server ----

type echoArgs struct {
	Msg string `json:"msg"`
}

func TestMCPCallOp_EndToEnd_InProcessServer(t *testing.T) {
	ctx := context.Background()

	s := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "0.0.1"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "echo", Description: "echo"},
		func(ctx context.Context, req *mcp.CallToolRequest, args echoArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: args.Msg}},
			}, nil, nil
		})

	st, ct := mcp.NewInMemoryTransports()
	serverSess, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSess.Close()

	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSess, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	sess := &mcpSession{client: c, session: clientSess}
	defer sess.close()

	out, err := sess.callTool(ctx, "echo", echoArgs{Msg: "hi there"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out.isToolError {
		t.Fatalf("unexpected tool error: %s", out.text)
	}
	if out.text != "hi there" {
		t.Errorf("expected 'hi there', got %q", out.text)
	}
}

func TestMCPCallOp_EndToEnd_ToolError(t *testing.T) {
	ctx := context.Background()

	s := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "0.0.1"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "fail"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
			res := &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "not allowed"}},
			}
			res.IsError = true
			return res, nil, nil
		})

	st, ct := mcp.NewInMemoryTransports()
	serverSess, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSess.Close()

	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSess, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	sess := &mcpSession{client: c, session: clientSess}
	defer sess.close()

	out, err := sess.callTool(ctx, "fail", map[string]any{}, 0)
	if err != nil {
		t.Fatalf("transport error not expected: %v", err)
	}
	if !out.isToolError {
		t.Fatalf("expected isToolError=true; got false (text=%q)", out.text)
	}
	if out.text != "not allowed" {
		t.Errorf("expected 'not allowed', got %q", out.text)
	}
}

// ---- Description registry ----

func TestAllDescriptions_IncludesMCPSection(t *testing.T) {
	all := AllDescriptions()
	if !strings.Contains(all, "## MCP") {
		t.Fatal("AllDescriptions() missing '## MCP' section")
	}
	if !strings.Contains(all, "MCPCallOp:") {
		t.Error("AllDescriptions() missing MCPCallOp entry")
	}
	if !strings.Contains(all, "MCPScriptOp:") {
		t.Error("AllDescriptions() missing MCPScriptOp entry")
	}
}

// ---- mcpParseTransportSpec ----

func TestMCPParseTransportSpec_DefaultsToStdio(t *testing.T) {
	spec, err := mcpParseTransportSpec(mustParams(t, map[string]string{
		"command": mcpTestCommand(),
	}), "test")
	if err != nil {
		t.Fatal(err)
	}
	if spec.kind != "stdio" {
		t.Errorf("want kind=stdio, got %q", spec.kind)
	}
}

func TestMCPParseTransportSpec_UnknownTransport(t *testing.T) {
	_, err := mcpParseTransportSpec(mustParams(t, map[string]string{
		"transport": "bogus",
	}), "test")
	if err == nil || !strings.Contains(err.Error(), "unknown transport") {
		t.Fatalf("want unknown-transport error, got %v", err)
	}
}

func TestMCPParseTransportSpec_HTTPMissingURL(t *testing.T) {
	_, err := mcpParseTransportSpec(mustParams(t, map[string]string{
		"transport": "http",
	}), "test")
	if err == nil || !strings.Contains(err.Error(), "url") {
		t.Fatalf("want missing-url error, got %v", err)
	}
}

func TestMCPParseTransportSpec_HTTPRejectsNonHTTPScheme(t *testing.T) {
	_, err := mcpParseTransportSpec(mustParams(t, map[string]string{
		"transport": "http",
		"url":       "ftp://example.com/mcp",
	}), "test")
	if err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("want scheme error, got %v", err)
	}
}

func TestMCPParseTransportSpec_HTTPHeadersSorted(t *testing.T) {
	spec, err := mcpParseTransportSpec(mustParams(t, map[string]string{
		"transport": "http",
		"url":       "https://example.com/mcp",
		"headers":   "Z=last, A=first, malformed, M=mid",
	}), "test")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"A=first", "M=mid", "Z=last"}
	if !reflect.DeepEqual(spec.headers, want) {
		t.Errorf("headers: want %v, got %v", want, spec.headers)
	}
}

// ---- HTTP transport: pool guard ----

func TestMCPCallOp_Setup_HTTPRejectsPool(t *testing.T) {
	op := &MCPCallOp[map[string]any, string]{}
	err := op.Setup(mustParams(t, map[string]string{
		"transport": "http",
		"url":       "https://example.com/mcp",
		"tool_name": "echo",
		"pool_size": "1",
	}))
	if err == nil || !strings.Contains(err.Error(), "pool_size") {
		t.Fatalf("want pool_size guard error, got %v", err)
	}
}

// ---- HTTP transport: end-to-end against httptest.Server ----

func TestMCPCallOp_EndToEnd_StreamableHTTP(t *testing.T) {
	ctx := context.Background()

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "0.0.1"}, nil)
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "echo", Description: "echo"},
		func(ctx context.Context, req *mcp.CallToolRequest, args echoArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: args.Msg}},
			}, nil, nil
		})

	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil)

	// Wrap in a middleware that asserts the injected header arrives on every request.
	var sawHeader atomic.Bool
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Auth") == "secret-token" {
			sawHeader.Store(true)
		}
		mcpHandler.ServeHTTP(w, r)
	})

	srv := httptest.NewServer(wrapped)
	defer srv.Close()

	spec, err := mcpParseTransportSpec(mustParams(t, map[string]string{
		"transport": "http",
		"url":       srv.URL,
		"headers":   "X-Test-Auth=secret-token",
	}), "test")
	if err != nil {
		t.Fatal(err)
	}

	sess, err := startMCPSessionFromSpec(ctx, spec, 0)
	if err != nil {
		t.Fatalf("startMCPSessionFromSpec: %v", err)
	}
	defer sess.close()

	out, err := sess.callTool(ctx, "echo", echoArgs{Msg: "hello over http"}, 0)
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}
	if out.isToolError {
		t.Fatalf("unexpected tool error: %s", out.text)
	}
	if out.text != "hello over http" {
		t.Errorf("want 'hello over http', got %q", out.text)
	}
	if !sawHeader.Load() {
		t.Error("injected header X-Test-Auth=secret-token never arrived at server")
	}
}
