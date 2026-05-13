package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/akennis/clawdag-go/library"
)

func runRetrievedSources(t *testing.T, docs []library.Document) *RetrievedSourcesOp {
	t.Helper()
	op := &RetrievedSourcesOp{Documents: &docs}
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return op
}

// TestRetrievedSourcesOp_UnionOfRetrieved asserts the op returns the union of
// MetadataSource values across the retrieved Documents slice it was given,
// preserving order of first appearance and de-duplicating repeats.
func TestRetrievedSourcesOp_UnionOfRetrieved(t *testing.T) {
	docs := []library.Document{
		{ID: "shipping", Content: "...", Metadata: map[string]any{library.MetadataSource: "shipping.txt"}},
		{ID: "returns", Content: "...", Metadata: map[string]any{library.MetadataSource: "returns.txt"}},
		// A second chunk from shipping.txt should not produce a duplicate.
		{ID: "shipping#2", Content: "...", Metadata: map[string]any{library.MetadataSource: "shipping.txt"}},
	}
	op := runRetrievedSources(t, docs)
	want := []string{"shipping.txt", "returns.txt"}
	if !reflect.DeepEqual(op.Sources, want) {
		t.Fatalf("Sources = %v, want %v", op.Sources, want)
	}
}

// TestRetrievedSourcesOp_FallsBackToIDDotTxt mirrors sourceFilename's fallback:
// when Metadata[library.MetadataSource] is absent the op uses ID + ".txt".
func TestRetrievedSourcesOp_FallsBackToIDDotTxt(t *testing.T) {
	docs := []library.Document{
		{ID: "ad-hoc", Content: "no metadata"},
	}
	op := runRetrievedSources(t, docs)
	want := []string{"ad-hoc.txt"}
	if !reflect.DeepEqual(op.Sources, want) {
		t.Fatalf("Sources = %v, want %v", op.Sources, want)
	}
}

// TestRetrievedSourcesOp_EmptyAndNil asserts an empty retrieval produces an
// empty source set (so the citation-validation filter rejects everything).
func TestRetrievedSourcesOp_EmptyAndNil(t *testing.T) {
	op := runRetrievedSources(t, nil)
	if len(op.Sources) != 0 {
		t.Fatalf("Sources = %v, want empty for empty retrieval", op.Sources)
	}
}

// TestCitationFilter_HallucinatedRealButUnretrievedFiltered is the regression
// test for the Bug #3 / Security #8 fix. Before the fix the driver derived
// knownSources from the entire loaded corpus, so an LLM that hallucinated a
// real KB filename it had NOT been shown (because it was not in the top-k
// retrieval) would pass the citation-validity check unchallenged. After the
// fix knownSources is derived from RetrievedSourcesOp.Sources — the set of
// sources actually present in the retrieved documents — so a citation naming
// a real-but-unretrieved KB file is filtered out.
func TestCitationFilter_HallucinatedRealButUnretrievedFiltered(t *testing.T) {
	// Loaded corpus contains all three files...
	loaded := []library.Document{
		{ID: "shipping", Content: "...", Metadata: map[string]any{library.MetadataSource: "shipping.txt"}},
		{ID: "returns", Content: "...", Metadata: map[string]any{library.MetadataSource: "returns.txt"}},
		{ID: "warranty", Content: "...", Metadata: map[string]any{library.MetadataSource: "warranty.txt"}},
	}
	_ = loaded // illustrative: the OLD knownSources would have allowed all three.

	// ...but only two were retrieved into the prompt for this question.
	retrieved := []library.Document{
		{ID: "shipping", Content: "...", Metadata: map[string]any{library.MetadataSource: "shipping.txt"}},
		{ID: "returns", Content: "...", Metadata: map[string]any{library.MetadataSource: "returns.txt"}},
	}
	op := runRetrievedSources(t, retrieved)

	knownSources := map[string]bool{}
	for _, s := range op.Sources {
		knownSources[s] = true
	}

	// The LLM hallucinates warranty.txt — a real KB file that was NOT
	// retrieved. It must be filtered.
	llmCited := []string{"returns.txt", "warranty.txt", "made-up.txt"}
	var kept []string
	for _, s := range llmCited {
		if knownSources[s] {
			kept = append(kept, s)
		}
	}
	want := []string{"returns.txt"}
	if !reflect.DeepEqual(kept, want) {
		t.Fatalf("filtered citations = %v, want %v (warranty.txt is loaded-but-unretrieved and MUST be filtered, made-up.txt is pure hallucination)", kept, want)
	}
	if knownSources["warranty.txt"] {
		t.Fatalf("knownSources contains warranty.txt despite it not being retrieved; the all-corpus bug has regressed")
	}
}
