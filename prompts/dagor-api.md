# Dagor API Reference

Dagor is a DAG execution engine for Go. This document is the **complete, authoritative API reference**
for code generation. If an API is not listed here, it does not exist — do NOT invent or assume methods
on any type. Every `eng.X`, `g.X`, `b.X`, or package-level call must appear in this document before
you write it.

---

## COMPLETE ENGINE API — `*dagor.Engine`

There are exactly **three** callable methods on `*Engine` after construction. No others exist.

```
func (e *Engine) Run(ctx context.Context) error
```
Executes the graph. Runs vertices in dependency order, respecting conditions and merge strategy.
Pass a context with a deadline/timeout. Returns the first non-nil error from any vertex.

```
func (e *Engine) GetOutput(wire string) (any, bool)
```
Reads a wire value after `Run` completes. Returns the value and `true` when the wire was produced,
`false` when the producing vertex was skipped or the wire name is unknown. Cast the `any` result to
the expected concrete pointer type (e.g. `*string`, `*int`, `*float64`).

```
func (e *Engine) VertexSkipped(name string) bool
```
Reports whether the named vertex was skipped during the last `Run`. Use only to gate post-run display
logic in `main()`. **Never** use it to select between branch results — coalesce wires instead.

### Engine construction

```
func dagor.NewEngine(g *graph.Graph, pool runtime.IGPool, opts ...Option) (*Engine, error)
```
Creates an engine from a compiled graph and a goroutine pool. Always pass `dagor.WithReporter(...)`.

```
func dagor.WithReporter(r Reporter) Option
```
Option that attaches a structured-log reporter to the engine. **Required** on every `NewEngine` call.

### Package-level utility

```
func dagor.RunID(ctx context.Context) string
```
Returns the workflow run ID embedded in the context by `Engine.Run`. Returns `""` if called outside
a running engine. Use in every `slog.DebugContext` call inside an operator to correlate log lines
with the reporter's vertex events.

### PROHIBITED — methods that do NOT exist on Engine
`eng.SetInput`, `eng.SetWire`, `eng.Inject`, `eng.AddVertex`, `eng.Vertices`, `eng.Graph` — none
of these exist. **Do not write them.** To inject external values into the DAG, use `ContextValFactory`
(see below).

---

## INJECTING EXTERNAL INPUTS — `builtin.ContextValFactory`

`eng.SetInput` does not exist. The only correct way to inject a runtime value (CLI flag, user text,
env var, etc.) into the DAG is via `context.WithValue` + `ContextValFactory`.

```
func builtin.ContextValFactory[T any](key any) func() operator.IOperator
```
Returns an operator factory that, at run time, reads the value stored under `key` from the context
and emits it on the `"Result"` wire. Register the factory with `operator.RegisterOpFactory`, then
reference it by name in the graph builder.

**Required pattern — use exactly this:**
```go
// 1. Declare a package-level key type (unexported, prevents collisions)
type ctxKey string
const inputKey ctxKey = "input"

// 2. Register a named factory once, at program start (in an init or main)
operator.RegisterOpFactory("InputOp", builtin.ContextValFactory[string](inputKey))

// 3. Store the value in the context before calling eng.Run
ctx = context.WithValue(ctx, inputKey, userText)

// 4. Reference the factory by name in the graph builder
graph.NewBuilder("my_graph").
    Vertex("input_vertex").
    Op("InputOp").
    Output("Result", "input_wire").
    ...
```

The import for `ContextValFactory` is `"github.com/wwz16/dagor/operator/builtin"`.
This is the same blank import already required for coalesce ops — `_ "github.com/wwz16/dagor/operator/builtin"` —
but to call `builtin.ContextValFactory` you need it named, not blank:
`builtin "github.com/wwz16/dagor/operator/builtin"`.

---

## OPERATOR REGISTRATION — `operator` package

```
func operator.RegisterOp[T any]() error
```
Registers the concrete struct type `T` under its type name. Call from `init()`. Used for every custom
op and every concrete `AIComputeOp` wrapper.

```
func operator.RegisterOpFactory(name string, factory func() operator.IOperator) error
```
Registers an operator under an explicit string name using a caller-supplied factory function. Required
for generic factory functions like `ContextValFactory` where the type name cannot be inferred.

---

## GRAPH BUILDER DSL — `graph` package

```
func graph.NewBuilder(name string) *Builder
```
Creates a new fluent graph builder. `name` is a label used in log output; it is not a wire name.

### On `*Builder`

```
func (b *Builder) Vertex(name string) *VertexBuilder
```
Adds a new vertex with the given unique name and returns a `*VertexBuilder` to configure it.

```
func (b *Builder) Build() (*Graph, error)
```
Compiles the builder configuration into an immutable `*Graph`. Call once after all vertices are defined.

### On `*VertexBuilder`

All methods return `*VertexBuilder` and chain fluently. Call `Vertex(...)` or `Build()` to move on.

```
func (vb *VertexBuilder) Op(name string) *VertexBuilder
```
Sets the operator for this vertex by registered name (e.g. `"AddOp"`, `"CoalesceStringOp"`).
Mutually exclusive with `MapOver`, `FilterBy`, and `ReduceBy`.

```
func (vb *VertexBuilder) Params(p any) *VertexBuilder
```
Passes a typed config value (e.g. `map[string]int{"n": 3}`) to the operator's `Setup` method.

```
func (vb *VertexBuilder) Input(opField, wire string) *VertexBuilder
```
Wires an upstream output wire `wire` into the operator field named `opField`.

```
func (vb *VertexBuilder) Output(opField, wire string) *VertexBuilder
```
Publishes the operator field named `opField` onto the global wire named `wire`.

```
func (vb *VertexBuilder) Condition(predName string) *VertexBuilder
```
Attaches a named predicate: the vertex runs only when the predicate returns `true`.

```
func (vb *VertexBuilder) ConditionInput(wire string) *VertexBuilder
```
Makes the wire `wire` available to the predicate without the vertex consuming it as an operator input.
Use when a predicate needs a wire that the op struct does not have a field for.

```
func (vb *VertexBuilder) PassthroughWire(outputField, sourceWire string) *VertexBuilder
```
When this vertex is skipped, copies the value of `sourceWire` onto `outputField`'s output wire so
downstream coalesce ops see a non-nil slot.

```
func (vb *VertexBuilder) Merge(strategy string) *VertexBuilder
```
Sets the merge strategy. Pass `config.MergeCoalesce` (the only valid constant) when this vertex
depends on multiple conditional branches; without it, a skipped branch propagates skip and the vertex
never fires.

```
func (vb *VertexBuilder) OnError(action string) *VertexBuilder
```
Sets the error action for the vertex (rarely needed in generated code).

```
func (vb *VertexBuilder) MapOver(itemWire string) *MapConfigBuilder
```
Converts the vertex into a map node that fans out over a slice wire. No `Op()` may be set.

```
func (vb *VertexBuilder) FilterBy(predicateName string) *FilterConfigBuilder
```
Converts the vertex into a filter node.

```
func (vb *VertexBuilder) ReduceBy(reducerName string) *ReduceConfigBuilder
```
Converts the vertex into a reduce node.

```
func (vb *VertexBuilder) Done() *Builder
```
Returns to the parent builder after configuring a map/filter/reduce sub-graph.

```
func (vb *VertexBuilder) Vertex(name string) *VertexBuilder
```
Shortcut — equivalent to `Done().Vertex(name)`.

```
func (vb *VertexBuilder) Build() (*Graph, error)
```
Shortcut — equivalent to `Done().Build()`.

---

## PREDICATE REGISTRATION — `predicate` package

```
func predicate.Register(name string, pred func(inputs map[string]any) bool) error
```
Registers a named predicate. Keys in `inputs` are **wire names**, not op field names. Call from
`main()` before `buildGraph`. Returns an error if the name is already registered.

---

## REPORTER — `reporter` package

```
func reporter.New(logger *slog.Logger) *reporter.SlogReporter
```
Creates a reporter that emits structured log lines for every graph/vertex lifecycle event and all
operator input/output field values. Pass to `dagor.WithReporter(...)`. **Required** on every engine.
Do not log input/output values manually in `Run` — the reporter already captures them.

---

## config.Params API — operator Setup

`*config.Params` is passed to `Setup`. Every getter takes `(path, defaultValue)` and returns **one**
value. There is no two-return-value form.

```
p.GetString(path, defaultValue string) string
p.GetInt(path string, defaultValue int) int
p.GetInt64(path string, defaultValue int64) int64
p.GetFloat64(path string, defaultValue float64) float64
p.GetBool(path string, defaultValue bool) bool
p.Exists(path string) bool
p.GetArrayString(path string) []string
p.GetArrayInt64(path string) []int64
p.GetArrayFloat64(path string) []float64
```

```go
// CORRECT:
op.name = p.GetString("name", "default")
op.n    = p.GetInt("n", 0)

// WRONG — compile error (returns 1 value, not 2):
v, ok := p.GetString("name", "")
```

---

## DAGOR PACKAGE IMPORTS

Only import what you use. Do not invent import paths.

```go
"github.com/panjf2000/ants/v2"               // goroutine pool — required by NewEngine
"github.com/wwz16/dagor"                     // dagor.NewEngine, dagor.WithReporter, dagor.RunID
"github.com/wwz16/dagor/config"              // config.MergeCoalesce
"github.com/wwz16/dagor/graph"               // graph.NewBuilder
"github.com/wwz16/dagor/operator"            // operator.RegisterOp, operator.RegisterOpFactory
"github.com/wwz16/dagor/operator/builtin"    // Coalesce*Op + ContextValFactory
                                             // use blank _ when not calling builtin.* directly
"github.com/wwz16/dagor/predicate"           // predicate.Register
"github.com/wwz16/dagor/reporter"            // reporter.New
```

---

## OPERATOR IMPLEMENTATION CONTRACT

Every custom op must implement `IOperator`:

```go
type IOperator interface {
    Setup(p *config.Params) error          // called once at engine init; read Params here
    Reset() error                           // called before each Run in pool-reuse scenarios
    Run(ctx context.Context) error          // executes the operator logic
    InputFields() map[string]any            // returns map[opFieldName → pointer-to-field]
    OutputFields() map[string]any           // returns map[opFieldName → pointer-to-field]
    SetInputField(field string, value any) error // sets a single input field from a wire value
    ResetFields()                           // zeroes all input and output fields
}
```

Library ops with `dag:"input"` / `dag:"output"` struct tags have `InputFields`, `OutputFields`,
`SetInputField`, and `ResetFields` generated by `daggen` — do NOT write them manually for those ops.

Minimal hand-written op:
```go
type MyOp struct {
    In  *string `dag:"input"`
    Out string  `dag:"output"`
}
func (op *MyOp) Setup(_ *config.Params) error { return nil }
func (op *MyOp) Reset() error                 { return nil }
func (op *MyOp) Run(_ context.Context) error {
    if op.In != nil { op.Out = *op.In }
    return nil
}
func (op *MyOp) InputFields() map[string]any  { return map[string]any{"In": &op.In} }
func (op *MyOp) OutputFields() map[string]any { return map[string]any{"Out": &op.Out} }
func (op *MyOp) SetInputField(f string, v any) error {
    if f == "In" { op.In = v.(*string); return nil }
    return fmt.Errorf("unknown field %s", f)
}
func (op *MyOp) ResetFields() { op.In = nil; op.Out = "" }
func init() { operator.RegisterOp[MyOp]() }
```

---

## HOW TO RUN A DAGOR GRAPH — exact pattern

```go
// Setup logging (once, before pool and engine)
slogLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
slog.SetDefault(slogLogger)

// Build graph
g, err := buildGraph()
if err != nil { log.Fatal(err) }

// Create pool and engine
pool, _ := ants.NewPool(10)
defer pool.Release()
eng, err := dagor.NewEngine(g, pool, dagor.WithReporter(reporter.New(slog.Default())))
if err != nil { log.Fatal(err) }

// Store external inputs in context, then run
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()
ctx = context.WithValue(ctx, inputKey, userInput) // inject external values HERE
if err := eng.Run(ctx); err != nil { log.Fatal(err) }

// Read results
raw, ok := eng.GetOutput("final_wire") // returns (any, bool)
if ok {
    result := raw.(*string) // cast to concrete pointer type
}
```

---

## COALESCE OPS — registered by `_ "github.com/wwz16/dagor/operator/builtin"`

2-input (`A`, `B` → `Result`) — first non-nil pointer wins:
- `CoalesceStringOp`, `CoalesceIntOp`, `CoalesceFloat64Op`, `CoalesceBoolOp`

N-input (`Input0`…`Input(n-1)` → `Result`; requires `Params(map[string]int{"n": <count>})`):
- `CoalesceNStringOp`, `CoalesceNIntOp`, `CoalesceNFloat64Op`, `CoalesceNBoolOp`

Every coalesce vertex **must** include `.Merge(config.MergeCoalesce)`. Without it the engine
propagates skip from any skipped upstream branch and the vertex never fires.

```go
Vertex("merge_result").
    Op("CoalesceStringOp").
    Merge(config.MergeCoalesce).
    Input("A", "branch_a_wire").
    Input("B", "branch_b_wire").
    Output("Result", "final_wire").
```

---

## MAP NODES — fan-out over a slice

Map vertices have **no `Op()`** — `MapOver` replaces the operator.

```go
Vertex("map_items").
    Input("Items", "slice_wire").
    MapOver("item").          // "item" is the element wire name inside the sub-graph
        SubVertex("process").
            Op("ProcessOp").
            Input("In", "item").
            Output("Out", "result").
        CollectInto("result", "results_wire"). // terminates sub-graph; output is always []any
```

Read results:
```go
raw, ok := eng.GetOutput("results_wire")
results := raw.([]any)
for _, v := range results {
    s := v.(string)
}
```

---

## PREDICATES — wire names, not op field names

Predicate `inputs` keys are **wire names** set by `.Output("Field", "wire_name")` and
`.ConditionInput("wire_name")` — never op field names or output field names.

```go
predicate.Register("is_positive", func(inputs map[string]any) bool {
    val, ok := inputs["source_out"].(*int)  // "source_out" is the wire name
    return ok && val != nil && *val > 0
})
```

---

## LOGGING IN OPERATORS

```go
func (op *FetchOp) Run(ctx context.Context) error {
    slog.DebugContext(ctx, "FetchOp.run", "run_id", dagor.RunID(ctx))
    // ... do work ...
    slog.DebugContext(ctx, "FetchOp.done", "run_id", dagor.RunID(ctx), "bytes", len(op.Result))
    return nil
}
```

Rules:
- Always include `"run_id", dagor.RunID(ctx)` in every in-op `slog` call.
- Log only intermediate state not captured by the reporter (which already logs all input/output fields).
- Never use `log.Printf` inside ops. `log.Fatal` in `main()` for unrecoverable errors is fine.

