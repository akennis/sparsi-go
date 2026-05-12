package library

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/operator"
)

const RetrieveWithFiltersOpDescription = `RetrieveWithFiltersOp: like RetrieveOp but with a runtime Filters input, for retrieval that depends on values produced by upstream graph ops (tenant id from an auth step, category from a classifier, date range from a planner, etc.).
  Params:   k string — number of documents to return (default "5").
            retriever_id string — selects a Retriever registered via library.RegisterRetriever (default "" → process default).
  Inputs:   Query *string — the natural-language search query.
            Filters *map[string]string — request-scoped filters. Installed into ctx via library.WithRetrievalFilters; the Retriever reads them via library.RetrievalFiltersFromContext. Values are strings by convention — Retriever implementations parse numbers, split CSV lists, etc. Empty or nil map skips installation (equivalent to RetrieveOp).
  Outputs:  Documents []library.Document — full records sorted best-first.
            Texts []string — parallel slice of Documents[i].Content.
  Use this op when filter values are dynamic; use plain RetrieveOp when no filters are needed. Retriever implementations that don't understand the filter keys must ignore them, not error.`

// RetrieveWithFiltersOp is the dynamic-filter sibling of RetrieveOp. It
// installs filters from the Filters input wire into ctx via
// WithRetrievalFilters before delegating to the resolved Retriever, so
// Retriever implementations see request-scoped filters without the framework
// or the underlying interface having to enumerate them.
type RetrieveWithFiltersOp struct {
	Query     *string            `dag:"input"`
	Filters   *map[string]string `dag:"input"`
	Documents []Document         `dag:"output"`
	Texts     []string           `dag:"output"`

	k           int
	retrieverID string
	retriever   Retriever
}

func (op *RetrieveWithFiltersOp) Setup(params *config.Params) error {
	op.k = 5
	if s := params.GetString("k", ""); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("RetrieveWithFiltersOp: param k = %q: %w", s, err)
		}
		if n <= 0 {
			return fmt.Errorf("RetrieveWithFiltersOp: param k must be positive, got %d", n)
		}
		op.k = n
	}
	op.retrieverID = params.GetString("retriever_id", "")
	r, err := resolveRetriever(op.retrieverID)
	if err != nil {
		return fmt.Errorf("RetrieveWithFiltersOp: %w", err)
	}
	op.retriever = r
	return nil
}

func (op *RetrieveWithFiltersOp) Reset() error { return nil }

func (op *RetrieveWithFiltersOp) Run(ctx context.Context) error {
	filters := *op.Filters
	slog.DebugContext(ctx, "RetrieveWithFiltersOp.run", "run_id", dagor.RunID(ctx), "k", op.k, "retriever_id", op.retrieverID, "filter_count", len(filters))

	ctx = WithRetrievalFilters(ctx, filters)

	docs, err := op.retriever.Retrieve(ctx, *op.Query, op.k)
	if err != nil {
		return fmt.Errorf("RetrieveWithFiltersOp: retrieve: %w", err)
	}
	op.Documents = docs
	op.Texts = make([]string, len(docs))
	for i, d := range docs {
		op.Texts[i] = d.Content
	}
	slog.InfoContext(ctx, "RetrieveWithFiltersOp.retrieved", "run_id", dagor.RunID(ctx), "count", len(docs))
	return nil
}

func init() {
	operator.RegisterOp[RetrieveWithFiltersOp]()
}
