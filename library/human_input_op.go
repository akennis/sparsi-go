package library

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/akennis/dagor"
	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Human-in-the-loop ops pause the DAG at a vertex and ask a real person for
// input, then feed the answer into the rest of the workflow. The person is
// reached through a HumanPrompter that the host program installs on the run
// context before calling eng.Run:
//
//   - Under the generated `-mcp` server mode, wire ElicitPrompter(req.Session)
//     so the question travels to the MCP client as an `elicitation/create`
//     request and the human answers in their client UI.
//   - In one-shot CLI mode, wire StdinPrompter() so the question is asked on
//     the terminal.
//
// Elicit (and StdinPrompter's Read) blocks until the human responds, so the
// vertex naturally suspends the workflow until the answer arrives — no
// checkpoint/resume machinery is needed. Downstream vertices fire with the
// captured value exactly as they would for any other producer.

// ErrHumanDeclined is returned (wrapped) when the human explicitly declines or
// cancels a prompt rather than submitting an answer. Callers that want a
// non-fatal path can branch on errors.Is(err, ErrHumanDeclined).
var ErrHumanDeclined = errors.New("human declined or cancelled the prompt")

// HumanPrompter asks a human for input during workflow execution. All methods
// block until the human responds, the request is cancelled via ctx, or the
// human declines (returning an error wrapping ErrHumanDeclined).
type HumanPrompter interface {
	// AskText requests a free-form string answer to prompt.
	AskText(ctx context.Context, prompt string) (string, error)
	// AskBool requests a yes/no (true/false) answer to prompt.
	AskBool(ctx context.Context, prompt string) (bool, error)
	// AskChoice presents options and returns the zero-based index of the option
	// the human chose. It errors if the answer matches no option or is out of
	// range.
	AskChoice(ctx context.Context, prompt string, options []string) (int, error)
}

type humanPrompterKey struct{}

// WithHumanPrompter returns a copy of ctx carrying p, so that human-in-the-loop
// ops executed under this context can reach the human. The generated MCP server
// handler installs ElicitPrompter(req.Session); a CLI entrypoint installs
// StdinPrompter().
//
// p is wrapped in a per-context serializer so that concurrent HITL ops — dagor
// runs independent vertices in parallel — never issue overlapping requests to
// the one human: at most one AskText/AskBool/AskChoice is in flight at a time.
// This means independent HITL ops do NOT have to be sequenced in the DAG for
// correctness; each request carries its own self-describing message, so the
// human always knows what they are answering. It does NOT impose a
// deterministic order among independent HITL ops (which prompts first is up to
// the scheduler); sequence them with a DAG edge if a specific order matters.
func WithHumanPrompter(ctx context.Context, p HumanPrompter) context.Context {
	if _, already := p.(*serialPrompter); !already {
		p = &serialPrompter{inner: p}
	}
	return context.WithValue(ctx, humanPrompterKey{}, p)
}

// serialPrompter wraps a HumanPrompter so that only one request is in flight at
// a time. The lock is held for the whole call — including while blocked waiting
// on the human — which is exactly what serializes the shared human/terminal.
type serialPrompter struct {
	mu    sync.Mutex
	inner HumanPrompter
}

func (p *serialPrompter) AskText(ctx context.Context, prompt string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inner.AskText(ctx, prompt)
}

func (p *serialPrompter) AskBool(ctx context.Context, prompt string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inner.AskBool(ctx, prompt)
}

func (p *serialPrompter) AskChoice(ctx context.Context, prompt string, options []string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inner.AskChoice(ctx, prompt, options)
}

// HumanPrompterFrom extracts the HumanPrompter installed by WithHumanPrompter.
func HumanPrompterFrom(ctx context.Context) (HumanPrompter, bool) {
	p, ok := ctx.Value(humanPrompterKey{}).(HumanPrompter)
	return p, ok
}

// ---------------------------------------------------------------------------
// MCP elicitation prompter (server mode)
// ---------------------------------------------------------------------------

type elicitPrompter struct {
	ss *mcp.ServerSession
}

// ElicitPrompter builds a HumanPrompter that reaches the human by sending an
// MCP `elicitation/create` request over the given server session. This only
// works when the workflow runs as an `-mcp` server AND the connected client
// declared the elicitation capability at initialize time; otherwise the
// underlying Elicit call returns "client does not support elicitation", which
// the op surfaces as a run error.
func ElicitPrompter(ss *mcp.ServerSession) HumanPrompter {
	return &elicitPrompter{ss: ss}
}

// ask runs a single-field ("value") form elicitation and returns the submitted
// value. valueSchema is the JSON-Schema (2020-12) fragment for that one field.
func (p *elicitPrompter) ask(ctx context.Context, message string, valueSchema map[string]any) (any, error) {
	res, err := p.ss.Elicit(ctx, &mcp.ElicitParams{
		Message: message,
		RequestedSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": valueSchema},
			"required":   []any{"value"},
		},
	})
	if err != nil {
		return nil, err
	}
	if res.Action != "accept" {
		return nil, fmt.Errorf("%w (action=%q)", ErrHumanDeclined, res.Action)
	}
	v, ok := res.Content["value"]
	if !ok {
		return nil, fmt.Errorf("elicitation accepted but returned no \"value\" field")
	}
	return v, nil
}

func (p *elicitPrompter) AskText(ctx context.Context, prompt string) (string, error) {
	v, err := p.ask(ctx, prompt, map[string]any{"type": "string"})
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("expected string answer, got %T", v)
	}
	return s, nil
}

func (p *elicitPrompter) AskBool(ctx context.Context, prompt string) (bool, error) {
	v, err := p.ask(ctx, prompt, map[string]any{"type": "boolean"})
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("expected boolean answer, got %T", v)
	}
	return b, nil
}

func (p *elicitPrompter) AskChoice(ctx context.Context, prompt string, options []string) (int, error) {
	enum := make([]any, len(options))
	for i, o := range options {
		enum[i] = o
	}
	v, err := p.ask(ctx, prompt, map[string]any{"type": "string", "enum": enum})
	if err != nil {
		return 0, err
	}
	s, ok := v.(string)
	if !ok {
		return 0, fmt.Errorf("expected string choice, got %T", v)
	}
	for i, o := range options {
		if o == s {
			return i, nil
		}
	}
	return 0, fmt.Errorf("choice %q is not one of the offered options", s)
}

// ---------------------------------------------------------------------------
// Stdin prompter (CLI mode)
// ---------------------------------------------------------------------------

type stdinPrompter struct {
	mu sync.Mutex // serialize concurrent prompts sharing the one terminal
	r  *bufio.Reader
	w  io.Writer
}

// StdinPrompter builds a HumanPrompter that asks questions on the terminal:
// prompts are written to stderr and answers read a line at a time from stdin.
// A mutex serializes prompts so parallel DAG branches don't interleave. Use it
// for the one-shot CLI entrypoint; do NOT use it in `-mcp` server mode, where
// stdin/stdout carry the MCP JSON stream.
func StdinPrompter() HumanPrompter {
	return &stdinPrompter{r: bufio.NewReader(os.Stdin), w: os.Stderr}
}

// stdinRetries is how many times the stdin prompter re-asks after an
// unparseable answer before giving up, so a single typo doesn't fail the run.
const stdinRetries = 3

// readLine reads and trims one line. Caller holds p.mu.
func (p *stdinPrompter) readLine() (string, error) {
	line, err := p.r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if err == io.EOF && strings.TrimSpace(line) == "" {
		return "", fmt.Errorf("no input available on stdin")
	}
	return strings.TrimSpace(line), nil
}

func (p *stdinPrompter) AskText(_ context.Context, prompt string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.w, "%s\n> ", prompt)
	return p.readLine()
}

func (p *stdinPrompter) AskBool(_ context.Context, prompt string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for attempt := 0; attempt < stdinRetries; attempt++ {
		fmt.Fprintf(p.w, "%s [y/n]\n> ", prompt)
		s, err := p.readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(s) {
		case "y", "yes", "true", "t", "1":
			return true, nil
		case "n", "no", "false", "f", "0":
			return false, nil
		}
		fmt.Fprintf(p.w, "please answer yes or no.\n")
	}
	return false, fmt.Errorf("no valid yes/no answer after %d attempts", stdinRetries)
}

func (p *stdinPrompter) AskChoice(_ context.Context, prompt string, options []string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for attempt := 0; attempt < stdinRetries; attempt++ {
		fmt.Fprintf(p.w, "%s\n", prompt)
		for i, o := range options {
			fmt.Fprintf(p.w, "  %d) %s\n", i+1, o)
		}
		fmt.Fprintf(p.w, "> ")
		s, err := p.readLine()
		if err != nil {
			return 0, err
		}
		// Accept a 1-based number...
		if n, convErr := strconv.Atoi(s); convErr == nil {
			if n >= 1 && n <= len(options) {
				return n - 1, nil
			}
			fmt.Fprintf(p.w, "please enter a number between 1 and %d.\n", len(options))
			continue
		}
		// ...or an exact (case-insensitive) option string.
		for i, o := range options {
			if strings.EqualFold(strings.TrimSpace(o), s) {
				return i, nil
			}
		}
		fmt.Fprintf(p.w, "unrecognized choice %q.\n", s)
	}
	return 0, fmt.Errorf("no valid choice after %d attempts", stdinRetries)
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// humanMessage combines the static `prompt` param with the optional wired
// Input (dynamic content to show the human), erroring if both are empty.
func humanMessage(promptParam string, input *string) (string, error) {
	var parts []string
	if p := strings.TrimSpace(promptParam); p != "" {
		parts = append(parts, p)
	}
	if input != nil {
		if s := strings.TrimSpace(*input); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("no message: set the \"prompt\" param or wire an Input")
	}
	return strings.Join(parts, "\n\n"), nil
}

// ---------------------------------------------------------------------------
// HumanInputOp — free-form string
// ---------------------------------------------------------------------------

const HumanInputOpDescription = `HumanInputOp: pause the workflow and ask a human for a free-form text answer, emitting what they type.
  Requires a HumanPrompter on the run context (ElicitPrompter under -mcp with an elicitation-capable client, or StdinPrompter in CLI mode).
  Params:   prompt string — the question shown to the human (optional if an Input is wired).
  Inputs:   Input *string — optional dynamic content appended below the prompt (e.g. the text to review). If unwired, only the prompt is shown.
  Outputs:  Result string — the human's typed answer (may be empty if they submit nothing).`

// HumanInputOp asks a human for a free-form string and emits it. Wire Input to
// show dynamic content (e.g. a draft to approve) beneath the static prompt.
type HumanInputOp struct {
	Input  *string `dag:"input"`
	Result string  `dag:"output"`

	prompt string
}

func (op *HumanInputOp) Setup(params *config.Params) error {
	op.prompt = params.GetString("prompt", "")
	return nil
}

func (op *HumanInputOp) Reset() error { return nil }

func (op *HumanInputOp) Run(ctx context.Context) error {
	p, ok := HumanPrompterFrom(ctx)
	if !ok {
		return fmt.Errorf("HumanInputOp: no HumanPrompter on context (run under -mcp with an elicitation-capable client, or install StdinPrompter in CLI mode)")
	}
	msg, err := humanMessage(op.prompt, op.Input)
	if err != nil {
		return fmt.Errorf("HumanInputOp: %w", err)
	}
	ans, err := p.AskText(ctx, msg)
	if err != nil {
		return fmt.Errorf("HumanInputOp: %w", err)
	}
	op.Result = ans
	slog.DebugContext(ctx, "HumanInputOp.run", "run_id", dagor.RunID(ctx), "result_len", len(ans))
	return nil
}

// ---------------------------------------------------------------------------
// HumanBoolInputOp — yes/no → bool
// ---------------------------------------------------------------------------

const HumanBoolInputOpDescription = `HumanBoolInputOp: pause the workflow and ask a human a yes/no (true/false) question, emitting a boolean.
  Requires a HumanPrompter on the run context (ElicitPrompter under -mcp with an elicitation-capable client, or StdinPrompter in CLI mode).
  Under -mcp the client renders a boolean toggle; on the terminal any of yes/y/true/t/1 → true and no/n/false/f/0 → false.
  Params:   prompt string — the yes/no question shown to the human (optional if an Input is wired).
  Inputs:   Input *string — optional dynamic content appended below the prompt (e.g. an action to confirm). If unwired, only the prompt is shown.
  Outputs:  Result bool — the human's answer. Feed it to a predicate/If op to gate downstream branches.`

// HumanBoolInputOp asks a yes/no question and emits a bool.
type HumanBoolInputOp struct {
	Input  *string `dag:"input"`
	Result bool    `dag:"output"`

	prompt string
}

func (op *HumanBoolInputOp) Setup(params *config.Params) error {
	op.prompt = params.GetString("prompt", "")
	return nil
}

func (op *HumanBoolInputOp) Reset() error { return nil }

func (op *HumanBoolInputOp) Run(ctx context.Context) error {
	p, ok := HumanPrompterFrom(ctx)
	if !ok {
		return fmt.Errorf("HumanBoolInputOp: no HumanPrompter on context (run under -mcp with an elicitation-capable client, or install StdinPrompter in CLI mode)")
	}
	msg, err := humanMessage(op.prompt, op.Input)
	if err != nil {
		return fmt.Errorf("HumanBoolInputOp: %w", err)
	}
	ans, err := p.AskBool(ctx, msg)
	if err != nil {
		return fmt.Errorf("HumanBoolInputOp: %w", err)
	}
	op.Result = ans
	slog.DebugContext(ctx, "HumanBoolInputOp.run", "run_id", dagor.RunID(ctx), "result", ans)
	return nil
}

// ---------------------------------------------------------------------------
// HumanChoiceInputOp — pick from a list → int index
// ---------------------------------------------------------------------------

const HumanChoiceInputOpDescription = `HumanChoiceInputOp: pause the workflow and ask a human to pick one option from a fixed list, emitting the zero-based index of their choice.
  Requires a HumanPrompter on the run context (ElicitPrompter under -mcp with an elicitation-capable client, or StdinPrompter in CLI mode).
  Errors if the human's answer matches no option or is out of range (e.g. a non-compliant client or a bad terminal entry).
  Params:   options string — comma-separated list of at least 2 choices (e.g. "approve,revise,reject").
            prompt string — the question shown above the options (optional if an Input is wired).
  Inputs:   Input *string — optional dynamic content appended below the prompt. If unwired, only the prompt is shown.
  Outputs:  Result int — zero-based index of the chosen option (0 for the first option). Feed it to SelectString/SwitchString or SliceAt to map the index to a value.`

// HumanChoiceInputOp asks a human to pick one of a fixed set of options and
// emits the zero-based index of the chosen option.
type HumanChoiceInputOp struct {
	Input  *string `dag:"input"`
	Result int     `dag:"output"`

	prompt  string
	options []string
}

func (op *HumanChoiceInputOp) Setup(params *config.Params) error {
	op.prompt = params.GetString("prompt", "")
	raw := params.GetString("options", "")
	if raw == "" {
		return fmt.Errorf("HumanChoiceInputOp: \"options\" param is required")
	}
	op.options = nil // reset before appending so pool reuse doesn't accumulate
	for _, o := range strings.Split(raw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			op.options = append(op.options, o)
		}
	}
	if len(op.options) < 2 {
		return fmt.Errorf("HumanChoiceInputOp: at least 2 options required, got %d", len(op.options))
	}
	return nil
}

func (op *HumanChoiceInputOp) Reset() error { return nil }

func (op *HumanChoiceInputOp) Run(ctx context.Context) error {
	p, ok := HumanPrompterFrom(ctx)
	if !ok {
		return fmt.Errorf("HumanChoiceInputOp: no HumanPrompter on context (run under -mcp with an elicitation-capable client, or install StdinPrompter in CLI mode)")
	}
	msg, err := humanMessage(op.prompt, op.Input)
	if err != nil {
		return fmt.Errorf("HumanChoiceInputOp: %w", err)
	}
	idx, err := p.AskChoice(ctx, msg, op.options)
	if err != nil {
		return fmt.Errorf("HumanChoiceInputOp: %w", err)
	}
	if idx < 0 || idx >= len(op.options) {
		return fmt.Errorf("HumanChoiceInputOp: chosen index %d out of range [0,%d)", idx, len(op.options))
	}
	op.Result = idx
	slog.DebugContext(ctx, "HumanChoiceInputOp.run", "run_id", dagor.RunID(ctx), "index", idx, "choice", op.options[idx])
	return nil
}

func init() {
	operator.RegisterOp[HumanInputOp]()
	operator.RegisterOp[HumanBoolInputOp]()
	operator.RegisterOp[HumanChoiceInputOp]()
}
