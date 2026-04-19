# Dagor LLM Hints

Dagor is a high-performance DAG (Directed Acyclic Graph) execution engine for Go. It supports conditional branching, vertex-level parallelism, and parameter-based configuration.

## Operator Implementation

Every operator must implement the `IOperator` interface. Below is a concise example of an operator that adds two integers.

```go
type AddOp struct {
	A      *int `dag:"input"`
	B      *int `dag:"input"`
	Result int  `dag:"output"`
}

func (op *AddOp) Setup(_ *config.Params) error { return nil }
func (op *AddOp) Reset() error                 { return nil }
func (op *AddOp) ResetFields()                 { op.A = nil; op.B = nil; op.Result = 0 }

func (op *AddOp) Run(ctx context.Context) error {
	if op.A == nil || op.B == nil {
		return fmt.Errorf("AddOp: missing required inputs")
	}
	op.Result = *op.A + *op.B
	return nil
}

// Boilerplate for IOperator
func (op *AddOp) InputFields() map[string]any  { return map[string]any{"A": &op.A, "B": &op.B} }
func (op *AddOp) OutputFields() map[string]any { return map[string]any{"Result": &op.Result} }
func (op *AddOp) SetInputField(field string, value any) error {
	if value == nil { return nil }
	ptr, ok := value.(*int)
	if !ok { return fmt.Errorf("AddOp: field %s expected *int", field) }
	if field == "A" { op.A = ptr } else if field == "B" { op.B = ptr }
	return nil
}

func init() {
	operator.RegisterOp[AddOp]()
}
```

## Conditional Branching & Merging

Dagor allows vertices to be executed conditionally using **Predicates**. When multiple conditional branches need to converge, use the `Coalesce` merge strategy.

### 1. Registering Predicates
Predicates are functions that take a map of inputs and return a boolean.

```go
predicate.Register("is_positive", func(inputs map[string]any) bool {
    val, ok := inputs["source_out"].(*int)
    return ok && val != nil && *val > 0
})
```

### 2. Building a Conditional Graph
The `Builder` DSL is used to define the DAG. The example below shows a graph that branches based on a value and coalesces the results.

```go
// source ──► positive_branch (if positive) ──► coalesce (MergeCoalesce)
//        └─► negative_branch (if negative) ──► 
func buildGraph(sourceVal int) (*graph.Graph, error) {
    return graph.NewBuilder("conditional_demo").
        Vertex("source").
        Op("SourceOp").
        Params(map[string]int{"value": sourceVal}).
        Output("out", "source_out").

        Vertex("pos_branch").
        Op("PositiveOp").
        Condition("is_positive"). // Only runs if "is_positive" is true
        Input("in", "source_out").
        Output("out", "pos_out").

        Vertex("neg_branch").
        Op("NegativeOp").
        Condition("is_negative").
        Input("in", "source_out").
        Output("out", "neg_out").

        Vertex("coalesce").
        Op("CoalesceIntOp"). // Built-in: returns the first non-nil input
        Merge(config.MergeCoalesce). // CRITICAL: Prevents "skip" propagation
        Input("A", "pos_out").
        Input("B", "neg_out").
        Output("Result", "final_out").

        Build()
}
```

## Key Concepts

- **Vertex**: A node in the DAG. Has a unique name and an Operator.
- **Operator (`Op`)**: The logic executed at a vertex.
- **Condition**: A named predicate that determines if a vertex should run.
- **Merge (`config.MergeCoalesce`)**: Used when a vertex depends on multiple conditional branches. It ensures that the vertex runs even if some upstream branches are skipped, as long as at least one branch provides data.
- **Input/Output Mapping**: `Input("op_field", "global_name")` and `Output("op_field", "global_name")` map operator fields to global names in the graph's scope.
- **CoalesceOp**: A built-in operator (`CoalesceIntOp`, `CoalesceStringOp`, etc.) that picks the first non-nil input from its branches.
