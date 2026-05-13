# rag-gemini-embed

Retrieval-augmented question answering over a small local knowledge base,
**using Gemini embeddings + cosine similarity** for retrieval, with
source-file citations.

The example loads every `.txt` file under `testdata/kb/`, tagging each
`library.Document` with `Metadata[library.MetadataSource] = "<filename>.txt"`
(`library.MetadataSource == "source"`). At
startup it embeds every passage with Gemini's `gemini-embedding-001` model
via `library.ResolveEmbeddingClient` (the framework's
`EmbeddingClientFactory` abstraction — no env-var reads in this example's
code) and stores the vectors in memory. Each request embeds the query
with the same model and ranks the corpus by cosine similarity.

The graph is identical in shape to `rag-bm25` — seven vertices:

1. lifts the question into the graph (`question_const`)
2. retrieves the top-3 matching passages (`RetrieveOp`, k=3)
3. labels each passage with its source filename and wraps it in a
   `<passage>` tag (`BuildRAGPromptOp`)
4. extracts the allow-list of source identifiers actually present in the
   retrieved passages (`RetrievedSourcesOp`, custom inline op)
5. asks the LLM to answer using only the provided context and to end
   with a `Sources: <files>` trailer (`AIComputeStringToStringOp`)
6. splits the response into answer body and cited filenames
   (`ParseCitationsOp`)
7. filters the parsed citations against the retrieved allow-list
   (`ValidateCitationsOp`) — hallucinated filenames land in `Rejected`,
   surviving entries in `Accepted`

The point of this example is the **credential plumbing** — a
vector-store-backed `Retriever` consumes the framework's
`EmbeddingClientFactory` the same way AI ops consume `AIClientFactory`,
including per-vertex routing via the `credential_ref` /
`client_factory_id` / `api_factory_timeout_ms` params on `RetrieveOp`.
The retrieval logic itself (in-memory cosine over Gemini vectors) is the
simplest possible vector backend; production deployments swap it for
pgvector, sqlite-vec, Pinecone, Weaviate, or a hosted search service and
keep the rest of the code identical.

**Security:** `loadKB` follows symlinks. The shipped `testdata/kb`
directory is fine, but in a multi-tenant or untrusted-input setting,
pointing `loadKB` at a user-controlled directory ships a path-traversal /
arbitrary-file-read vector (the user can symlink any readable file into
the directory). Sandbox the directory or replace `os.ReadFile` with a
symlink-rejecting reader before adapting this loader for production.

## Running

```
export GEMINI_API_KEY=...       # used by the bundled EnvEmbeddingClientFactory
export CLAUDE_API_KEY=sk-...    # used by the answer vertex (claude-sonnet-4-6)
cd examples/rag-gemini-embed
go run . --question "How long does shipping take?"
```

Stderr prints the indexing event and which passages cosine-ranked at the
top (with their similarity scores) so you can see retrieval working before
the LLM answers. Stdout receives the answer body followed by a blank line
and `Sources: file1.txt, file2.txt`.

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
`"I don't know based on the provided context."` when the retrieved
passages don't cover the question.

## Plugging in a non-env credential source

Override the default factory with anything that implements
`library.EmbeddingClientFactory` — Vault, AWS Secrets Manager, GCP Secret
Manager, Azure Key Vault, workload identity. The shape mirrors
`AIClientFactory` exactly:

```go
type vaultEmbeddingFactory struct{ /* … */ }

func (f *vaultEmbeddingFactory) Embedder(ctx context.Context, provider, model, ref string) (library.EmbeddingClient, error) {
    secret, err := f.vault.Read(ctx, ref)         // ref forwarded verbatim from vertex param
    if err != nil {
        return nil, err
    }
    return f.buildClient(provider, model, secret) // your SDK / HTTP wrapper
}

func main() {
    library.SetDefaultEmbeddingClientFactory(&vaultEmbeddingFactory{ /* … */ })
    // ... build and run the graph as in main.go
}
```

For multi-tenant routing, register per-tenant factories under named ids:

```go
library.RegisterEmbeddingClientFactory("tenant-a", tenantAFactory)
library.RegisterEmbeddingClientFactory("tenant-b", tenantBFactory)
```

Then set `client_factory_id` (and optionally `credential_ref`) on the
`retrieve` vertex in the graph:

```go
.Vertex("retrieve").Op("RetrieveOp").
  Params(map[string]string{
    "k":                 "3",
    "client_factory_id": "tenant-a",
    "credential_ref":    "secret/tenant-a/voyage",
  }).
  ...
```

`RetrieveOp` installs those values into ctx via
`library.WithEmbeddingCredentials`; the retriever's
`library.ResolveEmbeddingClient(ctx, "gemini", model)` call inside
`embed_retriever.go` picks them up automatically.

## Why Gemini

Gemini's embedding API is already a transitive dependency of this
framework (the `genai` SDK is used by `library.EnvAIClientFactory` for
chat), so this example needs no new `go.mod` entries. To use a different
embedder (Voyage, OpenAI, Cohere), implement
`library.EmbeddingClientFactory` for that provider and register it; the
retriever code in `embed_retriever.go` would just change its
`ResolveEmbeddingClient(ctx, "<provider>", "<model>")` call site.
