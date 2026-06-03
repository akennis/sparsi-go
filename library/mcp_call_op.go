package library

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akennis/dagor"
	"github.com/akennis/dagor/config"
)

const MCPCallOpDescription = `MCPCallOp: invoke a single MCP server tool as a DAG step.
Each Run opens a fresh session, completes the MCP handshake, calls the tool, and tears the
session down — unless pool_size > 0 opts into the warm-replenish pool (stdio only in v1).
  Params:   transport       — "stdio" (default) or "http". Selects how the MCP server is reached.
            stdio params:
              command         — server executable (e.g. "npx", "uvx", "/abs/path"). Required.
              args            — comma-separated CLI args. Optional.
              env             — comma-separated KEY=VALUE pairs. Optional.
            http params:
              url             — full endpoint URL (http or https). Required.
              headers         — comma-separated KEY=VALUE pairs injected into every request
                                (e.g. "Authorization=Bearer ${TOKEN}"). Optional.
            tool_name       — MCP tool to invoke. Required.
            init_timeout_ms — handshake timeout in ms (default "10000").
            call_timeout_ms — single tool call timeout in ms (default "30000").
            max_retries     — transient-error retries (default "3").
            pool_size       — warm-replenish pool target capacity per session-spec key
                              (default "0", no pool). Only supported for transport="stdio".
                              Pair with library.ShutdownMCPPool from main() so pre-started
                              subprocesses drain at exit.
            pool_prewarm    — when pool_size > 0, fill the pool during Setup (default "true").
  Inputs:   Input *In       — JSON-marshaled as the tool's "arguments" object. Implement
                              library.MCPArgsFormatter on *In to control marshaling.
  Outputs:  Result Out      — default dispatch handles string, float64, int, bool,
                              []string, []float64, []int, map[string]string, and any
                              struct decodable via json.Unmarshal (structured content
                              preferred when the server emits it). Implement
                              library.MCPResponseParser on *Out to fully control parsing.
Concrete variants embed library.MCPCallOp[In, Out] in a named struct and register via
operator.RegisterOp[ConcreteOp]() — never register the generic MCPCallOp directly.`

// MCPArgsFormatter is an optional interface for In types to control how they
// are marshaled into the MCP tool's "arguments" object. If unimplemented, the
// dereferenced *Input value is passed to the SDK and JSON-marshaled as-is.
type MCPArgsFormatter interface {
	FormatMCPArgs() (any, error)
}

// MCPResponseParser is an optional interface for Out types to fully control
// parsing of the MCP tool result. If implemented, MCPCallOp hands it the
// concatenated text content and the raw structured-content JSON (nil if the
// tool didn't emit any) and skips the built-in dispatch.
type MCPResponseParser interface {
	ParseMCPResponse(text string, structured json.RawMessage) error
}

// MCPCallOp is a generic operator that wraps a single MCP server tool as a
// dagor node. By default each Run opens a fresh session, completes the MCP
// handshake, invokes the tool, and tears the session down. Set pool_size to
// opt into the warm-replenish pool, which keeps N pre-started sessions ready
// and moves cold-start off the critical path. Pooling is supported only for
// transport="stdio" in v1.
//
// Vertex params:
//
//	transport       — "stdio" (default) or "http". Selects how the MCP server
//	                  is reached. "stdio" runs a local subprocess; "http" talks
//	                  to a remote server over the streamable HTTP transport.
//
//	stdio (transport="stdio"):
//	command         — server executable (e.g. "npx", "uvx", "/abs/path"). Required.
//	args            — comma-separated CLI args passed to the server. Optional.
//	                  Note: values containing commas are not supported.
//	env             — comma-separated KEY=VALUE pairs added to the subprocess env. Optional.
//	                  Note: values containing commas are not supported.
//
//	http (transport="http"):
//	url             — full endpoint URL (http or https scheme). Required.
//	headers         — comma-separated KEY=VALUE pairs injected into every
//	                  request (e.g. "Authorization=Bearer ${TOKEN}"). Optional.
//	                  Note: values containing commas are not supported.
//
//	tool_name       — MCP tool to invoke. Required.
//	init_timeout_ms — handshake timeout in ms (default "10000").
//	call_timeout_ms — single tool call timeout in ms (default "30000").
//	max_retries     — transient-error retries (default "3").
//	pool_size       — warm-replenish pool target capacity per session-spec key
//	                  (default "0", no pool). Vertices with the same spec
//	                  share warm slots. Each borrow is a fresh session — the
//	                  pool never reuses a session for a second logical call.
//	                  Only supported for transport="stdio" in v1.
//	pool_prewarm    — when pool_size > 0, fill the pool during Setup (default
//	                  "true"); set "false" to fill lazily on first borrow.
//
// Programs that opt into pooling should defer library.ShutdownMCPPool(ctx)
// from main() so pre-started subprocesses are drained at exit.
//
// Do not register MCPCallOp directly — declare a concrete variant such as
// type EchoOp struct{ MCPCallOp[EchoArgs, string] } and register that.
type MCPCallOp[In, Out any] struct {
	Input  *In
	Result Out

	spec        mcpTransportSpec
	toolName    string
	initTimeout time.Duration
	callTimeout time.Duration
	maxRetries  int
	poolSize    int
	poolPrewarm bool
}

func (op *MCPCallOp[In, Out]) Setup(params *config.Params) error {
	spec, err := mcpParseTransportSpec(params, "MCPCallOp")
	if err != nil {
		return err
	}
	op.spec = spec
	op.toolName = params.GetString("tool_name", "")
	if op.toolName == "" {
		return fmt.Errorf("MCPCallOp: 'tool_name' param is required")
	}
	op.initTimeout = mcpParseDurationMs(params, "init_timeout_ms", 10000)
	op.callTimeout = mcpParseDurationMs(params, "call_timeout_ms", 30000)
	op.maxRetries = 3
	if s := params.GetString("max_retries", ""); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			op.maxRetries = n
		}
	}
	op.poolSize = mcpParsePoolSize(params)
	op.poolPrewarm = mcpParsePoolPrewarm(params)
	if op.poolSize > 0 && op.spec.kind != "stdio" {
		return fmt.Errorf("MCPCallOp: pool_size > 0 is only supported for transport=\"stdio\" in v1 (got transport=%q)", op.spec.kind)
	}
	if op.poolSize > 0 && op.poolPrewarm {
		mcpPoolPrewarm(op.spec, op.initTimeout, op.poolSize)
	}
	return nil
}

func (op *MCPCallOp[In, Out]) Reset() error { return nil }

func (op *MCPCallOp[In, Out]) InputFields() map[string]any {
	return map[string]any{"Input": &op.Input}
}

func (op *MCPCallOp[In, Out]) OutputFields() map[string]any {
	return map[string]any{"Result": &op.Result}
}

func (op *MCPCallOp[In, Out]) SetInputField(field string, value any) error {
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

func (op *MCPCallOp[In, Out]) ResetFields() {
	var zeroIn *In
	op.Input = zeroIn
	var zeroOut Out
	op.Result = zeroOut
}

func (op *MCPCallOp[In, Out]) Run(ctx context.Context) error {
	slog.DebugContext(ctx, "MCPCallOp.run", "run_id", dagor.RunID(ctx), "transport", op.spec.kind, "target", op.spec.label(), "tool", op.toolName)

	args, err := op.encodeArgs()
	if err != nil {
		return err
	}

	delay := 500 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt <= op.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay = min(delay*2, 30*time.Second)
		}

		outcome, callErr := op.runOnce(ctx, args)
		if callErr != nil {
			lastErr = callErr
			slog.WarnContext(ctx, "MCPCallOp.attempt_failed", "attempt", attempt+1, "of", op.maxRetries, "err", callErr)
			continue
		}
		if outcome.isToolError {
			return fmt.Errorf("MCPCallOp: tool %q reported error: %s", op.toolName, strings.TrimSpace(outcome.text))
		}
		return op.decodeResult(outcome)
	}
	return fmt.Errorf("MCPCallOp: all %d attempts failed; last error: %w", op.maxRetries+1, lastErr)
}

func (op *MCPCallOp[In, Out]) runOnce(ctx context.Context, args any) (mcpCallOutcome, error) {
	sess, err := mcpPoolAcquire(ctx, op.spec, op.initTimeout, op.poolSize)
	if err != nil {
		return mcpCallOutcome{}, err
	}
	defer func() {
		if cerr := sess.close(); cerr != nil {
			slog.WarnContext(ctx, "MCPCallOp.close_warn", "err", cerr)
		}
	}()
	return sess.callTool(ctx, op.toolName, args, op.callTimeout)
}

func (op *MCPCallOp[In, Out]) encodeArgs() (any, error) {
	if op.Input == nil {
		return map[string]any{}, nil
	}
	if f, ok := any(op.Input).(MCPArgsFormatter); ok {
		return f.FormatMCPArgs()
	}
	return *op.Input, nil
}

func (op *MCPCallOp[In, Out]) decodeResult(outcome mcpCallOutcome) error {
	if p, ok := any(&op.Result).(MCPResponseParser); ok {
		return p.ParseMCPResponse(outcome.text, outcome.structured)
	}
	if len(outcome.structured) > 0 {
		if err := json.Unmarshal(outcome.structured, &op.Result); err == nil {
			return nil
		}
	}
	return op.parseResultText(outcome.text)
}

func (op *MCPCallOp[In, Out]) parseResultText(raw string) error {
	raw = strings.TrimSpace(raw)
	switch v := any(&op.Result).(type) {
	case *string:
		*v = raw
	case *float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("expected float64, got %q: %w", raw, err)
		}
		*v = f
	case *int:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("expected int, got %q: %w", raw, err)
		}
		*v = n
	case *bool:
		switch strings.ToLower(raw) {
		case "true", "yes":
			*v = true
		case "false", "no":
			*v = false
		default:
			return fmt.Errorf("expected bool (true/false), got %q", raw)
		}
	case *[]float64:
		if raw == "" {
			*v = nil
			return nil
		}
		parts := strings.Split(raw, ",")
		s := make([]float64, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			f, err := strconv.ParseFloat(p, 64)
			if err != nil {
				return fmt.Errorf("expected []float64 CSV, got %q: %w", raw, err)
			}
			s = append(s, f)
		}
		*v = s
	case *[]int:
		if raw == "" {
			*v = nil
			return nil
		}
		parts := strings.Split(raw, ",")
		s := make([]int, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			n, err := strconv.Atoi(p)
			if err != nil {
				return fmt.Errorf("expected []int CSV, got %q: %w", raw, err)
			}
			s = append(s, n)
		}
		*v = s
	case *[]string:
		if raw == "" {
			*v = nil
			return nil
		}
		parts := strings.Split(raw, ",")
		s := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				s = append(s, p)
			}
		}
		*v = s
	case *map[string]string:
		if raw == "" {
			*v = map[string]string{}
			return nil
		}
		m := make(map[string]string)
		for _, pair := range strings.Split(raw, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			idx := strings.IndexByte(pair, '=')
			if idx < 0 {
				return fmt.Errorf("expected key=value pair, got %q", pair)
			}
			m[strings.TrimSpace(pair[:idx])] = strings.TrimSpace(pair[idx+1:])
		}
		*v = m
	default:
		if err := json.Unmarshal([]byte(raw), &op.Result); err != nil {
			return fmt.Errorf("MCPCallOp: unsupported output type %T (got %q): implement MCPResponseParser", op.Result, raw)
		}
	}
	return nil
}

func mcpSplitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func mcpParseDurationMs(p *config.Params, key string, defaultMs int64) time.Duration {
	s := p.GetString(key, "")
	if s == "" {
		return time.Duration(defaultMs) * time.Millisecond
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return time.Duration(defaultMs) * time.Millisecond
	}
	return time.Duration(n) * time.Millisecond
}

// mcpParsePoolSize reads the pool_size param. Returns 0 (no pooling) for
// missing, malformed, or non-positive values.
func mcpParsePoolSize(p *config.Params) int {
	s := p.GetString("pool_size", "")
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// mcpParsePoolPrewarm reads the pool_prewarm param. Defaults to true when the
// param is unset; only "false"/"0"/"no" (case-insensitive) disable prewarm.
func mcpParsePoolPrewarm(p *config.Params) bool {
	s := p.GetString("pool_prewarm", "")
	if s == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "false", "0", "no":
		return false
	}
	return true
}

// mcpParseTransportSpec reads transport-selection params and validates them
// for the chosen kind. opName is used as a prefix in error messages so callers
// (MCPCallOp / MCPScriptOp) get clear, contextual diagnostics.
func mcpParseTransportSpec(p *config.Params, opName string) (mcpTransportSpec, error) {
	kind := strings.ToLower(strings.TrimSpace(p.GetString("transport", "")))
	if kind == "" {
		kind = "stdio"
	}
	switch kind {
	case "stdio":
		command := p.GetString("command", "")
		if command == "" {
			return mcpTransportSpec{}, fmt.Errorf("%s: 'command' param is required for transport=\"stdio\"", opName)
		}
		if _, err := exec.LookPath(command); err != nil {
			return mcpTransportSpec{}, fmt.Errorf("%s: command %q not found on PATH: %w", opName, command, err)
		}
		args := mcpSplitCSV(p.GetString("args", ""))
		var env []string
		for _, kv := range mcpSplitCSV(p.GetString("env", "")) {
			if strings.Contains(kv, "=") {
				env = append(env, kv)
			}
		}
		return mcpTransportSpec{
			kind:    "stdio",
			command: command,
			args:    args,
			env:     env,
		}, nil
	case "http":
		raw := p.GetString("url", "")
		if raw == "" {
			return mcpTransportSpec{}, fmt.Errorf("%s: 'url' param is required for transport=\"http\"", opName)
		}
		u, err := url.Parse(raw)
		if err != nil {
			return mcpTransportSpec{}, fmt.Errorf("%s: invalid url %q: %w", opName, raw, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return mcpTransportSpec{}, fmt.Errorf("%s: url %q must use http or https scheme (got %q)", opName, raw, u.Scheme)
		}
		var headers []string
		for _, kv := range mcpSplitCSV(p.GetString("headers", "")) {
			if strings.Contains(kv, "=") {
				headers = append(headers, kv)
			}
		}
		sort.Strings(headers)
		return mcpTransportSpec{
			kind:    "http",
			url:     raw,
			headers: headers,
		}, nil
	default:
		return mcpTransportSpec{}, fmt.Errorf("%s: unknown transport %q (want \"stdio\" or \"http\")", opName, kind)
	}
}
