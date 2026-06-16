# Writing Workflows

## AI ops

AI ops default to Claude (`claude-sonnet-4-6`) but accept a `provider` param (`"claude"` or `"gemini"`) and a `model` param. They send structured prompts, retry on parse failure, and emit reasoning alongside the result.

**`AIComputeOp[In, Out]`** is a generic base. Concrete variants are defined by embedding it with typed input/output pairs.

Any concrete variant accepts an optional `SkipIf *string` input wire. When non-empty at runtime the AI call is skipped and that value is forwarded as the result — enabling a deterministic-first / AI-fallback pattern.

## Conditionals

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
```

## Map nodes

Fan out a sub-graph over each element of a slice. The MapOver vertex itself has no `.Op()` — its work is the sub-graph.

## Retrieval (RAG)

`RetrieveOp` fans external context into a graph via a registered `library.Retriever`.

**Citation re-validation.** When the model emits citations alongside an answer, treat the parsed list as untrusted. Wire `ValidateCitationsOp` between the citation parser and any downstream surface.
