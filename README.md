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
| `ContextValOp[T]` (via `ContextValFactory`) | deterministic | Reads a typed value from `context.Context` at run time — see [Injecting values](#injecting-values) |
| `ConstOp[T]` (via `RegisterConst`) | deterministic | Emits a fixed Go value captured at registration — use for truly static constants |
| **Math** | | |
| `AddOp` | deterministic | A + B (`float64`) |
| `SubOp` | deterministic | A − B (`float64`) |
| `MulOp` | deterministic | A × B (`float64`) |
| `DivOp` | deterministic | A ÷ B — errors on zero divisor |
| `RoundOp` | deterministic | Rounds a `float64` to nearest integer |
| `ClampOp` | deterministic | Clamps Value to [Min, Max] |
| `SumOp` | deterministic | Sums a `[]float64` slice |
| `MinOp` | deterministic | Minimum of a `[]float64` slice |
| `MaxOp` | deterministic | Maximum of a `[]float64` slice |
| `PackMathOperandsOp` | deterministic | Packs two `float64` inputs into a `MathOperands` struct |
| `AIComputeMathOperandsToFloat64Op` | AI | Performs any binary float64 operation (e.g. multiply) via Claude |
| **Strings** | | |
| `StringConcatOp` | deterministic | Concatenates two strings |
| `StringToLowerOp` | deterministic | Lowercases a string |
| `StringSplitOp` | deterministic | Splits a string by a separator into `[]string` |
| `StringLookupOp` | deterministic | Looks up a key in a params-configured map; returns `""` on miss |
| `RegexMatchOp` | deterministic | Reports whether input matches a compiled regex |
| `RegexExtractOp` | deterministic | Returns first match (or submatch group 1) of a regex |
| `AIComputeStringToStringOp` | AI | Performs any string→string transformation via Claude |
| **Booleans** | | |
| `BoolNotOp` | deterministic | Logical NOT |
| `BoolAndOp` | deterministic | Logical AND |
| `BoolOrOp` | deterministic | Logical OR |
| **Predicates — float64** | | |
| `IfFloatGtOp` | deterministic | A > B |
| `IfFloatLtOp` | deterministic | A < B |
| `IfFloatEqOp` | deterministic | A == B |
| `IfFloatGeOp` | deterministic | A >= B |
| `IfFloatLeOp` | deterministic | A <= B |
| `BetweenFloatOp` | deterministic | Min <= Value <= Max (inclusive) |
| **Predicates — int** | | |
| `IfIntGtOp` | deterministic | A > B |
| `IfIntLtOp` | deterministic | A < B |
| `IfIntEqOp` | deterministic | A == B |
| `IfIntGeOp` | deterministic | A >= B |
| `IfIntLeOp` | deterministic | A <= B |
| **Predicates — string** | | |
| `IfStringEqOp` | deterministic | A == B |
| `IfStringContainsOp` | deterministic | A contains B as a substring |
| `IfStringHasPrefixOp` | deterministic | A starts with B |
| `IfStringHasSuffixOp` | deterministic | A ends with B |
| `IfStringRegexMatchOp` | deterministic | Input matches a compiled regex (param: `pattern`) |
| `IfEmptyStringOp` | deterministic | Value is nil or empty string |
| `IfEmptySliceStringOp` | deterministic | `[]string` value is nil or empty |
| `IfEmptySliceFloat64Op` | deterministic | `[]float64` value is nil or empty |
| **Routing / select** | | |
| `SelectStringOp` | deterministic | Ternary: returns IfTrue or IfFalse based on a bool condition |
| `SelectFloat64Op` | deterministic | Ternary over `float64` |
| `SelectIntOp` | deterministic | Ternary over `int` |
| `SelectBoolOp` | deterministic | Ternary over `bool` |
| `SwitchStringOp` | deterministic | Maps Key through a params-configured cases table; returns a default on miss |
| `DefaultStringOp` | deterministic | Returns Default when Value is nil or empty; otherwise Value |
| `DefaultFloat64Op` | deterministic | Returns Default when Value is nil; zero is a valid value |
| `DefaultIntOp` | deterministic | Returns Default when Value is nil; zero is a valid value |
| **Slices** | | |
| `SliceLenOp` | deterministic | Length of a `[]string` |
| `SliceAtOp` | deterministic | Element at index (param or wire) |
| `SliceFirstOp` | deterministic | First element |
| `SliceLastOp` | deterministic | Last element |
| `SliceContainsOp` | deterministic | Reports whether a `[]string` contains a value |
| `SliceJoinOp` | deterministic | Joins `[]string` with a separator |
| `SliceFilterEqOp` | deterministic | Filters `[]string` to elements equal to Value |
| `SliceTopKOp` | deterministic | Indices of the K highest scores in a `[]float64` |
| **JSON** | | |
| `JSONExtractOp` | deterministic | Extracts a value from JSON using a dot-separated path |
| **I/O** | | |
| `FileReadOp` | deterministic | Reads a file from disk |
| `EnvOp` | deterministic | Reads an environment variable |
| `HTTPGetOp` | deterministic | HTTP GET — returns Body and StatusCode |
| **Time** | | |
| `CityTimeOp` | deterministic | Returns the current time for "New York" or "Tokyo" |
| **AI ops** | | |
| `ModeSelectOp` | AI | Classifies input text into one of a fixed set of categories |
| `AIBoolOp` | AI | Yes/no predicate about input text |
| `AIScoreOp` | AI | Scores text against a criterion, returns float64 ∈ [0,1] |
| `AIClassifyMultiLabelOp` | AI | Maps input to zero or more of a fixed set of labels |
| `AIExtractStringSliceOp` | AI | Extracts a list of strings from free-form text |
| `AIExtractMapOp` | AI | Extracts key-value pairs from free-form text |
| `AIParseNumberOp` | AI | Converts free-form text to a `float64` |
| `AISummarizeOp` | AI | Summarizes a `[]string` into a single result string |
| `AIBestMatchOp` | AI | Returns the 0-based index of the best-matching candidate for a query |
| `AIRerankOp` | AI | Returns a permutation of candidate indices, best first |

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

### Injecting values

Every wire value in a dagor graph must be produced by an operator. Use `ContextValOp` from `github.com/wwz16/dagor/operator/builtin` to inject per-execution values via Go's `context.Context`. The graph is built once; each `eng.Run(ctx)` call supplies a different value through the context — the key pattern for request pipelines and servers.

```go
import builtin "github.com/wwz16/dagor/operator/builtin"

type itemsKey struct{}
type thresholdKey struct{}

func init() {
    operator.RegisterOpFactory("my_items", builtin.ContextValFactory[[]string](itemsKey{}))
    operator.RegisterOpFactory("threshold", builtin.ContextValFactory[float64](thresholdKey{}))
}

// Build the graph once at startup:
g, _ := graph.NewBuilder("my_graph").
    Vertex("items_src").Op("my_items").Output("Result", "items").
    Vertex("threshold_src").Op("threshold").Output("Result", "threshold").
    // ... downstream vertices consume "items" and "threshold" wires
    Build()

// Inject values at run time — a new Engine per call, same graph:
ctx := context.WithValue(context.Background(), itemsKey{}, []string{"foo", "bar", "baz"})
ctx = context.WithValue(ctx, thresholdKey{}, 0.75)
eng, _ := dagor.NewEngine(g, pool)
eng.Run(ctx)
```

`ContextValFactory[T](key)` returns a factory for `operator.RegisterOpFactory`. The resulting op reads the value from the context key at each `Run` call; it errors if the key is missing or has the wrong type. Context keys must be unexported struct types to avoid collisions.

**Params vs `ContextValOp`**

Use **params** for static configuration that is part of the op's definition: operation names, map keys, regex patterns, flags. Params are JSON-encoded into the graph config and read by the op's `Setup` method, so they only work for JSON-representable types and require the op to explicitly parse them.

Use **`ContextValOp`** for any value that varies per execution: user input, request data, file content, computed URLs, API responses, or any type that is awkward to serialize into JSON. Registering the factory once and injecting via context means the same graph definition serves many executions.

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

The `examples/` directory demonstrates a self-contained arithmetic workflow: AI parses operator precedence, deterministic library ops (`AddOp`, `SubOp`, `DivOp`) handle what they can, and `AIComputeMathOperandsToFloat64Op` handles multiplication (intentionally absent from the library):

```bash
export CLAUDE_API_KEY=<your key>
go run ./examples/...
```

## Examples

Each example is a standalone Go binary that builds and runs a dagor workflow. All live under `examples/` and are compiled from the root module.

| Example | Description | Env vars required |
|---|---|---|
| [`01-ticket-triager`](examples/01-ticket-triager/) | Classifies a free-text support ticket into billing / bug / feature / other and routes it through a category-specific extraction lane to produce structured output. | `CLAUDE_API_KEY` |
| [`02-recipe-analyzer`](examples/02-recipe-analyzer/) | Fetches recipe instructions from TheMealDB (or a local fixture), runs three parallel AI extractors (ingredients, steps, cook time), scores difficulty deterministically, and returns difficulty-specific cooking advice. Uses Gemini for all AI vertices. | `GEMINI_API_KEY` |
| [`03-readme-quality`](examples/03-readme-quality/) | Fetches a GitHub README (or fixture), runs five AI quality probes in parallel, averages the scores, and routes through a quality-band lane to produce a structured report. | `CLAUDE_API_KEY` |
| [`04-weather-advisor`](examples/04-weather-advisor/) | Fetches live weather data for a city (or fixture), classifies conditions via AI, and combines deterministic temperature-band logic with AI-generated outfit advice. | `CLAUDE_API_KEY` |
| [`05-hn-topic-brief`](examples/05-hn-topic-brief/) | Queries the HN Algolia API for a topic, fans out per-story relevance and classification checks over a MapOver node, identifies the dominant category, and produces a styled topic brief. | `CLAUDE_API_KEY` |
| [`06-faithful-summary`](examples/06-faithful-summary/) | Demonstrates cross-model verification: Claude summarizes a source document in 3–5 sentences, then Gemini independently checks whether every claim in the summary is grounded in the source text, returning a boolean faithfulness verdict. | `CLAUDE_API_KEY`, `GEMINI_API_KEY` |

```bash
# Example: run the summarization faithfulness checker
export CLAUDE_API_KEY=<your Anthropic key>
export GEMINI_API_KEY=<your Google AI key>
go run ./examples/06-faithful-summary --file article.txt
```

## Claude Code Skills

Two installable skill packages let you design and generate clawdag-go workflows interactively
through an AI assistant rather than running the driver CLI directly.

| Skill | Trigger | Purpose |
|---|---|---|
| `clawdag-design` | `/clawdag-design` | Design a maximally deterministic DAG workflow from a task description, with an interactive refinement loop |
| `clawdag-codegen` | `/clawdag-codegen` | Generate, compile, and fix a Go workflow binary from an approved design |

Download the latest bundle from the [releases page](https://github.com/akennis/clawdag-go/releases)
and follow the installation instructions in the bundle's `README.md`.

### Generating the skills directory

`skills/` is a build artifact — it is gitignored and assembled from canonical sources by
`tools/genskills/main.go`. Run from the repo root:

```bash
go generate .
```

This regenerates `skills/` alongside the driver op boilerplate. Sources that feed into `skills/`:

| Canonical source | Generated output |
|---|---|
| `skill-src/*/SKILL.md` | `skills/*/SKILL.md` |
| `skill-src/*/references/examples/README.md` | `skills/*/references/examples/README.md` |
| `skill-src/README.md` | `skills/README.md` |
| `prompts/dag_design.md` | `skills/clawdag-design/references/design-rules.md` |
| `prompts/dagor-api.md` | `skills/clawdag-codegen/references/dagor-api.md` |
| `examples/0N-*/main.go` | `skills/*/references/examples/0N-*.go` (with `//go:build ignore` prepended) |
| `library.AllDescriptions()` | `skills/*/references/library.md` |

### Building a release bundle

Generate `skills/` first, then package it into a versioned zip from the repo root.

**Linux / macOS:**
```bash
go generate .
cd skills && zip -r ../clawdag-go-skills-v0.1.0.zip clawdag-design clawdag-codegen README.md
```

**Windows (PowerShell):**
```powershell
go generate .
Compress-Archive -Path skills\clawdag-design, skills\clawdag-codegen, skills\README.md `
    -DestinationPath clawdag-go-skills-v0.1.0.zip
```

Upload the zip as a release asset alongside the tagged source code.

### Release checklist for version bumps

When cutting a new library version (e.g. `v0.1.0 → v0.2.0`):

1. **Update version references** — two files need the new version string:
   - `skill-src/clawdag-design/SKILL.md` — `version:` and `library_version:` in frontmatter
   - `skill-src/clawdag-codegen/SKILL.md` — same frontmatter fields, plus the `require github.com/akennis/clawdag-go` line in the go.mod template in the Steps section
   - `skill-src/README.md` — the "This bundle targets …" line at the top

2. **Update the dagor replace directive** if `github.com/akennis/dagor` has been tagged at a new
   version — update the `replace github.com/wwz16/dagor =>` line in the go.mod template in
   `skill-src/clawdag-codegen/SKILL.md` to match.

3. **Regenerate and publish** — run `go generate .`, build the zip, upload as a release asset.

## Code Generation

`go generate .` regenerates driver op boilerplate (`daggen`) and assembles `skills/`:

```bash
go generate .             # driver ops + skills/
go generate ./library/... # library ops only
```

Do not edit `*_gen.go` files or anything under `skills/` manually — both are generated.

## Prompt Templates

The `prompts/` directory contains embedded prompt templates used by driver ops:

| File | Used By | Purpose |
|------|---------|---------|
| `dag_design.md` | `DAGDesignOp`; also → `skills/clawdag-design/references/design-rules.md` | Instructs AI to design a DAG workflow |
| `dag_design_refine.md` | `DAGDesignRefineOp` | Refines design based on user feedback |
| `codegen.md` | `GenerateOp`, `FallbackOp` | Full code generation instructions with DSL reference |
| `compile_error_context.md` | `FallbackOp` | Error context for compile failure retries |
| `dag_validation_error_context.md` | `FallbackOp` | Error context for DAG validation failures |
| `mcpb_manifest_ai.md` | `MCPBManifestAIOp` | Generates package name/description metadata |
| `dagor-api.md` | → `skills/clawdag-codegen/references/dagor-api.md` | Dagor engine API reference for the codegen skill |

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
│   ├── descriptions.go   — AllDescriptions() — joins all 71 op description constants
│   ├── const_op.go       — ConstOp[T] + RegisterConst (static constant injection)
│   ├── math_ops.go       — AddOp, SubOp, DivOp, PackMathOperandsOp, and more
│   ├── string_ops.go     — StringLookupOp, StringToLowerOp, AIComputeStringToStringOp, and more
│   ├── time_ops.go       — CityTimeOp
│   ├── mode_select_op.go — ModeSelectOp
│   ├── ai_compute_op.go  — generic AIComputeOp[In, Out] base
│   ├── gen.go            — //go:generate directives for library ops
│   └── *_gen.go          — generated (do not edit)
├── tools/
│   ├── genlibdesc/
│   │   └── main.go       — standalone library.md generator (legacy; go generate . uses genskills)
│   └── genskills/
│       └── main.go       — assembles skills/ from skill-src/, prompts/, examples/, and library
├── skill-src/            — canonical sources for skill-specific content
│   ├── README.md         — end-user install instructions (→ skills/README.md)
│   ├── clawdag-design/
│   │   ├── SKILL.md      — skill definition (→ skills/clawdag-design/SKILL.md)
│   │   └── references/examples/README.md
│   └── clawdag-codegen/
│       ├── SKILL.md      — skill definition (→ skills/clawdag-codegen/SKILL.md)
│       └── references/examples/README.md
├── skills/               — generated by go generate . (gitignored; do not edit)
├── examples/
│   └── ...               — standalone workflow examples (01–06)
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
