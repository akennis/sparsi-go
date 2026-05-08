package library

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
)

// MCPSession is the per-call interface handed to an MCPScriptOp's Script
// callback. It wraps a single live MCP client session; each CallTool invocation
// reuses the same subprocess and protocol session, so server-side state
// (browser pages, file handles, cursors) persists across calls within one Run.
//
// CallTool returns the concatenated text content and the raw structured-content
// JSON (nil if the tool emitted none). Tool-level errors (where the server
// returned IsError=true) surface as a *MCPToolError; transport / I/O failures
// surface as their underlying error.
type MCPSession interface {
	CallTool(ctx context.Context, name string, args any) (text string, structured json.RawMessage, err error)
}

// MCPToolError is returned by MCPSession.CallTool when the MCP server reported
// a tool-level error (IsError=true). Scripts can errors.As this to recover
// from anticipated failures (e.g. element-not-found on a click).
type MCPToolError struct {
	Tool string
	Text string
}

func (e *MCPToolError) Error() string {
	return fmt.Sprintf("MCP tool %q error: %s", e.Tool, strings.TrimSpace(e.Text))
}

const MCPScriptOpDescription = `MCPScriptOp: orchestrate a sequence of MCP tool calls against a single,
long-lived MCP session. Use this when one DAG step needs multiple tool calls
that share server-side state (browser session, file handles, etc.). The
user-supplied Script callback runs exactly once per Run, receives a
MCPSession, and is free to invoke CallTool any number of times in any order.
  Params:   transport       — "stdio" (default) or "http". Selects how the MCP server is reached.
            stdio params:
              command         — server executable (e.g. "npx", "uvx", "/abs/path"). Required.
              args            — comma-separated CLI args (e.g. "-y,@playwright/mcp@latest"). Optional.
              env             — comma-separated KEY=VALUE pairs. Optional.
            http params:
              url             — full endpoint URL (http or https). Required.
              headers         — comma-separated KEY=VALUE pairs injected into every request
                                (e.g. "Authorization=Bearer ${TOKEN}"). Optional.
            init_timeout_ms — handshake timeout in ms (default "10000").
            call_timeout_ms — per-tool-call timeout in ms (default "30000").
            max_retries     — retries for session-start failures only; the Script runs at most
                              once per Run (default "3").
            pool_size       — warm-replenish pool target capacity per session-spec key
                              (default "0", no pool). Vertices with the same spec share warm
                              slots; each Run gets a fresh session — sessions are never reused
                              for a second Run. Only supported for transport="stdio" in v1.
                              Pair with library.ShutdownMCPPool from main() so pre-started
                              subprocesses drain at exit.
            pool_prewarm    — when pool_size > 0, fill the pool during Setup (default "true");
                              set "false" to fill lazily on first Run.
  Inputs:   Input *In       — typed input handed to Script.
  Outputs:  Result Out      — populated by Script.
Concrete variants embed library.MCPScriptOp[In, Out] in a named struct and
register via operator.RegisterOpFactory(name, factory), with the Script field
assigned in the factory closure.`

// MCPScriptOp is a generic operator that owns one MCP server subprocess for
// the duration of a single Run, hands a MCPSession to a user-supplied Script,
// and tears the subprocess down when Script returns.
//
// Unlike MCPCallOp, MCPScriptOp does not retry the Script on failure — only
// session-start failures are retried (max_retries times). Scripts that need
// internal retries should implement them themselves; this keeps semantics
// predictable for non-idempotent tool sequences.
//
// Do not register MCPScriptOp directly — declare a concrete variant with the
// Script field bound in a factory and register via operator.RegisterOpFactory.
type MCPScriptOp[In, Out any] struct {
	Input  *In
	Result Out

	// Script is invoked exactly once per Run after the MCP session is open.
	// It receives the per-call session, the (possibly nil) input pointer, and
	// a pointer to the Result it should populate. Returning a non-nil error
	// fails the Run.
	Script func(ctx context.Context, sess MCPSession, in *In, out *Out) error

	spec        mcpTransportSpec
	initTimeout time.Duration
	callTimeout time.Duration
	maxRetries  int
	poolSize    int
	poolPrewarm bool
}

func (op *MCPScriptOp[In, Out]) Setup(params *config.Params) error {
	spec, err := mcpParseTransportSpec(params, "MCPScriptOp")
	if err != nil {
		return err
	}
	op.spec = spec
	op.initTimeout = mcpParseDurationMs(params, "init_timeout_ms", 10000)
	op.callTimeout = mcpParseDurationMs(params, "call_timeout_ms", 30000)
	op.maxRetries = 3
	if s := params.GetString("max_retries", ""); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			op.maxRetries = n
		}
	}
	if op.Script == nil {
		return fmt.Errorf("MCPScriptOp: Script callback is nil — bind it in your RegisterOpFactory closure")
	}
	op.poolSize = mcpParsePoolSize(params)
	op.poolPrewarm = mcpParsePoolPrewarm(params)
	if op.poolSize > 0 && op.spec.kind != "stdio" {
		return fmt.Errorf("MCPScriptOp: pool_size > 0 is only supported for transport=\"stdio\" in v1 (got transport=%q)", op.spec.kind)
	}
	if op.poolSize > 0 && op.poolPrewarm {
		mcpPoolPrewarm(op.spec, op.initTimeout, op.poolSize)
	}
	return nil
}

func (op *MCPScriptOp[In, Out]) Reset() error { return nil }

func (op *MCPScriptOp[In, Out]) InputFields() map[string]any {
	return map[string]any{"Input": &op.Input}
}

func (op *MCPScriptOp[In, Out]) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}

func (op *MCPScriptOp[In, Out]) SetInputField(field string, value any) error {
	switch field {
	case "Input":
		val, ok := value.(*In)
		if !ok {
			return fmt.Errorf("field Input: expected *%T, got %T", op.Input, value)
		}
		op.Input = val
	default:
		return fmt.Errorf("field %s is not defined", field)
	}
	return nil
}

func (op *MCPScriptOp[In, Out]) ResetFields() {
	var zeroIn *In
	op.Input = zeroIn
	var zeroOut Out
	op.Result = zeroOut
}

func (op *MCPScriptOp[In, Out]) Run(ctx context.Context) error {
	slog.DebugContext(ctx, "MCPScriptOp.run", "run_id", dagor.RunID(ctx), "transport", op.spec.kind, "target", op.spec.label())

	delay := 500 * time.Millisecond
	var lastStartErr error
	for attempt := 0; attempt <= op.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay = min(delay*2, 30*time.Second)
		}
		sess, err := mcpPoolAcquire(ctx, op.spec, op.initTimeout, op.poolSize)
		if err != nil {
			lastStartErr = err
			slog.WarnContext(ctx, "MCPScriptOp.start_failed", "attempt", attempt+1, "of", op.maxRetries+1, "err", err)
			continue
		}
		adapter := &mcpSessionAdapter{s: sess, callTimeout: op.callTimeout}
		scriptErr := op.Script(ctx, adapter, op.Input, &op.Result)
		if cerr := sess.close(); cerr != nil {
			slog.WarnContext(ctx, "MCPScriptOp.close_warn", "err", cerr)
		}
		return scriptErr
	}
	return fmt.Errorf("MCPScriptOp: all %d session-start attempts failed; last error: %w", op.maxRetries+1, lastStartErr)
}

// mcpSessionAdapter is the unexported MCPSession implementation that wraps an
// internal *mcpSession and applies the configured per-call timeout.
type mcpSessionAdapter struct {
	s           *mcpSession
	callTimeout time.Duration
}

func (a *mcpSessionAdapter) CallTool(ctx context.Context, name string, args any) (string, json.RawMessage, error) {
	out, err := a.s.callTool(ctx, name, args, a.callTimeout)
	if err != nil {
		return "", nil, err
	}
	if out.isToolError {
		return out.text, out.structured, &MCPToolError{Tool: name, Text: out.text}
	}
	return out.text, out.structured, nil
}
