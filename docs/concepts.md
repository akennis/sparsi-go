# Core Concepts

## The DAG model

Workflows are DAGs built from operators (ops). Each op is a Go struct with `dag:"input"` and `dag:"output"` field tags. The `daggen` code-generation tool reads those tags and generates boilerplate interface methods (`InputFields`, `OutputFields`, `SetInputField`, `ResetFields`). The [dagor](https://github.com/akennis/dagor) engine resolves dependencies, schedules ops in parallel, and threads **wire values** between them — a `Vertex(...).Output("Result", "x")` writes wire `x`; a downstream `Vertex(...).Input("A", "x")` reads it.

## Params vs `ContextValOp`

A vertex gets configuration from two places:

- **Params** — static configuration that is part of the op's definition: operation names, map keys, regex patterns, flags. Params are JSON-encoded into the graph config and read by the op's `Setup` method, so they only work for JSON-representable types.
- **`ContextValOp`** — any value that varies per execution: user input, request data, file content, computed URLs, API responses. The graph is built once; each `eng.Run(ctx)` supplies a different value through `context.Context`.

### Injecting per-execution values

Use `ContextValOp` from `github.com/akennis/dagor/operator/builtin` to inject per-execution values. The graph is built once at startup; each `eng.Run(ctx)` call supplies different values through the context — the key pattern for request pipelines and servers.

```go
import builtin "github.com/akennis/dagor/operator/builtin"

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
	// ... downstream vertices consume the "items" and "threshold" wires
	Build()

// Inject values at run time — a new Engine per call, same graph:
ctx := context.WithValue(context.Background(), itemsKey{}, []string{"foo", "bar", "baz"})
ctx = context.WithValue(ctx, thresholdKey{}, 0.75)
eng, _ := dagor.NewEngine(g, pool)
eng.Run(ctx)
```

`ContextValFactory[T](key)` returns a factory for `operator.RegisterOpFactory`. The resulting op reads the value from the context key at each `Run` call; it errors if the key is missing or has the wrong type. Context keys must be unexported struct types to avoid collisions.

For truly static constants, `library.RegisterConst[T](name, value)` registers a `ConstOp[T]` that emits a fixed value captured at registration.
