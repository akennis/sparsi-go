// Gemini-embedding retriever — an in-memory implementation of
// library.Retriever that ranks documents by cosine similarity between
// Gemini embeddings of the query and each KB passage.
//
// Embeddings come from library.ResolveEmbeddingClient, which routes through
// the framework's EmbeddingClientFactory abstraction (default reads
// GEMINI_API_KEY via the bundled EnvEmbeddingClientFactory). The retriever
// never reads env vars itself — that's the point of the credential
// plumbing.
//
// Suitable for small KBs (~hundreds of documents). For larger corpora, plug
// the cosine search into a real vector store (pgvector, sqlite-vec,
// Pinecone, Weaviate, …) and keep using ResolveEmbeddingClient for the
// query side.
package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/akennis/clawdag-go/library"
)

// GeminiVectorRetriever holds a fixed corpus and its precomputed embeddings.
// The corpus is immutable after NewGeminiVectorRetriever returns; rebuild a
// new instance to change it. Retrieve is read-only on indexed state, so
// concurrent calls are safe.
type GeminiVectorRetriever struct {
	docs    []library.Document
	vectors [][]float32 // vectors[i] is the embedding for docs[i].Content
	model   string

	// indexClient is the EmbeddingClient resolved at construction. The
	// retriever reuses it for query embedding at Retrieve time, falling back
	// to ResolveEmbeddingClient if the request ctx carries different
	// credentials than the indexing ctx did (multi-tenant case). It is set
	// once by NewGeminiVectorRetriever and never mutated afterward, so reads
	// need no synchronization.
	indexClient library.EmbeddingClient
}

// NewGeminiVectorRetriever embeds every doc up-front using
// library.ResolveEmbeddingClient. The supplied ctx provides the credential
// routing for the indexing call: pass context.Background() to use the
// process-default EmbeddingClientFactory (env-var bundled factory), or
// install routing via library.WithEmbeddingCredentials first for Vault /
// Secrets Manager / per-tenant setups.
//
// Returns an error if no documents are supplied, the embedding client can't
// be resolved, or the API rejects the indexing batch.
func NewGeminiVectorRetriever(ctx context.Context, docs []library.Document, model string) (*GeminiVectorRetriever, error) {
	if len(docs) == 0 {
		return nil, errors.New("GeminiVectorRetriever: no documents to index")
	}
	if model == "" {
		return nil, errors.New("GeminiVectorRetriever: empty model")
	}
	client, err := library.ResolveEmbeddingClient(ctx, "gemini", model)
	if err != nil {
		return nil, fmt.Errorf("GeminiVectorRetriever: resolve embedding client: %w", err)
	}
	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.Content
	}
	vectors, err := client.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("GeminiVectorRetriever: embed corpus: %w", err)
	}
	if len(vectors) != len(docs) {
		return nil, fmt.Errorf("GeminiVectorRetriever: embedding count mismatch: got %d, want %d", len(vectors), len(docs))
	}
	return &GeminiVectorRetriever{
		docs:        docs,
		vectors:     vectors,
		model:       model,
		indexClient: client,
	}, nil
}

// Retrieve embeds the query with the same model used for the corpus,
// computes cosine similarity against every indexed vector, and returns the
// top-k documents best-first. Each returned Document has its Score field
// populated with the cosine similarity in [-1, 1].
//
// Empty queries and empty corpora return nil without calling the API.
func (r *GeminiVectorRetriever) Retrieve(ctx context.Context, query string, k int) ([]library.Document, error) {
	if query == "" || len(r.docs) == 0 {
		return nil, nil
	}
	client, err := r.queryClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GeminiVectorRetriever: %w", err)
	}
	qVecs, err := client.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("GeminiVectorRetriever: embed query: %w", err)
	}
	if len(qVecs) != 1 {
		return nil, fmt.Errorf("GeminiVectorRetriever: query embedding count = %d, want 1", len(qVecs))
	}
	qVec := qVecs[0]

	scored := make([]library.Document, 0, len(r.docs))
	for i, d := range r.docs {
		cp := d
		cp.Score = cosineSimilarity(qVec, r.vectors[i])
		scored = append(scored, cp)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if k < len(scored) {
		scored = scored[:k]
	}
	return scored, nil
}

// queryClient returns the cached indexing client when the request carries
// no embedding-credential overrides; otherwise it resolves a fresh client
// using whatever credentials the request installed. This keeps the common
// single-tenant case cheap (one client cached for the process lifetime)
// while still honoring per-vertex routing when a workflow needs it.
func (r *GeminiVectorRetriever) queryClient(ctx context.Context) (library.EmbeddingClient, error) {
	creds := library.EmbeddingCredentialsFromContext(ctx)
	if creds.Ref == "" && creds.FactoryID == "" {
		if r.indexClient != nil {
			return r.indexClient, nil
		}
	}
	return library.ResolveEmbeddingClient(ctx, "gemini", r.model)
}

// cosineSimilarity returns dot(a,b) / (|a|·|b|) in [-1, 1]. Returns 0 when
// either vector is zero-length or zero-norm so the score is well-defined for
// degenerate inputs.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		na += ai * ai
		nb += bi * bi
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
