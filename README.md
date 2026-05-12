# clawdag-go

A DAG workflow framework for Go where computation is maximally deterministic and AI is used only where no deterministic solution exists. clawdag-go is a **library**: you import it, you write your own `main`, and you `go build` your workflow into a binary you control.

Code generation is handled by **AI assistants over the bundled skills** (`clawdag-design`, `clawdag-codegen`) — see [Claude Code Skills](#claude-code-skills) below. There is no built-in driver binary in this repository.

## Quick Start

Add the library and a DAG engine to your project, then write a `main.go` that builds and runs a graph.

```bash
go get github.com/akennis/clawdag-go/library@latest
go get github.com/wwz16/dagor@latest
go get github.com/panjf2000/ants/v2@latest
```

A minimal workflow looks like this:

```go
package main

import (
    "context"
    "fmt"

    "github.com/panjf2000/ants/v2"
    "github.com/wwz16/dagor"
    "github.com/wwz16/dagor/graph"
    "github.com/wwz16/dagor/operator"

    _ "github.com/akennis/clawdag-go/library" // registers all library ops
)

func main() {
    g, err := graph.NewBuilder("hello").
        Vertex("greet").Op("StringConcatOp").
        Params(map[string]string{"a": "Hello, ", "b": "world"}).
        Output("Result", "out").
        Build()
    if err != nil { panic(err) }

    pool, _ := ants.NewPool(4)
    defer pool.Release()

    eng, _ := dagor.NewEngine(g, pool)
    if err := eng.Run(context.Background()); err != nil { panic(err) }

    out, _ := eng.GetOutput("out")
    fmt.Println(*(out.(*string)))
    eng.Close(context.Background())
    _ = operator.Registry // ensure the registry is referenced; library import does the work
}
```

For real workflows, see the [`examples/`](examples/) directory — six end-to-end programs that cover classification, scoring, parallel fan-out, MapOver, and cross-model verification.

## Philosophy

Most AI-assisted workflows treat every step as a prompt. clawdag-go inverts this: the default is a deterministic, composable library of pure-function operators, and AI is invoked only when necessary.

The result is a workflow that is:

- **Auditable** — every deterministic step has a known, testable outcome
- **Minimal in AI calls** — AI is invoked only when necessary, reducing cost and non-determinism
- **Transparently hybrid** — when AI does run, it logs its inputs, output, and reasoning for inspection

## Architecture

Workflows are DAGs built from operators (ops). Each op is a Go struct with `dag:"input"` and `dag:"output"` field tags. The `daggen` code-generation tool reads those tags and generates boilerplate interface methods (`InputFields`, `OutputFields`, `SetInputField`, `ResetFields`). The [dagor](https://github.com/wwz16/dagor) engine resolves dependencies, schedules ops in parallel, and threads wire values between them.

### The Library

`library/` contains registered ops that workflows can use:

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
| `AIComputeMathOperandsToFloat64Op` | AI | Performs any binary float64 operation (e.g. multiply) via the AI provider |
| **Strings** | | |
| `StringConcatOp` | deterministic | Concatenates two strings |
| `StringToLowerOp` | deterministic | Lowercases a string |
| `StringSplitOp` | deterministic | Splits a string by a separator into `[]string` |
| `StringLookupOp` | deterministic | Looks up a key in a params-configured map; returns `""` on miss |
| `RegexMatchOp` | deterministic | Reports whether input matches a compiled regex |
| `RegexExtractOp` | deterministic | Returns first match (or submatch group 1) of a regex |
| `AIComputeStringToStringOp` | AI | Performs any string→string transformation via the AI provider |
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
| **MCP / external tools** | | |
| `MCPCallOp[In, Out]` | external | Generic single-call MCP tool wrapper. Embed in a concrete struct with typed In/Out and register the subclass. Optional warm-replenish pool via `pool_size` |
| `MCPScriptOp[In, Out]` | external | Generic multi-call MCP scripted session — one DAG step makes many tool calls that share server-side state (browser, file handles). Embed + register. Optional warm-replenish pool via `pool_size` |
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
3. Add a `const MyOpDescription` string and include it in `library.AllDescriptions()` so the codegen skill knows it exists.
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

### Custom credential routing

By default, AI ops read `CLAUDE_API_KEY` / `GEMINI_API_KEY` from the process environment. Teams that need to route credentials through Vault, AWS Secrets Manager, GCP Secret Manager, workload identity, or an egress proxy — or run a multi-tenant workflow where different vertices use different credentials — swap that path by implementing `library.AIClientFactory`:

```go
type AIClientFactory interface {
    Anthropic(ctx context.Context, ref string) (*anthropic.Client, error)
    Gemini(ctx context.Context, ref string) (*genai.Client, error)
}
```

`ref` is opaque to the library — empty means "default". Implementations decide whether it is a Vault path, tenant id, region, or anything else. The factory returns a fully configured SDK client; the library never sees the API key.

The `ctx` passed to factory methods is bounded by an op-level deadline (default 30 s) so credential lookups can't hang the workflow at Setup. Factories that do network I/O must honor `ctx.Done()`. Override the deadline per vertex with the `api_factory_timeout_ms` param (string, ms; `"0"` disables).

**Process-wide swap.** Register the factory once at startup and every AI op vertex uses it:

```go
import (
    "github.com/akennis/clawdag-go/library"
    "github.com/anthropics/anthropic-sdk-go"
    "google.golang.org/genai"
)

type vaultFactory struct{ addr string }

func (f *vaultFactory) Anthropic(ctx context.Context, ref string) (*anthropic.Client, error) {
    key, err := fetchFromVault(ctx, f.addr, ref) // ref → vault path
    if err != nil { return nil, err }
    c := anthropic.NewClient(option.WithAPIKey(key))
    return &c, nil
}
func (f *vaultFactory) Gemini(ctx context.Context, ref string) (*genai.Client, error) {
    key, err := fetchFromVault(ctx, f.addr, ref)
    if err != nil { return nil, err }
    return genai.NewClient(ctx, &genai.ClientConfig{APIKey: key})
}

func main() {
    library.SetDefaultAIClientFactory(&vaultFactory{addr: "https://vault.internal"})
    // build graph, run engine ...
}
```

**Per-vertex routing.** Register factories under string ids, then opt vertices in via the `client_factory_id` param. `credential_ref` is forwarded to the factory verbatim:

```go
library.RegisterAIClientFactory("tenant-a", &vaultFactory{addr: "https://vault.internal"})
library.RegisterAIClientFactory("tenant-b", &awsFactory{region: "us-east-1"})

graph.NewBuilder("multi_tenant").
    Vertex("classify_a").Op("AIBoolOp").
        Params(map[string]string{
            "predicate":         "is this in English?",
            "client_factory_id": "tenant-a",
            "credential_ref":    "secret/tenant-a/anthropic",
        }).
        Input("Input", "text").
        Output("Result", "is_english_a")
```

Vertices that omit `client_factory_id` fall back to the process-wide default factory. The bundled `EnvAIClientFactory` is the default when nothing is registered, so existing workflows keep working with no code change.

Factories also serve as the seam for hermetic tests — see `library/ai_factory_test.go` for a recording-stub pattern that asserts ref propagation without any network traffic.

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

### MCP operators

`MCPCallOp[In, Out]` and `MCPScriptOp[In, Out]` invoke an MCP server (local subprocess via stdio, or remote via streamable HTTP) as a workflow step. They are escape hatches for steps that need an external tool (a headless browser, a sandboxed filesystem, a hosted vector-search service) without writing transport plumbing yourself. Embed the generic in a named concrete struct with typed In/Out and register that — never register the generic directly. The `transport` param selects `"stdio"` (default) or `"http"`. See `examples/remote-mcp-server/main.go` for a single-call MCPCallOp variant against a remote HTTP MCP server, and `examples/local-mcp-server/main.go` for the scripted multi-call form against a local subprocess.

#### Warm-replenish pool

By default each `Run` spawns a fresh MCP subprocess and tears it down on completion. When the cold start dominates wall time (e.g. Playwright launching Chromium for every screenshot in a fan-out), opt into the warm-replenish pool by setting `pool_size: "N"` on the vertex params. The pool keeps N pre-started sessions ready and refills the slot in a background goroutine after each borrow, so subsequent vertices skip the cold start. Each borrow yields a *fresh* session — sessions are never reused for a second logical call — so stateful servers (browser tabs, file cursors) cannot leak state across vertices.

`pool_prewarm` defaults to `"true"`: the pool fills during `Setup`. Set it to `"false"` for lazy fill (first borrow pays cold start; subsequent borrows are warm).

When any vertex sets `pool_size > 0`, defer `ShutdownMCPPool` from `main()` so pre-started subprocesses drain on exit:

```go
defer library.ShutdownMCPPool(context.Background())
```

Run `examples/local-mcp-server` with `--log-level=debug` to see the pool in action — `mcp pool prewarm scheduled`, `mcp pool acquire warm hit`, and `mcp pool replenish ok` lines trace each lifecycle event. Pooling is supported only for `transport: "stdio"` in v1; HTTP MCP vertices reject `pool_size > 0` at `Setup`.

#### Custom argument and response shapes

By default, `MCPCallOp` JSON-marshals the dereferenced `*In` value as the tool's `arguments` object, and dispatches the result over the `Out` type — `string`, `float64`, `int`, `bool`, `[]string`, `[]float64`, `[]int`, `map[string]string`, or any other type via `json.Unmarshal` (structured content is preferred when the server emits it).

Two optional interfaces let In/Out types override these defaults:

- **`MCPArgsFormatter`** — implement `FormatMCPArgs() (any, error)` on the `In` type when the tool's argument schema doesn't match the natural JSON shape of your struct (nested wrapping, renamed fields, dynamic keys).
- **`MCPResponseParser`** — implement `ParseMCPResponse(text string, structured json.RawMessage) error` on `*Out` to fully control parsing of the tool's reply. Receives both the concatenated text content and the raw structured-content JSON (nil if the tool emitted none).

Inside an `MCPScriptOp` script, `MCPSession.CallTool` returns a `*MCPToolError` when the server reports a tool-level error (`IsError=true`); transport / I/O failures surface as their underlying error. Scripts that want to recover from anticipated failures (element-not-found on a click, missing file) can `errors.As` against `*MCPToolError` and continue.

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

## Examples

Each example is a standalone Go binary that builds and runs a dagor workflow. All live under `examples/` and are compiled from the root module.

| Example | Description | Env vars required |
|---|---|---|
| [`ticket-triager`](examples/ticket-triager/) | Classifies a free-text support ticket into billing / bug / feature / other and routes it through a category-specific extraction lane to produce structured output. | `CLAUDE_API_KEY` |
| [`recipe-analyzer`](examples/recipe-analyzer/) | Fetches recipe instructions from TheMealDB (or a local fixture), runs three parallel AI extractors (ingredients, steps, cook time), scores difficulty deterministically, and returns difficulty-specific cooking advice. Uses Gemini for all AI vertices. | `GEMINI_API_KEY` |
| [`readme-quality`](examples/readme-quality/) | Fetches a GitHub README (or fixture), runs five AI quality probes in parallel, averages the scores, and routes through a quality-band lane to produce a structured report. | `CLAUDE_API_KEY` |
| [`stock-analyzer`](examples/stock-analyzer/) | Fetches live stock data and news from Yahoo Finance, performs deterministic calculations, and generates an AI-driven investment recommendation. Uses Gemini for AI vertices. | `GEMINI_API_KEY` |
| [`weather-advisor`](examples/weather-advisor/) | Fetches live weather data for a city (or fixture), classifies conditions via AI, and combines deterministic temperature-band logic with AI-generated outfit advice. | `CLAUDE_API_KEY` |
| [`hn-topic-brief`](examples/hn-topic-brief/) | Queries the HN Algolia API for a topic, fans out per-story relevance and classification checks over a MapOver node, identifies the dominant category, and produces a styled topic brief. | `CLAUDE_API_KEY` |
| [`faithful-summary`](examples/faithful-summary/) | Demonstrates cross-model verification: Claude summarizes a source document in 3–5 sentences, then Gemini independently checks whether every claim in the summary is grounded in the source text, returning a boolean faithfulness verdict. | `CLAUDE_API_KEY`, `GEMINI_API_KEY` |
| [`local-mcp-server`](examples/local-mcp-server/) | Two `MCPScriptOp` variants composed via MapOver against a local Playwright MCP subprocess: a Google search returns a slice of URLs, and per-URL screenshot sub-vertices fan out using the warm-replenish pool (`pool_size: 8`). Demonstrates `defer library.ShutdownMCPPool(...)` in `main()`. | none (requires `npx`) |
| [`remote-mcp-server`](examples/remote-mcp-server/) | Single-vertex `MCPCallOp` against a remote (HTTP) MCP server — calls Cloudflare's public docs MCP at `https://docs.mcp.cloudflare.com/mcp` and prints the search result text. The minimal pattern for `transport: "http"`. | none |

```bash
# Example: run the summarization faithfulness checker
export CLAUDE_API_KEY=<your Anthropic key>
export GEMINI_API_KEY=<your Google AI key>
go run ./examples/faithful-summary --file article.txt
```

## Claude Code Skills

Two installable skill packages let you design and generate clawdag-go workflows interactively through Claude Code (or any AI assistant that supports the `SKILL.md / references/` convention). These skills are **the** way to bootstrap a new workflow — they replace what was previously a built-in driver binary.

| Skill | Trigger | Purpose |
|---|---|---|
| `clawdag-design` | `/clawdag-design` | Design a maximally deterministic DAG workflow from a task description, with an interactive refinement loop |
| `clawdag-codegen` | `/clawdag-codegen` | Generate a Go workflow `main.go` + `go.mod` from an approved design, run `go mod tidy`, and fix any build errors |

Download the latest bundle from the [releases page](https://github.com/akennis/clawdag-go/releases) and follow the installation instructions in the bundle's `README.md`.

### Generating the skills directory

`skills/` is a build artifact — it is gitignored and assembled from canonical sources by `tools/genskills/main.go`. Run from the repo root:

```bash
go generate .
```

Sources that feed into `skills/`:

| Canonical source | Generated output |
|---|---|
| `skill-src/README.md` | `skills/README.md` |
| `skill-src/<skill>/SKILL.md` | `skills/<skill>/SKILL.md` |
| `skill-src/<skill>/references/examples/README.md` | `skills/<skill>/references/examples/README.md` |
| `skill-src/clawdag-design/references/design-rules.md` | `skills/clawdag-design/references/design-rules.md` |
| `skill-src/clawdag-codegen/references/dagor-api.md` | `skills/clawdag-codegen/references/dagor-api.md` |
| `examples/0N-*/main.go` | `skills/<skill>/references/examples/0N-*.go` (with `//go:build ignore` prepended) |
| `library.AllDescriptions()` | `skills/<skill>/references/library.md` |

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

1. **Update version references** — three files need the new version string:
   - `skill-src/clawdag-design/SKILL.md` — `version:` and `library_version:` in frontmatter
   - `skill-src/clawdag-codegen/SKILL.md` — same frontmatter fields, plus the `require github.com/akennis/clawdag-go` line in the go.mod template in the Steps section
   - `skill-src/README.md` — the "This bundle targets …" line at the top

2. **Update the dagor replace directive** if `github.com/akennis/dagor` has been tagged at a new version — update the `replace github.com/wwz16/dagor =>` line in the go.mod template in `skill-src/clawdag-codegen/SKILL.md` to match.

3. **Regenerate and publish** — run `go generate .`, build the zip, upload as a release asset.

## Code Generation

`go generate ./...` regenerates library op boilerplate (`daggen`) and assembles `skills/`:

```bash
go generate .              # skills/ only
go generate ./library/...  # library op boilerplate
go generate ./...          # both
```

Do not edit `*_gen.go` files or anything under `skills/` manually — both are generated.

## File Layout

```
clawdag-go/
├── gen.go                  — //go:generate directive that assembles skills/
├── library/                — the framework itself (importable subpackage)
│   ├── descriptions.go     — AllDescriptions() — joins all op description constants
│   ├── const_op.go         — ConstOp[T] + RegisterConst (static constant injection)
│   ├── math_ops.go         — AddOp, SubOp, DivOp, PackMathOperandsOp, …
│   ├── string_ops.go       — StringLookupOp, StringToLowerOp, …
│   ├── time_ops.go         — CityTimeOp
│   ├── mode_select_op.go   — ModeSelectOp
│   ├── ai_compute_op.go    — generic AIComputeOp[In, Out] base
│   ├── mcp_client.go       — low-level MCP session (subprocess + protocol handshake)
│   ├── mcp_call_op.go      — generic MCPCallOp[In, Out] (single-call MCP wrapper)
│   ├── mcp_script_op.go    — generic MCPScriptOp[In, Out] (multi-call scripted MCP session)
│   ├── mcp_pool.go         — warm-replenish session pool + ShutdownMCPPool
│   ├── gen.go              — //go:generate directives for library ops
│   └── *_gen.go            — generated (do not edit)
├── tools/
│   ├── genlibdesc/main.go  — standalone library.md generator
│   └── genskills/main.go   — assembles skills/ from skill-src/, examples/, and library
├── skill-src/              — canonical sources for the skill bundle
│   ├── README.md           — end-user install instructions (→ skills/README.md)
│   ├── clawdag-design/
│   │   ├── SKILL.md
│   │   └── references/
│   │       ├── design-rules.md
│   │       └── examples/README.md
│   └── clawdag-codegen/
│       ├── SKILL.md
│       └── references/
│           ├── dagor-api.md
│           └── examples/README.md
├── skills/                 — generated by go generate . (gitignored; do not edit)
├── examples/               — six end-to-end workflow examples
└── CLAUDE.md               — instructions for AI assistants in this repo
```

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `CLAUDE_API_KEY` | Required for AI library ops when using the default `EnvAIClientFactory` |
| `GEMINI_API_KEY` | Required only for ops or examples that select `provider: "gemini"` (still with the default factory) |
| `RUN_LIVE_AI=1` | Opt-in to live-API integration tests under `library/`; tests skip otherwise even when `CLAUDE_API_KEY` is set |

Neither key is read when a custom `AIClientFactory` is registered — see [Custom credential routing](#custom-credential-routing).

## Dependencies

- [dagor](https://github.com/wwz16/dagor) — DAG execution engine
- [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) — Claude API client
- [ants/v2](https://github.com/panjf2000/ants) — goroutine worker pool
