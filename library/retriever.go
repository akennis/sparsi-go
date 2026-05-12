package library

import (
	"context"
	"fmt"
	"sync"
)

// Document is a single retrieved item. ID identifies it within the corpus
// (filename, primary key, vector-store ID — Retriever implementations choose).
// Score is populated by Retrieve and conventionally orders results best-first.
type Document struct {
	ID      string
	Content string
	Score   float64
}

// Retriever finds the k documents most relevant to a query. Implementations
// are free to use any backend (BM25, embeddings + vector store, hosted search
// service); the library never sees their internals.
//
// Implementations must be safe for concurrent Retrieve calls — graph
// execution may invoke a single Retriever from multiple parallel vertices.
type Retriever interface {
	Retrieve(ctx context.Context, query string, k int) ([]Document, error)
}

var (
	retrieverMu       sync.RWMutex
	defaultRetriever  Retriever
	retrieverRegistry = map[string]Retriever{}
)

// SetDefaultRetriever replaces the process-wide default Retriever. Call once
// at program start, before running any graph that contains a RetrieveOp.
// Passing nil clears the default.
func SetDefaultRetriever(r Retriever) {
	retrieverMu.Lock()
	defer retrieverMu.Unlock()
	defaultRetriever = r
}

// RegisterRetriever registers a Retriever under an id. RetrieveOp vertices opt
// in by setting the retriever_id vertex param; unknown ids fall back to the
// process-wide default. Passing nil deregisters.
func RegisterRetriever(id string, r Retriever) {
	retrieverMu.Lock()
	defer retrieverMu.Unlock()
	if r == nil {
		delete(retrieverRegistry, id)
		return
	}
	retrieverRegistry[id] = r
}

// resolveRetriever looks up an id in the registry; missing ids fall back to
// the process-wide default. Returns an error if neither path yields a
// Retriever — RetrieveOp surfaces this at Setup time so misconfigured graphs
// fail fast instead of at first Run.
func resolveRetriever(id string) (Retriever, error) {
	retrieverMu.RLock()
	defer retrieverMu.RUnlock()
	if id != "" {
		if r, ok := retrieverRegistry[id]; ok {
			return r, nil
		}
	}
	if defaultRetriever != nil {
		return defaultRetriever, nil
	}
	if id != "" {
		return nil, fmt.Errorf("retriever %q is not registered and no default Retriever is set; call library.RegisterRetriever or library.SetDefaultRetriever before running the graph", id)
	}
	return nil, fmt.Errorf("no default Retriever is set; call library.SetDefaultRetriever before running the graph")
}
