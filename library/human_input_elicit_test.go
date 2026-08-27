package library

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectElicit wires an in-memory MCP client/server pair whose client answers
// elicitation requests with respond, and returns an ElicitPrompter over the
// live server session. This exercises the real server-mode path, including the
// SDK's validation of returned content against the schema our prompter builds.
func connectElicit(t *testing.T, respond func(*mcp.ElicitParams) *mcp.ElicitResult) HumanPrompter {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "v0"}, nil)
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	cli := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return respond(req.Params), nil
		},
	})
	cs, err := cli.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	return ElicitPrompter(ss)
}

func TestElicitPrompter_AskText(t *testing.T) {
	p := connectElicit(t, func(*mcp.ElicitParams) *mcp.ElicitResult {
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"value": "typed answer"}}
	})
	got, err := p.AskText(context.Background(), "your name?")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "typed answer" {
		t.Fatalf("got %q", got)
	}
}

func TestElicitPrompter_AskBool(t *testing.T) {
	p := connectElicit(t, func(*mcp.ElicitParams) *mcp.ElicitResult {
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"value": true}}
	})
	got, err := p.AskBool(context.Background(), "proceed?")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !got {
		t.Fatal("want true")
	}
}

func TestElicitPrompter_AskChoiceMapsToIndex(t *testing.T) {
	opts := []string{"approve", "revise", "reject"}
	p := connectElicit(t, func(*mcp.ElicitParams) *mcp.ElicitResult {
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"value": "reject"}}
	})
	idx, err := p.AskChoice(context.Background(), "pick", opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if idx != 2 {
		t.Fatalf("idx = %d, want 2", idx)
	}
}

func TestElicitPrompter_Decline(t *testing.T) {
	p := connectElicit(t, func(*mcp.ElicitParams) *mcp.ElicitResult {
		return &mcp.ElicitResult{Action: "decline"}
	})
	_, err := p.AskText(context.Background(), "?")
	if !errors.Is(err, ErrHumanDeclined) {
		t.Fatalf("err = %v, want ErrHumanDeclined", err)
	}
}
