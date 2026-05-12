// Package main demonstrates retrieval-augmented generation (RAG) over a small
// local knowledge base.
//
// On startup it loads every .txt file under testdata/kb/, indexes them with
// an in-memory BM25 retriever (bm25.go), and registers that retriever as the
// process default so library.RetrieveOp can find it.
//
// The graph is three vertices: RetrieveOp pulls the top-3 documents matching
// the user's question, BuildRAGPromptOp formats them into a single prompt
// alongside the question, and AIComputeStringToStringOp produces the answer.
//
// The pattern (define a Retriever, register it before Run, wire RetrieveOp
// into the graph) is the same regardless of backend — swap BM25 for a vector
// store or hosted search service by implementing library.Retriever.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akennis/clawdag-go/library"

	"github.com/panjf2000/ants/v2"
	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/graph"
	"github.com/wwz16/dagor/operator"
	builtin "github.com/wwz16/dagor/operator/builtin"
	"github.com/wwz16/dagor/reporter"
)

// ─── Context keys ──────────────────────────────────────────────────────────

type questionKey struct{}

// ─── Custom ops ────────────────────────────────────────────────────────────

// BuildRAGPromptOp combines the user's question with the retrieved passages
// into a single prompt for a string→string AI op. Texts is the parallel
// []string output of RetrieveOp — already in best-first order.
type BuildRAGPromptOp struct {
	Question *string   `dag:"input"`
	Context  *[]string `dag:"input"`
	Prompt   string    `dag:"output"`
}

func (op *BuildRAGPromptOp) Setup(_ *config.Params) error { return nil }
func (op *BuildRAGPromptOp) Reset() error                 { return nil }
func (op *BuildRAGPromptOp) Run(_ context.Context) error {
	var sb strings.Builder
	sb.WriteString("Answer the question using ONLY the provided context passages. ")
	sb.WriteString("If the context does not contain the answer, reply exactly: \"I don't know based on the provided context.\"\n\n")
	sb.WriteString("Context passages:\n")
	if len(*op.Context) == 0 {
		sb.WriteString("(no passages retrieved)\n")
	}
	for i, t := range *op.Context {
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, t)
	}
	sb.WriteString("\nQuestion: ")
	sb.WriteString(*op.Question)
	op.Prompt = sb.String()
	return nil
}
func (op *BuildRAGPromptOp) InputFields() map[string]any {
	return map[string]any{"Question": &op.Question, "Context": &op.Context}
}
func (op *BuildRAGPromptOp) OutputFields() map[string]any {
	return map[string]any{"Prompt": &op.Prompt}
}
func (op *BuildRAGPromptOp) SetInputField(field string, value any) error {
	switch field {
	case "Question":
		v, ok := value.(*string)
		if !ok {
			return fmt.Errorf("BuildRAGPromptOp: Question: expected *string, got %T", value)
		}
		op.Question = v
	case "Context":
		v, ok := value.(*[]string)
		if !ok {
			return fmt.Errorf("BuildRAGPromptOp: Context: expected *[]string, got %T", value)
		}
		op.Context = v
	default:
		return fmt.Errorf("BuildRAGPromptOp: unknown field %q", field)
	}
	return nil
}
func (op *BuildRAGPromptOp) ResetFields() {
	op.Question = nil
	op.Context = nil
	op.Prompt = ""
}

func init() {
	mustReg := func(name string, f func() operator.IOperator) {
		if err := operator.RegisterOpFactory(name, f); err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}
	mustReg("question_const", builtin.ContextValFactory[string](questionKey{}))

	if err := operator.RegisterOp[BuildRAGPromptOp](); err != nil {
		log.Fatalf("register BuildRAGPromptOp: %v", err)
	}
}

// ─── Graph ─────────────────────────────────────────────────────────────────

func buildGraph() (*graph.Graph, error) {
	return graph.NewBuilder("rag_bm25").
		Vertex("question_const").Op("question_const").
		Output("Result", "question").
		Vertex("retrieve").Op("RetrieveOp").
		Params(map[string]string{"k": "3"}).
		Input("Query", "question").
		Output("Documents", "documents").
		Output("Texts", "texts").
		Vertex("format_prompt").Op("BuildRAGPromptOp").
		Input("Question", "question").
		Input("Context", "texts").
		Output("Prompt", "prompt").
		Vertex("answer").Op("AIComputeStringToStringOp").
		Params(map[string]string{
			"operation": "answer the question grounded in the provided context",
			"provider":  "claude",
			"model":     "claude-sonnet-4-6",
		}).
		Input("Input", "prompt").
		Output("Result", "answer").
		Build()
}

// ─── Knowledge base ────────────────────────────────────────────────────────

func loadKB(dir string) ([]library.Document, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var docs []library.Document
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		docs = append(docs, library.Document{
			ID:      strings.TrimSuffix(e.Name(), ".txt"),
			Content: string(body),
		})
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no .txt files in %s", dir)
	}
	return docs, nil
}

// ─── Driver ────────────────────────────────────────────────────────────────

func main() {
	question := flag.String("question", "", "the question to answer using the knowledge base")
	kbDir := flag.String("kb", "testdata/kb", "directory of .txt knowledge base files")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if strings.TrimSpace(*question) == "" {
		fmt.Fprintln(os.Stderr, "usage: rag-bm25 --question \"<your question>\" [--kb <dir>]")
		os.Exit(2)
	}

	docs, err := loadKB(*kbDir)
	if err != nil {
		log.Fatalf("load knowledge base: %v", err)
	}
	library.SetDefaultRetriever(NewBM25Retriever(docs))

	g, err := buildGraph()
	if err != nil {
		log.Fatalf("build graph: %v", err)
	}

	pool, err := ants.NewPool(10)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	defer pool.Release()

	eng, err := dagor.NewEngine(g, pool, dagor.WithReporter(reporter.New(slog.Default())))
	if err != nil {
		log.Fatalf("create engine: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx = context.WithValue(ctx, questionKey{}, *question)

	if err := eng.Run(ctx); err != nil {
		log.Fatalf("run graph: %v", err)
	}

	if raw, ok := eng.GetOutput("documents"); ok {
		if p, ok := raw.(*[]library.Document); ok && p != nil {
			fmt.Fprintln(os.Stderr, "Retrieved passages:")
			for _, d := range *p {
				fmt.Fprintf(os.Stderr, "  [%s] score=%.3f\n", d.ID, d.Score)
			}
		}
	}

	if raw, ok := eng.GetOutput("answer"); ok {
		if p, ok := raw.(*string); ok && p != nil {
			fmt.Println(*p)
			return
		}
	}
	log.Fatalf("answer not produced")
}
