# rag-bm25

Retrieval-augmented question answering over a small local knowledge base.

The example loads every `.txt` file under `testdata/kb/`, indexes them with
an in-memory BM25 retriever (`bm25.go`), registers it as the process default
via `library.SetDefaultRetriever`, then runs a 4-vertex DAG that:

1. lifts the question into the graph (`question_const`)
2. retrieves the top-3 matching passages (`RetrieveOp`, k=3)
3. assembles a prompt that grounds the LLM in those passages (`BuildRAGPromptOp`)
4. asks Claude for an answer (`AIComputeStringToStringOp`)

The point of the example is the **pattern**, not BM25 specifically. To swap
in any other backend — pgvector, sqlite-vec, Pinecone, a hosted search
service — implement `library.Retriever` and call `SetDefaultRetriever` with
your implementation. The graph stays identical.

## Running

```
export CLAUDE_API_KEY=sk-...
cd examples/rag-bm25
go run . --question "How long does shipping take?"
```

Stderr will print which passages BM25 selected (with their relevance scores)
so you can see the retrieval working before the LLM answers. Stdout receives
the final answer.

## Try these

```
go run . --question "How do I return an item?"
go run . --question "Is my heat pump supported?"
go run . --question "What payment methods do you accept?"
go run . --question "My display is blank, what should I do?"
```

Ask something off-corpus to see the grounded behavior:

```
go run . --question "What's the weather like today?"
```

The prompt instructs the model to reply
`"I don't know based on the provided context."` when the retrieved passages
don't cover the question.

## Plugging in your own retriever

```go
type MyRetriever struct{ /* … */ }

func (r *MyRetriever) Retrieve(ctx context.Context, query string, k int) ([]library.Document, error) {
    // call your vector store / search service here
}

func main() {
    library.SetDefaultRetriever(&MyRetriever{ /* … */ })
    // ... build and run the graph as in main.go
}
```

`Document.ID` and `Document.Score` are populated by you; downstream graph
vertices can wire `Documents` for ID/score awareness or `Texts` (the
parallel `[]string`) for the common case of just feeding content into an AI
op.
