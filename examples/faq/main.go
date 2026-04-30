package main

import (
	"context"
	"fmt"
	"log"

	"github.com/panjf2000/ants/v2"
	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/graph"
	"github.com/wwz16/dagor/operator"
	builtin "github.com/wwz16/dagor/operator/builtin"

	_ "github.com/akennis/clawdag-go/library"
)

type queryKey struct{}
type faqKey struct{}

var faqs = []string{
	"Shipping takes 3-5 business days.",
	"Contact support at support@example.com.",
	"Returns must be shipped within 3 days.",
}

func init() {
	mustReg := func(name string, f func() operator.IOperator) {
		if err := operator.RegisterOpFactory(name, f); err != nil {
			log.Fatalf("register %s: %v", name, err)
		}
	}
	mustReg("query_const", builtin.ContextValFactory[string](queryKey{}))
	mustReg("faq_const", builtin.ContextValFactory[[]string](faqKey{}))
}

var g *graph.Graph

func init() {
	var err error
	g, err = graph.NewBuilder("faq_lookup").
		Vertex("query_const").Op("query_const").
		Output("Result", "user_question").

		Vertex("faq_const").Op("faq_const").
		Output("Result", "faq_entries").

		Vertex("best_faq").Op("AIBestMatchOp").
		Input("Query", "user_question").
		Input("Candidates", "faq_entries").
		Output("Result", "best_index").

		Vertex("get_faq_text").Op("SliceAtOp").
		Input("Input", "faq_entries").
		Input("Index", "best_index").
		Output("Result", "faq_answer").

		Build()
	if err != nil {
		log.Fatalf("build graph: %v", err)
	}
}

func main() {
	pool, err := ants.NewPool(4)
	if err != nil {
		log.Fatalf("ants pool: %v", err)
	}
	defer pool.Release()

	eng, err := dagor.NewEngine(g, pool)
	if err != nil {
		log.Fatalf("new engine: %v", err)
	}

	ctx := context.WithValue(context.Background(), queryKey{}, "What is the return policy?")
	ctx = context.WithValue(ctx, faqKey{}, faqs)

	if err := eng.Run(ctx); err != nil {
		log.Fatalf("run: %v", err)
	}

	if raw, ok := eng.GetOutput("faq_answer"); ok {
		fmt.Printf("FAQ Answer: %v\n", *(raw.(*string)))
	} else {
		fmt.Println("faq_answer wire not found")
	}
}
