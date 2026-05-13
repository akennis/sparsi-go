// BM25 retriever — an in-memory implementation of library.Retriever using the
// classic Robertson/Sparck-Jones BM25 ranking function. Index built at
// construction; reads are lock-free and safe for concurrent Retrieve calls.
//
// Suitable up to ~10k documents. For larger corpora plug in an external
// search backend by implementing library.Retriever yourself.
package main

import (
	"context"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/akennis/clawdag-go/library"
)

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// BM25Retriever indexes a fixed corpus at construction. The corpus is
// immutable after NewBM25Retriever returns; rebuild a new instance to change
// it. Retrieve is read-only on all fields, so concurrent calls are safe.
type BM25Retriever struct {
	docs     []library.Document
	termFreq []map[string]int // termFreq[i][term] = count of term in docs[i]
	docLen   []int
	df       map[string]int // df[term] = number of docs containing term
	avgdl    float64
}

// NewBM25Retriever tokenizes every document and precomputes the index.
// Documents with empty content are kept (and will simply never match).
func NewBM25Retriever(docs []library.Document) *BM25Retriever {
	r := &BM25Retriever{
		docs:     docs,
		termFreq: make([]map[string]int, len(docs)),
		docLen:   make([]int, len(docs)),
		df:       map[string]int{},
	}
	var totalLen int
	for i, d := range docs {
		toks := tokenize(d.Content)
		tf := map[string]int{}
		for _, t := range toks {
			tf[t]++
		}
		r.termFreq[i] = tf
		r.docLen[i] = len(toks)
		totalLen += len(toks)
		for t := range tf {
			r.df[t]++
		}
	}
	if len(docs) > 0 {
		r.avgdl = float64(totalLen) / float64(len(docs))
	}
	return r
}

// Retrieve scores every doc against the query and returns the top k by score,
// best-first. Each returned Document has its Score field populated.
func (r *BM25Retriever) Retrieve(_ context.Context, query string, k int) ([]library.Document, error) {
	qTerms := tokenize(query)
	if len(qTerms) == 0 || len(r.docs) == 0 {
		return nil, nil
	}
	n := float64(len(r.docs))

	scored := make([]library.Document, 0, len(r.docs))
	for i, d := range r.docs {
		var score float64
		dl := float64(r.docLen[i])
		for _, qt := range qTerms {
			f := r.termFreq[i][qt]
			if f == 0 {
				continue
			}
			df := r.df[qt]
			idf := math.Log(1 + (n-float64(df)+0.5)/(float64(df)+0.5))
			tf := float64(f)
			denom := tf + bm25K1*(1-bm25B+bm25B*dl/r.avgdl)
			score += idf * tf * (bm25K1 + 1) / denom
		}
		if score > 0 {
			cp := d
			cp.Score = score
			scored = append(scored, cp)
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if k < len(scored) {
		scored = scored[:k]
	}
	return scored, nil
}

// tokenize lowercases the input and splits on any rune that isn't a letter or
// digit. No stemming, no stopword removal — keep the example minimal and let
// the BM25 weighting handle common words via their low IDF.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
