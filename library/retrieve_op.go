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

const RetrieveOpDescription = `RetrieveOp: pulls the top-k documents most relevant to a query from a registered Retriever (RAG fan-in).
  Params:   k string — number of documents to return (default "5"). Lower values keep prompts compact; higher values broaden recall.
            retriever_id string — selects a Retriever registered via library.RegisterRetriever (default "" → process default set via library.SetDefaultRetriever).
  Inputs:   Query *string — the natural-language search query.
  Outputs:  Documents []library.Document — full records ({ID, Content, Score}) sorted best-first. Use when downstream ops need IDs or scores.
            Texts []string — parallel slice of Documents[i].Content in the same order. Wire this into AI ops that take *[]string (AISummarizeOp, AIRerankOp, AIBestMatchOp candidates).
  Setup is fail-fast: if no Retriever is registered the graph errors before any vertex runs. Call SetDefaultRetriever (or RegisterRetriever) before engine.Run.`

// RetrieveOp wraps any registered Retriever so a graph can pull external
// context (knowledge base, vector store, search service) into a downstream AI
// op. Documents and Texts are parallel: Texts[i] == Documents[i].Content.
type RetrieveOp struct {
	Query     *string    `dag:"input"`
	Documents []Document `dag:"output"`
	Texts     []string   `dag:"output"`

	k           int
	retrieverID string
	retriever   Retriever
}

func (op *RetrieveOp) Setup(params *config.Params) error {
	op.k = 5
	if s := params.GetString("k", ""); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("RetrieveOp: param k = %q: %w", s, err)
		}
		if n <= 0 {
			return fmt.Errorf("RetrieveOp: param k must be positive, got %d", n)
		}
		op.k = n
	}
	op.retrieverID = params.GetString("retriever_id", "")
	r, err := resolveRetriever(op.retrieverID)
	if err != nil {
		return fmt.Errorf("RetrieveOp: %w", err)
	}
	op.retriever = r
	return nil
}

func (op *RetrieveOp) Reset() error { return nil }

func (op *RetrieveOp) Run(ctx context.Context) error {
	slog.DebugContext(ctx, "RetrieveOp.run", "run_id", dagor.RunID(ctx), "k", op.k, "retriever_id", op.retrieverID)

	docs, err := op.retriever.Retrieve(ctx, *op.Query, op.k)
	if err != nil {
		return fmt.Errorf("RetrieveOp: retrieve: %w", err)
	}
	op.Documents = docs
	op.Texts = make([]string, len(docs))
	for i, d := range docs {
		op.Texts[i] = d.Content
	}
	slog.InfoContext(ctx, "RetrieveOp.retrieved", "run_id", dagor.RunID(ctx), "count", len(docs))
	return nil
}

func init() {
	operator.RegisterOp[RetrieveOp]()
}
