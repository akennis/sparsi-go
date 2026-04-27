# clawdag-go

A DAG workflow framework for Go where computation is maximally deterministic and AI is used only where no deterministic solution exists.

## Philosophy

Most AI-assisted workflows treat every step as a prompt. clawdag-go inverts this: the default is a deterministic, composable library of pure-function operators, and AI is a fallback that fills gaps the library cannot cover.

The result is a workflow that is:

- **Auditable** — every deterministic step has a known, testable outcome
- **Minimal in AI calls** — AI is invoked only when necessary, reducing cost and non-determinism
- **Transparently hybrid** — when AI does run, it logs its inputs, output, and reasoning for inspection
- **Self-correcting** — when AI-generated code fails to compile, a fallback op regenerates it with the error as context

## Architecture

Workflows are DAGs built from operators (ops). Each op is a Go struct with `dag:"input"` and `dag:"output"` field tags. The `daggen` code-generation tool reads those tags and generates the boilerplate interface methods (`InputFields`, `OutputFields`, `SetInputField`, `ResetFields`). The [dagor](https://github.com/wwz16/dagor) engine resolves dependencies, schedules ops in parallel, and threads wire values between them.

### The driver DAG

The top-level program runs a **three-phase pipeline**, each phase as a separate DAG.

#### Phase 1: Design

```
PromptOp ──────────────┐
                        ▼
LibraryScanOp ────► DAGDesignOp → design
```

`DAGDesignOp` is an AI op that produces a natural-language design for the solution DAG given the user's prompt and the available library.

#### Phase 2: Review loop (up to 3 rounds, interactive)

The user reviews the design. If feedback is given, a refine DAG runs:

```
StringConstOp (prompt) ──────────────────┐
StringConstOp (library) ─────────────────┤
StringConstOp (prev design) ─────────────┼─► DAGDesignRefineOp → design
StringConstOp (feedback) ────────────────┘
```

This repeats until the user approves or 3 rounds are exhausted.

#### Phase 3: Codegen

```
StringConstOp (prompt) ──────┐
StringConstOp (library) ─────┤
StringConstOp (design) ───────┴──► GenerateOp → go_files
                                        │
                          ┌─────────────┴─────────────┐
                          ▼                             ▼
                   ValidateDAGOp                  WriteFilesOp → temp_dir
                          │                             │
                          └──────────────┐              ▼
                                         └──► CompileOp (on_error: continue)
                                                        │
                                         ┌──────────────┴──────────────┐
                                         ▼                              ▼
                                    FallbackOp ──────────────────► RunOp (on_error: continue)
                                    (no-op if compile OK;               │
                                     re-generates + recompiles          ▼
                                     on failure)                    OutputOp
```

`GenerateOp` and `FallbackOp` call Claude (claude-sonnet-4-6) to produce a `main.go` that wires together library ops into a solution DAG. The solution binary outputs structured JSON:

```json
{"result": "17", "ai_nodes": [{"op": "MultiplyOp", "inputs": {...}, "output": 17, "reasoning": "..."}]}
```

### The library

`library/` contains a growing set of registered ops that generated solutions can use:

| Op | Kind | Description |
|----|------|-------------|
| `ConstOp` | deterministic | Injects a constant `float64` |
| `AddOp` | deterministic | Addition |
| `SubOp` | deterministic | Subtraction |
| `DivOp` | deterministic | Division (errors on zero divisor) |
| `PackMathOperandsOp` | deterministic | Packs two `float64` inputs into a `MathOperands` struct |
| `StringConstOp` | deterministic | Injects a constant string |
| `StringLookupOp` | deterministic | Looks up a key in a hardcoded map; returns `""` on miss |
| `StringToLowerOp` | deterministic | Lowercases a string |
| `CityTimeOp` | deterministic | Returns the current time for New York or Tokyo |
| `AIComputeMathOperandsToFloat64Op` | AI | Performs any binary float64 operation (e.g. multiply) |
| `AIComputeStringToStringOp` | AI | Performs any string→string transformation |
| `ModeSelectOp` | AI | Classifies input text into one of a fixed set of categories |

### AI ops

All AI ops share a common pattern: they call Claude (or Gemini for `ModeSelectOp`) with a structured prompt, retry on parse failure, and emit their reasoning alongside the result.

**`AIComputeOp[In, Out]`** is a generic base. Concrete variants are defined by combining it with typed input/output pairs:

```go
type AIComputeMathOperandsToFloat64Op struct {
    AIComputeOp[MathOperands, float64]
}
```

Any concrete variant accepts an optional `SkipIf *string` input wire. When `SkipIf` is non-empty at runtime the AI call is skipped and that value is forwarded as the result — enabling a deterministic-first / AI-fallback pattern:

```go
// Lookup first; AI fills in any miss
Vertex("lookup").Op("StringLookupOp").
    Params(map[string]string{"map": `{"hamburger":"ketchup","hotdog":"mustard"}`}).
    Input("Key", "food").
    Output("Result", "known_condiment").

Vertex("ai_suggest").Op("AIComputeStringToStringOp").
    Params(map[string]string{"operation": "suggest a condiment that pairs with the given food"}).
    Input("Input", "food").
    Input("SkipIf", "known_condiment"). // skips AI if lookup hit
    Output("Result", "condiment").
```

**`ModeSelectOp`** classifies input text into one of a caller-specified set of categories. Use it at the top of a branching workflow to dispatch to the appropriate branch:

```go
Vertex("classify").Op("ModeSelectOp").
    Params(map[string]string{"categories": "arithmetic expression,city name"}).
    Input("Input", "user_input").
    Output("Result", "input_mode").
```

## Extending the library

### Adding a deterministic op

1. Add a struct to `library/` with `dag:"input"` / `dag:"output"` tags and implement `Setup`, `Reset`, `Run`.
2. Register it in `init()`:
   ```go
   operator.RegisterOp[MyOp]()
   ```
3. Add a `const MyOpDescription` string and include it in `LibraryScanOp.Run` so generated solutions know it exists.
4. Run `go generate ./library/...` to regenerate boilerplate.

### Adding an AI op

Define a concrete variant of `AIComputeOp` for the input/output types you need:

```go
type AIComputeStringToFloat64Op struct {
    AIComputeOp[string, float64]
}

func init() {
    operator.RegisterOp[AIComputeStringToFloat64Op]()
}
```

For custom struct output types, implement `AIResponseParser`:

```go
func (r *MyResult) ParseAIResponse(raw string) error {
    return json.Unmarshal([]byte(raw), r)
}
```

### Adding conditionals

Register a named predicate and attach it to a vertex with `.Condition(...)`:

```go
predicate.Register("result_is_positive", func(inputs map[string]any) bool {
    v, ok := inputs["value"].(*float64)
    return ok && v != nil && *v > 0
})

// In the builder:
Vertex("positive_branch").Op("MyOp").
    Condition("result_is_positive").
    Input("value", "some_wire").
    ...
```

When the predicate returns false the vertex (and all vertices that depend exclusively on its outputs) is skipped. Use `CoalesceNFloat64Op` (or a similar merge op) downstream to collect whichever branch actually ran.

## Running the demo

```bash
export CLAUDE_API_KEY=<your key>
go run .
```

The example in `example/` demonstrates a fully self-contained arithmetic workflow: AI parses operator precedence, deterministic library ops (`AddOp`, `SubOp`, `DivOp`) handle what they can, and `AIComputeMathOperandsToFloat64Op` handles multiplication (intentionally absent from the library):

```bash
export GOOGLE_GENAI_API_KEY=<your key>
go run ./example/...
```

## Code generation

Operator boilerplate is generated by `daggen`. Re-run after changing struct tags:

```bash
go generate .           # driver ops
go generate ./library/... # library ops
```

Do not edit `*_gen.go` files manually.

## Environment

| Variable | Purpose |
|----------|---------|
| `CLAUDE_API_KEY` | Required for `GenerateOp`, `FallbackOp`, and `AIComputeOp` variants |
| `GOOGLE_GENAI_API_KEY` | Required for `ModeSelectOp` and the `example/` arithmetic workflow |

## Dependencies

- [dagor](https://github.com/wwz16/dagor) — DAG execution engine
- [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) — Claude API client
- [generative-ai-go](https://github.com/google/generative-ai-go) — Google Gemini SDK
- [ants/v2](https://github.com/panjf2000/ants) — goroutine worker pool
