# clawdag-go

A DAG workflow framework for Go where computation is maximally deterministic and AI is used only where no deterministic solution exists. Given a natural-language prompt, clawdag-go designs, generates, compiles, and packages a self-contained workflow binary — ready to run as a CLI tool or an MCP server.

## Quick Start

```bash
export CLAUDE_API_KEY=<your Anthropic API key>
go run .
```

You'll be prompted for a task description. The system then:
1. Designs a DAG workflow (AI-assisted)
2. Presents the design for your review (up to 3 refinement rounds)
3. Generates, compiles, and packages the solution
4. Outputs the binary path and an optional `.mcpb` package

### Example session

```
Enter prompt: Write a program that tells me the current time in any city
═══ DAG DESIGN (round 1/3) ═══
...
Press Enter to approve, or type feedback to refine: <Enter>
Design approved.
--- Generated Binary ---
~/.dag-ai/solution/solution_bin.exe
--- Generated MCPB ---
~/.dag-ai/solution.mcpb
```

The generated binary supports two modes:
```bash
./solution_bin --mode cli    # interactive CLI (default)
./solution_bin --mode mcp    # MCP server for AI clients
```

## Philosophy

Most AI-assisted workflows treat every step as a prompt. clawdag-go inverts this: the default is a deterministic, composable library of pure-function operators, and AI is a fallback that fills gaps the library cannot cover.

The result is a workflow that is:

- **Auditable** — every deterministic step has a known, testable outcome
- **Minimal in AI calls** — AI is invoked only when necessary, reducing cost and non-determinism
- **Transparently hybrid** — when AI does run, it logs its inputs, output, and reasoning for inspection
- **Self-correcting** — when AI-generated code fails to compile, a fallback op regenerates it with the error as context
- **Packageable** — successful builds are bundled as `.mcpb` archives for distribution

## Architecture

Workflows are DAGs built from operators (ops). Each op is a Go struct with `dag:"input"` and `dag:"output"` field tags. The `daggen` code-generation tool reads those tags and generates boilerplate interface methods (`InputFields`, `OutputFields`, `SetInputField`, `ResetFields`). The [dagor](https://github.com/wwz16/dagor) engine resolves dependencies, schedules ops in parallel, and threads wire values between them.

### The Driver Pipeline

The top-level program runs a **three-phase pipeline**:

#### Phase 1: Design

```
PromptOp ──────────────┐
                        ▼
LibraryScanOp ────► DAGDesignOp → design
```

`DAGDesignOp` calls Claude to produce a natural-language design for the solution DAG given the user's prompt and the available library.

#### Phase 2: Review loop (up to 3 rounds, interactive)

The user reviews the design. If feedback is given, a refine DAG runs:

```
StringConstOp (prompt) ──────────────────┐
StringConstOp (library) ─────────────────┤
StringConstOp (prev design) ─────────────┼─► DAGDesignRefineOp → design
StringConstOp (feedback) ────────────────┘
```

This repeats until the user approves or 3 rounds are exhausted.

#### Phase 3: Codegen + Package

```
StringConstOp (prompt) ──────┐
StringConstOp (library) ─────┤
StringConstOp (design) ───────┴──► GenerateOp → go_files
                                        │
                          ┌─────────────┼─────────────┐
                          ▼             ▼             ▼
                   ValidateDAGOp   WriteFilesOp   EnvScanOp
                          │             │             │
                          └──────┐      ▼             │
                                 └► CompileOp         │
                                        │             │
                          ┌─────────────┴─────┐       │
                          ▼                   ▼       │
                     FallbackOp ──────► MCPBManifestAIOp
                     (no-op if OK;            │       │
                      regen on fail)          ▼       │
                                     MCPBManifestPromptOp ◄─┘
                                              │
                                              ▼
                                        PackageMCPBOp → .mcpb
```

**Key ops in Phase 3:**

| Op | Role |
|----|------|
| `GenerateOp` | Calls Claude (claude-sonnet-4-6) to produce a `main.go` solution |
| `ValidateDAGOp` | Validates the DAG structure of generated code |
| `WriteFilesOp` | Writes files to `~/.dag-ai/solution/` and runs `go mod tidy` |
| `CompileOp` | Compiles the solution binary (`on_error: continue`) |
| `FallbackOp` | If compile/validation failed, regenerates + recompiles |
| `EnvScanOp` | Scans generated code for `os.Getenv` calls |
| `MCPBManifestAIOp` | AI-generates name/display_name/description for the package |
| `MCPBManifestPromptOp` | Prompts user to confirm/edit manifest fields |
| `PackageMCPBOp` | Bundles binary + manifest into a `.mcpb` ZIP archive |

### The Library

`library/` contains registered ops that generated solutions can use:

| Op | Kind | Description |
|----|------|-------------|
| `ConstOp` | deterministic | Injects a constant `float64` |
| `AddOp` | deterministic | Addition |
| `SubOp` | deterministic | Subtraction |
| `DivOp` | deterministic | Division (errors on zero divisor) |
| `PackMathOperandsOp` | deterministic | Packs two `float64` inputs into a `MathOperands` struct |
| `StringConstOp` | deterministic | Injects a constant string |
| `StringLookupOp` | deterministic | Looks up a key in a configurable map; returns `""` on miss |
| `StringToLowerOp` | deterministic | Lowercases a string |
| `CityTimeOp` | deterministic | Returns the current time for "New York" or "Tokyo" |
| `AIComputeMathOperandsToFloat64Op` | AI | Performs any binary float64 operation (e.g. multiply) |
| `AIComputeStringToStringOp` | AI | Performs any string-to-string transformation |
| `ModeSelectOp` | AI | Classifies input text into one of a fixed set of categories |

### AI Ops

All AI ops call Claude (claude-sonnet-4-6) with structured prompts, retry on parse failure, and emit reasoning alongside the result.

**`AIComputeOp[In, Out]`** is a generic base. Concrete variants are defined by embedding it with typed input/output pairs:

```go
type AIComputeMathOperandsToFloat64Op struct {
    AIComputeOp[MathOperands, float64]
}
```

Any concrete variant accepts an optional `SkipIf *string` input wire. When non-empty at runtime the AI call is skipped and that value is forwarded as the result — enabling a deterministic-first / AI-fallback pattern:

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

**`ModeSelectOp`** classifies input text into one of a caller-specified set of categories:

```go
Vertex("classify").Op("ModeSelectOp").
    Params(map[string]string{"categories": "arithmetic expression,city name"}).
    Input("Input", "user_input").
    Output("Result", "input_mode").
```

### Generated Solution Structure

Generated solutions are dual-mode executables with this architecture:

```go
// UserInput — all workflow parameters in one struct
// readCLIInput() — populates UserInput from stdin
// buildGraph(input) — constructs the DAG from UserInput
// runWorkflow(ctx, pool, input) — shared DAG execution
// runCLIProgram(ctx, pool) — CLI mode wrapper
// runMCPServer(pool) — MCP server mode wrapper
// main() — flag parsing, mode dispatch
```

The solution binary outputs structured JSON in CLI mode:
```json
{"result": "17", "ai_nodes": [{"op": "MultiplyOp", "inputs": {...}, "output": 17, "reasoning": "..."}]}
```

### MCPB Packaging

When compilation succeeds, the driver packages the binary into a `.mcpb` archive (ZIP) containing:
- `manifest.json` — MCP manifest with tool metadata, env var declarations, and platform compatibility
- `server/solution_bin.exe` — the compiled binary

The manifest includes `user_config` entries for any environment variables detected via `EnvScanOp`.

## Extending the Library

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
func (r *MyResult) ExpectedFormat() string {
    return `Respond with JSON: {"field": "value"}. No explanation.`
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
    ConditionInput("value"). // predicate sees the wire; op does not
    ...
```

When the predicate returns false the vertex (and all vertices that depend exclusively on its outputs) is skipped. Use `CoalesceNStringOp` (or a similar merge op with `Merge(config.MergeCoalesce)`) downstream to collect whichever branch actually ran.

### Map nodes

Fan out a sub-graph over each element of a slice:

```go
Vertex("upper_all").
Input("Items", "raw_strings").
MapOver("item").
    SubVertex("to_upper").
        Op("StringToUpperOp").
        Input("Value", "item").
        Output("Result", "result").
    CollectInto("result", "upper_strings").
```

## Running the Demo

```bash
export CLAUDE_API_KEY=<your key>
go run .
```

The `example/` directory demonstrates a self-contained arithmetic workflow: AI parses operator precedence, deterministic library ops (`AddOp`, `SubOp`, `DivOp`) handle what they can, and `AIComputeMathOperandsToFloat64Op` handles multiplication (intentionally absent from the library):

```bash
export CLAUDE_API_KEY=<your key>
go run ./example/...
```

## Code Generation

Operator boilerplate is generated by `daggen`. Re-run after changing struct tags:

```bash
go generate .           # driver ops
go generate ./library/... # library ops
```

Do not edit `*_gen.go` files manually.

## Prompt Templates

The `prompts/` directory contains embedded prompt templates used by driver ops:

| File | Used By | Purpose |
|------|---------|---------|
| `dag_design.md` | `DAGDesignOp` | Instructs AI to design a DAG workflow |
| `dag_design_refine.md` | `DAGDesignRefineOp` | Refines design based on user feedback |
| `codegen.md` | `GenerateOp`, `FallbackOp` | Full code generation instructions with DSL reference |
| `compile_error_context.md` | `FallbackOp` | Error context for compile failure retries |
| `dag_validation_error_context.md` | `FallbackOp` | Error context for DAG validation failures |
| `mcpb_manifest_ai.md` | `MCPBManifestAIOp` | Generates package name/description metadata |

## File Layout

```
clawdag-go/
├── main.go               — driver DAG phases + retry loop
├── driver_ops.go         — 16 driver op structs
├── gen.go                — //go:generate directives for driver ops
├── driver_*_gen.go       — generated (do not edit)
├── prompts/
│   ├── codegen.md        — code generation prompt template
│   ├── dag_design.md     — DAG design prompt
│   ├── dag_design_refine.md — design refinement prompt
│   ├── compile_error_context.md
│   ├── dag_validation_error_context.md
│   └── mcpb_manifest_ai.md
├── library/
│   ├── math_ops.go       — AddOp, SubOp, DivOp, ConstOp, PackMathOperandsOp
│   ├── string_ops.go     — StringConstOp, StringLookupOp, StringToLowerOp, AIComputeStringToStringOp
│   ├── time_ops.go       — CityTimeOp
│   ├── mode_select_op.go — ModeSelectOp
│   ├── ai_compute_op.go  — generic AIComputeOp[In, Out] base
│   ├── gen.go            — //go:generate directives for library ops
│   └── *_gen.go          — generated (do not edit)
├── example/
│   └── main.go           — arithmetic demo workflow
└── CLAUDE.md             — instructions for AI assistants
```

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `CLAUDE_API_KEY` | Required for all Claude API calls (design, codegen, AI library ops, ModeSelectOp) |

## Dependencies

- [dagor](https://github.com/wwz16/dagor) — DAG execution engine
- [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) — Claude API client
- [mcp-go](https://github.com/mark3labs/mcp-go) — MCP server library (used in generated solutions)
- [ants/v2](https://github.com/panjf2000/ants) — goroutine worker pool
