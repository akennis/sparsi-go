package library

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpSession owns a connected MCP client and its underlying transport. The
// transport is responsible for shutting down its backing resource (subprocess
// for stdio, HTTP connections for streamable) when the session is closed.
type mcpSession struct {
	client  *mcp.Client
	session *mcp.ClientSession
}

// mcpTransportSpec describes how to construct a transport for a single MCP
// session. Vertices populate this in Setup; the pool keys on it; the session
// constructor turns it into a concrete mcp.Transport.
type mcpTransportSpec struct {
	kind string // "stdio" | "http"

	// stdio fields
	command string
	args    []string
	env     []string

	// http fields
	url     string
	headers []string // sorted KEY=VALUE pairs
}

// buildTransport returns the SDK transport implied by the spec. For stdio it
// constructs a fresh *exec.Cmd bound to ctx so the subprocess inherits ctx
// cancellation. For http it builds a StreamableClientTransport with an
// HTTPClient that injects the configured static headers on every request.
func (s mcpTransportSpec) buildTransport(ctx context.Context) (mcp.Transport, error) {
	switch s.kind {
	case "stdio":
		cmd := exec.CommandContext(ctx, s.command, s.args...)
		if len(s.env) > 0 {
			cmd.Env = append(os.Environ(), s.env...)
		}
		return &mcp.CommandTransport{Command: cmd}, nil
	case "http":
		return &mcp.StreamableClientTransport{
			Endpoint:   s.url,
			HTTPClient: httpClientWithHeaders(s.headers),
		}, nil
	default:
		return nil, fmt.Errorf("mcpTransportSpec: unknown kind %q", s.kind)
	}
}

// label returns a human-readable identifier for use in log/error messages.
func (s mcpTransportSpec) label() string {
	if s.kind == "http" {
		return s.url
	}
	return s.command
}

// httpClientWithHeaders returns an *http.Client that injects each KEY=VALUE
// header in headers on every request. When headers is empty it returns
// http.DefaultClient unchanged so connection pooling remains shared.
func httpClientWithHeaders(headers []string) *http.Client {
	if len(headers) == 0 {
		return http.DefaultClient
	}
	parsed := make([][2]string, 0, len(headers))
	for _, h := range headers {
		idx := strings.IndexByte(h, '=')
		if idx <= 0 {
			continue
		}
		parsed = append(parsed, [2]string{
			strings.TrimSpace(h[:idx]),
			strings.TrimSpace(h[idx+1:]),
		})
	}
	if len(parsed) == 0 {
		return http.DefaultClient
	}
	return &http.Client{Transport: &headerInjectingTransport{base: http.DefaultTransport, headers: parsed}}
}

// headerInjectingTransport wraps an http.RoundTripper and adds a fixed set of
// headers to every outbound request. Existing headers on the request are not
// overwritten — this lets the SDK still set protocol headers (e.g.
// Mcp-Session-Id) without our static auth layer stomping on them.
type headerInjectingTransport struct {
	base    http.RoundTripper
	headers [][2]string
}

func (t *headerInjectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for _, kv := range t.headers {
		if clone.Header.Get(kv[0]) == "" {
			clone.Header.Set(kv[0], kv[1])
		}
	}
	return t.base.RoundTrip(clone)
}

// startMCPSessionFromSpec builds the transport described by spec and connects
// an MCP client over it. Pass initTimeout=0 for no handshake timeout.
func startMCPSessionFromSpec(ctx context.Context, spec mcpTransportSpec, initTimeout time.Duration) (*mcpSession, error) {
	transport, err := spec.buildTransport(ctx)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "clawdag-go", Version: "0.0.0"}, nil)

	connectCtx := ctx
	if initTimeout > 0 {
		var cancel context.CancelFunc
		connectCtx, cancel = context.WithTimeout(ctx, initTimeout)
		defer cancel()
	}
	sess, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", spec.label(), err)
	}
	return &mcpSession{client: client, session: sess}, nil
}

// mcpCallOutcome carries a tool-call result after content has been split into
// text vs. structured forms.
type mcpCallOutcome struct {
	text        string
	structured  json.RawMessage // nil if the tool emitted none
	isToolError bool
}

// callTool invokes the named tool with the given arguments. Returns a
// transport error on protocol failure; tool-level errors surface via
// mcpCallOutcome.isToolError.
func (s *mcpSession) callTool(ctx context.Context, name string, args any, callTimeout time.Duration) (mcpCallOutcome, error) {
	callCtx := ctx
	if callTimeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, callTimeout)
		defer cancel()
	}
	res, err := s.session.CallTool(callCtx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return mcpCallOutcome{}, err
	}

	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(tc.Text)
		}
		// Non-text content (Image/Audio/EmbeddedResource/ResourceLink) is
		// ignored in v1; consumers that need it must implement
		// MCPResponseParser and read StructuredContent.
	}

	out := mcpCallOutcome{
		text:        sb.String(),
		isToolError: res.IsError,
	}
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return mcpCallOutcome{}, fmt.Errorf("marshal structured content: %w", err)
		}
		out.structured = b
	}
	return out, nil
}

// close shuts down the client session and (via the transport) its backing
// resource. Safe to call on a zero-value or partially-initialized session.
func (s *mcpSession) close() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Close()
}
