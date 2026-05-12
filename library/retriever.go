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

// retrievalFiltersKey is the unexported context key under which
// RetrieveWithFiltersOp installs request-scoped filters before calling
// Retriever.Retrieve. Retriever implementations read them via
// RetrievalFiltersFromContext.
type retrievalFiltersKey struct{}

// WithRetrievalFilters returns a new context carrying the given filters.
// RetrieveWithFiltersOp calls this to make filter values produced by upstream
// graph ops available to Retriever implementations through ctx. Plain
// RetrieveOp does not install anything — callers who want static filters can
// install them themselves before engine.Run.
//
// The map shape is stringly-typed by convention so filters compose with the
// DAG's string ops and stringly-typed vertex params. Retriever
// implementations own the interpretation of each value — parsing numbers,
// splitting CSV lists, treating them as ID prefixes, mapping them to a
// vector-store metadata predicate, and so on. Document what your Retriever
// understands; unknown filter keys should be ignored, not errored.
//
// The returned context shares the filter map with ctx; treat it as
// read-only. Empty or nil filters return ctx unchanged.
func WithRetrievalFilters(ctx context.Context, filters map[string]string) context.Context {
	if len(filters) == 0 {
		return ctx
	}
	return context.WithValue(ctx, retrievalFiltersKey{}, filters)
}

// RetrievalFiltersFromContext extracts the filters installed by
// WithRetrievalFilters. Retriever implementations call this to read
// request-scoped filters. Returns ok=false when no filters are present —
// implementations should fall back to unfiltered retrieval in that case.
//
// The returned map must not be mutated; concurrent retrievers may share it.
func RetrievalFiltersFromContext(ctx context.Context) (map[string]string, bool) {
	f, ok := ctx.Value(retrievalFiltersKey{}).(map[string]string)
	return f, ok
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
