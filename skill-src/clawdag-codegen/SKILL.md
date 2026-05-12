---
name: clawdag-codegen
description: Generate a compilable Go workflow executable from an approved clawdag-go DAG design
version: 0.1.0
library_version: github.com/akennis/clawdag-go v0.1.0
triggers: [clawdag codegen, generate workflow code, implement dag design]
input:
  design:     {type: string, description: "Approved DAG design (output of clawdag-design)", required: true}
  output_dir: {type: string, description: "Directory to write generated Go program", required: true}
  task:       {type: string, description: "Original task description", required: false}
---

# Context

You are generating Go source code for a clawdag-go DAG workflow from an approved design.
The output must compile with `go build` and run correctly.

Read the following references before writing any code:
1. `references/library.md` — all 89 op descriptions with exact field names and types
2. `references/dagor-api.md` — operator boilerplate, builder DSL, config.Params API, logging, coalesce/map rules
3. `references/examples/README.md` — pick the most structurally similar example
4. Read that example file in `references/examples/`

# Steps

1. Read all three references as listed above.
2. Implement the approved design exactly — no improvisation, no added ops.
3. Create `<output_dir>/` and write the complete Go source to `<output_dir>/main.go`.
4. Write `<output_dir>/go.mod` with this content (substitute the actual Go version):
   ```
   module solution

   go <version>

   require (
       github.com/akennis/clawdag-go v0.0.0-00010101000000-000000000000
       github.com/wwz16/dagor v0.0.0
   )

   replace (
       github.com/akennis/clawdag-go => github.com/akennis/clawdag-go v0.0.0-00010101000000-000000000000
       github.com/wwz16/dagor => github.com/akennis/dagor v0.0.0
   )
   ```
5. Run `go get github.com/akennis/clawdag-go@init` in `<output_dir>` — this resolves the `init` branch to its current commit pseudo-version and updates `go.mod` automatically. Remove the `replace` directive for `clawdag-go` that was written in step 4 (it is no longer needed after this step).
6. Run `go mod tidy` in `<output_dir>` — this resolves all remaining dependencies (ants, etc.) and writes `go.sum`.
7. Run `go build ./...` in `<output_dir>` to compile.
8. If the build fails, read the error output, fix `main.go`, and re-run step 7.
9. Repeat until the build exits 0.

# Implementation rules

## Operator boilerplate contract
Every custom op must implement `Setup`, `Reset`, `Run`, `InputFields`, `OutputFields`,
`SetInputField`, and `ResetFields`. The first three methods are defined on the operator;
the last four are the IOperator interface. Library ops with `dag:"input"` / `dag:"output"` tags
have these generated — do NOT write them manually for library ops.

## Trailing commas
Go requires a trailing comma after the LAST element of every multi-line composite literal.
Missing it is a compile error.
```
// WRONG:              RIGHT:
map[string]any{        map[string]any{
  "a": 1,               "a": 1,
  "b": 2                "b": 2,   // ← required
}                      }
```

## Wire naming
Wire names are arbitrary strings you assign in `Output("FieldName", "wire_name")` then reference
in `Input("FieldName", "wire_name")`. They are NOT "vertex.Field" syntax.

## ConditionInput rule
When a predicate needs a wire that the op itself does not consume, use
`.ConditionInput("wire_name")` on the vertex. Do NOT add a dummy field to the op struct.

## PassthroughWire rule
Use `.PassthroughWire("OutputField", "source_wire")` to inherit an upstream wire's value when
the vertex is skipped, so a downstream CoalesceOp sees a non-nil slot.

## Predicate wire name rule
Predicates receive WIRE NAMES as keys, never op field names or output field names.
```
// WRONG: inputs["City"]           // "City" is an op field name
// WRONG: inputs["Result"]         // "Result" is an output field
// RIGHT: inputs["lookup_result"]  // wire name from Input("City", "lookup_result")
```

## CoalesceOp vs SelectStringOp
- **CoalesceOp** (+ `Merge(config.MergeCoalesce)`): use when upstream branches may be SKIPPED.
- **SelectStringOp**: use when BOTH inputs always exist and the choice is a runtime bool wire.
Never use CoalesceOp when neither branch is conditional.

## Value injection rule

There are exactly two ways a value may enter the DAG. Every value falls into one of these cases — no exceptions.

**True constants** — values that are compile-time literals, never differ between runs — use `RegisterConst`:

```go
// Before buildGraph: register a named factory that always emits this value
clawdag.RegisterConst[int]("CountThreshold", 5)
clawdag.RegisterConst[string]("DefaultMode", "fast")

// In the graph builder — output field is always "Result"
graph.NewBuilder("my_graph").
    Vertex("threshold").Op("CountThreshold").Output("Result", "threshold_wire").
    ...
```

`ConstOp` (the backing type) has no params and no inputs; the value is captured at registration time.
Use the named import `clawdag "github.com/akennis/clawdag-go/library"` to call `clawdag.RegisterConst`.

**Everything else** — CLI flags, user text, env values, runtime-computed values, or anything that could
vary between runs — MUST be injected via `context.WithValue` using a dedicated unexported key type.
The DAG reads these values through a `ContextValOp` vertex (registered via `builtin.ContextValFactory`).
`eng.SetInput` is **prohibited**.

```go
// WRONG:
eng.SetInput("query_wire", userText)

// RIGHT — three steps:
// 1. Declare key type and register factory (before buildGraph)
type ctxKey string
const queryKey ctxKey = "query"
operator.RegisterOpFactory("QueryInputOp", builtin.ContextValFactory[string](queryKey))

// 2. Wire it in the graph builder — output field is always "Result"
graph.NewBuilder("my_graph").
    Vertex("query_input").Op("QueryInputOp").Output("Result", "query_wire").
    ...

// 3. Inject value into context before eng.Run
ctx = context.WithValue(ctx, queryKey, userText)
```

## Env var resolution in main()
ALL `os.Getenv` calls MUST use literal string names in `main()`.
Never call `os.Getenv` inside an operator's `Setup` or `Run`.

## CLI flag parsing
Parse all user inputs from CLI flags in `main()` using the `flag` package. Validate required flags
before building the graph. Generated programs are plain CLI tools — no server modes or HTTP handlers.

```go
input := flag.String("input", "", "input text to process")
flag.Parse()
if *input == "" { log.Fatal("--input is required") }
// then: context.WithValue, buildGraph, eng.Run
```

## MCP transport selection
MCP vertices accept a `transport` param of either `"stdio"` (default) or `"http"`. Stdio
vertices require `command` and accept optional `args` / `env`. HTTP vertices require `url`
and accept optional `headers` (CSV `KEY=VALUE` pairs injected on every request — typical
use is a Bearer token: `headers: "Authorization=Bearer ${TOKEN}"`). Default to `"stdio"`
for any local server (npx/uvx); use `"http"` only when the user is explicitly targeting a
remote MCP endpoint.

## MCP pool lifecycle
Pooling applies **only to `transport: "stdio"` MCP vertices in v1.** Setup rejects
`pool_size > 0` for `transport: "http"`. When any stdio MCP vertex sets `pool_size > 0`
(warm-replenish pool for `MCPCallOp` / `MCPScriptOp`), `main()` MUST defer
`library.ShutdownMCPPool(context.Background())` after the engine pool release so pre-started
subprocesses drain on exit:
```go
defer pool.Release()
defer library.ShutdownMCPPool(context.Background())
```
Use the named import `clawdag "github.com/akennis/clawdag-go/library"` (or `library` alias)
to call `ShutdownMCPPool`. Skip the defer when no stdio MCP vertex sets `pool_size`.

## Custom MCP argument and response shapes
The default `MCPCallOp` Out dispatch handles `string`, `float64`, `int`, `bool`, `[]string`,
`[]float64`, `[]int`, `map[string]string`, and any struct decodable via `json.Unmarshal`
(structured content preferred when the server emits it). When the tool's argument schema
doesn't match the natural JSON shape of the `In` struct, implement
`FormatMCPArgs() (any, error)` on `*In` (the `library.MCPArgsFormatter` interface). When
the response cannot be decoded by the default dispatch, implement
`ParseMCPResponse(text string, structured json.RawMessage) error` on `*Out`
(the `library.MCPResponseParser` interface). Inside `MCPScriptOp` scripts, recover from
anticipated tool errors via `errors.As(&toolErr)` against `*library.MCPToolError`;
transport / I/O failures surface as their underlying error.

## Known library gaps
Write these as inline custom ops when needed:

**String truncation** — no library op caps string length. Write a custom `StringTruncateOp` when
passing large text (e.g. a fetched web page) to AI ops to stay within context limits.

## Custom AI compute ops
`AIComputeOp[In, Out]` cannot be used directly in the graph. Embed it in a named concrete struct:
```go
type ScoreOp struct { clawdag.AIComputeOp[string, float64] }
func init() { operator.RegisterOp[ScoreOp]() }
```
Use `clawdag "github.com/akennis/clawdag-go/library"` as the named import when embedding
`AIComputeOp`. When `Out` is a struct, implement `ExpectedFormat() string` and
`ParseAIResponse(string) error` on `*Out` to replace the default format prompt and parser.

### Prompt precision: design for first-try parse success
Every retry is an extra API call. `operation` + `ExpectedFormat()` together
must fully specify what the model emits so that `ParseAIResponse` succeeds on
turn 1; self-repair is a *safety net* for residual misses, not a substitute
for a precise prompt.

`ExpectedFormat()` should pin down, at minimum:
- the **exact shape** (single token, CSV, tag-wrapped, JSON envelope, …);
- the **value domain** when applicable (range, enum, regex);
- the **prose policy** (e.g. "No prose, no markdown, no surrounding whitespace");
- a **literal example** of a valid response when the shape is non-obvious.

Examples:
- Good: `"Reply with a single float in [0, 1]. No prose."`
- Good: `"Reply with one of: bug, feature, question. No prose, no quotes."`
- Good: `"Reply wrapped as <sum>N</sum> where N is the integer sum. Example: <sum>15</sum>. No surrounding text."`
- Weak: `"Return the score."` — no shape, no domain → forces retries.

When `operation` + `ExpectedFormat()` are tight, the default `max_retries: 3`
typically never fires. If a particular op routinely needs repair turns,
tighten the prompt first; raise `max_retries` only after that.

### In-conversation self-repair (preferred over wrapping AI ops with `WithRepair`)
`ParseAIResponse` can opt the op into **in-conversation repair** by returning
`*clawdag.ErrRepairable{Prompt, Cause}`. When it does, `AIComputeOp.Run` keeps
the same conversation open: it appends the model's prior response as an
assistant turn, sends `ErrRepairable.Prompt` as the next user turn, and re-asks
within the same `max_retries` budget. The model retains its prior reasoning
context, so the correction is typically one short follow-up turn — no second
cold call, no need to wrap the AI op with `WithRepair`.

```go
// Out type validates its own response; returns *ErrRepairable on fixable misses.
type ScoreOut struct{ Value float64 }

func (o *ScoreOut) ExpectedFormat() string {
    return "Reply with a single float in [0, 1]. No prose."
}

func (o *ScoreOut) ParseAIResponse(response string) error {
    f, err := strconv.ParseFloat(strings.TrimSpace(response), 64)
    if err != nil {
        return &clawdag.ErrRepairable{
            Prompt: "Your last response was not a number. Reply with one float in [0, 1].",
            Cause:  err,
        }
    }
    if f < 0 || f > 1 {
        return &clawdag.ErrRepairable{
            Prompt: fmt.Sprintf("Your last response %v is outside [0, 1]. Clamp it and reply with just the number.", f),
            Cause:  errors.New("out of range"),
        }
    }
    o.Value = f
    return nil
}
```

Non-`*ErrRepairable` errors from `ParseAIResponse` continue to use the legacy
single-shot retry (fresh prompt + previous-response feedback) — switch to
`*ErrRepairable` when threading the conversation gives the model more useful
context than re-asking from scratch.

**Prompt-content rule.** Unlike `WithRepair` prompts (which must be
self-contained because re-`Run` starts cold), an AI op's `ErrRepairable.Prompt`
is a **conversational correction** sent as a follow-up turn. The model still
has its prior reasoning in context, so prompts should be short and refer to
"your last response":

- Good (AI op): `"Your last response %v is outside [0, 1]. Reply with just the clamped number."`
- Wrong (AI op): restating the entire task + input + schema — wastes tokens and confuses the turn.
- For `WithRepair`: the opposite — always restate input + error + schema, because `Run` re-fires from scratch.

**Retry budget.** `max_retries` caps the *total* attempts in `Run` — stateless
retries and conversational repair turns share one counter. There is no separate
repair budget.

**Mode is sticky.** Once `ParseAIResponse` returns `*ErrRepairable` for the
first time, the op stays in conversational mode for the remainder of `Run`. A
subsequent envelope/format failure (e.g. malformed JSON in reasoning mode) is
turned into a corrective user turn on the same conversation rather than a
fresh prompt with the retry template.

**Reasoning mode.** When the op is run with a logger attached (`{result,
reasoning}` envelope), the system prompt persists across repair turns — the
model still emits the envelope shape on the follow-up. Do not restate the
envelope rule in `ErrRepairable.Prompt`.

**Token cost.** Each repair turn re-sends the full prior history. Keep
`ParseAIResponse` validations tight (one or two `*ErrRepairable` returns per
Out type, each with a short corrective prompt) so the typical recovery is one
follow-up turn.

Use this **instead of** wrapping an AI op with `WithRepair`. Reserve
`WithRepair` for *deterministic* ops at the input boundary (see above).

## AIClientFactory — optional enterprise credential routing

Every AI op sources its SDK client from a `library.AIClientFactory`. The default
(`library.EnvAIClientFactory`) reads `CLAUDE_API_KEY` / `GEMINI_API_KEY` from the
process environment. **Do not** emit factory wiring unless the approved design
explicitly calls for non-env credentials (Vault, Secrets Manager, workload
identity, multi-tenant routing, egress proxy). When in doubt, omit it.

Two patterns appear in designs that require it:

**Process-wide factory.** A single factory used by every AI op. Register once
in `main()` BEFORE building the graph:

```go
func main() {
    library.SetDefaultAIClientFactory(&vaultFactory{addr: os.Getenv("VAULT_ADDR")})
    // ... pool, buildGraph, eng.Run ...
}
```

**Per-vertex factory.** Multiple factories selected per-vertex via the
`client_factory_id` param. Register named factories in `main()`:

```go
library.RegisterAIClientFactory("tenant-a", &tenantFactory{tenant: "a"})
library.RegisterAIClientFactory("tenant-b", &tenantFactory{tenant: "b"})
```

Then the design supplies `client_factory_id` and `credential_ref` as vertex
params:

```go
Vertex("classify").Op("AIBoolOp").
    Params(map[string]string{
        "predicate":         "is this text spam?",
        "client_factory_id": "tenant-a",                  // selects registered factory
        "credential_ref":    "secret/tenant-a/anthropic", // forwarded verbatim
    }).
    Input("Input", "ticket_text").
    Output("Result", "is_spam")
```

Rules:
- `credential_ref` is opaque to the library — the factory decides what it means
  (Vault path, tenant id, region). Empty = factory default.
- `client_factory_id` is optional; omit it on vertices that should use the
  process default.
- The factory type must be defined in `main.go` (or a sibling file in
  `<output_dir>/`) and implement both `Anthropic(ctx, ref)` and
  `Gemini(ctx, ref)` methods returning `*anthropic.Client` / `*genai.Client`.
  Both methods receive a context bounded by `api_factory_timeout_ms` (default
  30 s); factories that do network I/O MUST honor `ctx.Done()`.
- `api_factory_timeout_ms` is an optional vertex param (string, ms) that caps
  the factory credential lookup at Setup; emit it only when the design's
  vertex line specifies it. `"0"` disables the deadline.
- Use the named import `clawdag "github.com/akennis/clawdag-go/library"` (or
  `library`) to call `SetDefaultAIClientFactory` / `RegisterAIClientFactory`.

When the design does NOT mention enterprise credential routing, generate code
exactly as before — no factory imports, no registration in `main()`, no
`credential_ref` / `client_factory_id` on vertices.

## AI recovery wrapper (WithRepair)
When a deterministic op may fail on structurally-fixable bad input (malformed JSON,
near-miss enum, missing field, schema-violating record), wrap it via
`clawdag.RegisterWithRepair` to give it bounded LLM-driven retry. The inner op opts in
by returning `*clawdag.ErrRepairable{Prompt, Cause}`; the input type opts in by
implementing `clawdag.RepairableInput` (`UnmarshalRepair(string) error`).

**Where it belongs in the DAG.** WithRepair is most suitable at the **upstream
boundary** — wrap the op that first ingests outside input (user text, fetched
payloads, untrusted JSON, third-party API responses) so the workflow validates and,
if necessary, repairs that input before anything downstream depends on it. Once a
value has passed a WithRepair stage, downstream vertices can treat it as well-formed
and skip defensive re-parsing.

```go
// 1. Inner op returns *clawdag.ErrRepairable on repairable failures.
//    Prompt MUST be self-contained — include the current input verbatim,
//    the validation error, and the exact expected response shape.
func (op *ParseTicketOp) Run(_ context.Context) error {
    if err := json.Unmarshal([]byte(op.Raw.Text), &op.Result); err != nil {
        return &clawdag.ErrRepairable{
            Prompt: fmt.Sprintf("Below is invalid ticket JSON (error: %v). %s\n\nInput:\n%s\n\nOutput corrected JSON only.",
                err, ticketSchemaSpec, op.Raw.Text),
            Cause:  err,
        }
    }
    return nil
}

// 2. Input type implements RepairableInput so the wrapper can deserialize
//    the LLM's response back into a typed value.
type TicketRaw struct{ Text string }
func (t *TicketRaw) UnmarshalRepair(s string) error { t.Text = strings.TrimSpace(s); return nil }

// 3. Register the wrapped op from init() under a distinct name.
func init() {
    clawdag.RegisterWithRepair[*ParseTicketOp](
        "ParseTicketRepair",
        func() *ParseTicketOp { return &ParseTicketOp{} },
        clawdag.RepairConfig{
            InputField:   "Raw",   // required: the inner field the LLM may mutate
            MaxAttempts:  3,
            PromptPrefix: "You are a strict JSON corrector. Output corrected JSON only.\n\n",
        },
    )
}
```

Wire the wrapped vertex by its **registered name** (`"ParseTicketRepair"`), not the
inner type name. Input/output field names match the inner op exactly:
```go
Vertex("parse").Op("ParseTicketRepair").Input("Raw", "raw_wire").Output("Result", "parsed_wire")
```

Use the `clawdag "github.com/akennis/clawdag-go/library"` named import. When the
field to repair is a struct (not a string wrapper), have the struct's
`UnmarshalRepair` delegate to `xml.Unmarshal` — XML is preferred over JSON for
record-shaped repair payloads. See `references/examples/with-repair.go` for both
string-target and struct-target stages in one workflow.

**Inner op MUST be idempotent or pure** — repair re-executes `Run` with mutated
input. Do not wrap ops that have side effects (DB writes, network mutations,
file deletes).

## Required imports
```go
// Standard library
"log/slog"    // structured logging
"os"          // os.Stderr for slog handler
"context"     // context.WithValue, context.WithTimeout
"flag"        // CLI flag parsing

// clawdag-go library
_ "github.com/akennis/clawdag-go/library"     // library ops — always include (triggers init)
                                              // use named import when calling RegisterConst or embedding AIComputeOp:
                                              //   clawdag "github.com/akennis/clawdag-go/library"

// dagor ecosystem (see references/dagor-api.md for per-package details)
"github.com/panjf2000/ants/v2"               // goroutine pool
"github.com/wwz16/dagor"                     // NewEngine, WithReporter, RunID
"github.com/wwz16/dagor/config"              // config.MergeCoalesce
"github.com/wwz16/dagor/graph"               // graph.NewBuilder
"github.com/wwz16/dagor/operator"            // RegisterOp, RegisterOpFactory
"github.com/wwz16/dagor/operator/builtin"    // Coalesce*Op + ContextValFactory
"github.com/wwz16/dagor/predicate"           // predicate.Register (only when using conditions)
"github.com/wwz16/dagor/reporter"            // reporter.New
```

# Prohibited patterns

## ModeGateOp anti-pattern
Do NOT introduce a "gate" or "passthrough" vertex that fans the input out to lane siblings.
Every lane vertex must gate ITSELF with its own `Condition` + `ConditionInput`.

## VertexSkipped misuse
Do NOT use `eng.VertexSkipped` to select between branch results. Always coalesce and read
from `eng.GetOutput("final_result")`.

## g.Vertices iteration
```
// WRONG: for _, v := range g.Vertices { ... }  // g.Vertices is a func — compile error
// RIGHT: eng.GetOutput("wire_name") for every value you need
```

## MERGE constant
```
// WRONG: .Merge(1)                    // untyped int — compile error
// RIGHT: .Merge(config.MergeCoalesce) // import "github.com/wwz16/dagor/config"
```

## eng.SetInput anti-pattern
Do NOT call `eng.SetInput` to feed values into the graph. Use `ContextValOp` + `context.WithValue`
as described in the **Context-Driven Input Rule** above.
```
// WRONG: eng.SetInput("wire", value)
// RIGHT: context.WithValue(ctx, key, value)  +  ContextValFactory(keyString) vertex in the graph
```
