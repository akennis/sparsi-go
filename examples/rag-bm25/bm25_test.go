package main

import (
	"context"
	"sync"
	"testing"
)

func newTestRetriever(t *testing.T) *BM25Retriever {
	t.Helper()
	docs, err := loadKB("testdata/kb")
	if err != nil {
		t.Fatalf("loadKB: %v", err)
	}
	return NewBM25Retriever(docs)
}

func TestBM25_TopHitMatchesObviousQuery(t *testing.T) {
	r := newTestRetriever(t)
	cases := []struct {
		query  string
		wantID string
	}{
		{"how do I return an item", "returns"},
		{"how long does shipping take", "shipping"},
		{"what does the warranty cover", "warranty"},
		{"how do I pair the thermostat with my phone", "setup"},
		{"can I pay with PayPal", "payments"},
		{"my display is blank", "troubleshooting"},
		{"is my heat pump supported", "compatibility"},
	}
	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			docs, err := r.Retrieve(context.Background(), c.query, 3)
			if err != nil {
				t.Fatalf("Retrieve: %v", err)
			}
			if len(docs) == 0 {
				t.Fatalf("Retrieve returned no docs for %q", c.query)
			}
			if docs[0].ID != c.wantID {
				ids := make([]string, len(docs))
				for i, d := range docs {
					ids[i] = d.ID
				}
				t.Fatalf("top hit for %q = %q (full ranking %v), want %q", c.query, docs[0].ID, ids, c.wantID)
			}
			if docs[0].Score <= 0 {
				t.Fatalf("top hit Score = %v, want > 0", docs[0].Score)
			}
		})
	}
}

func TestBM25_ScoresMonotonicallyDecrease(t *testing.T) {
	r := newTestRetriever(t)
	docs, err := r.Retrieve(context.Background(), "shipping warranty returns thermostat", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	for i := 1; i < len(docs); i++ {
		if docs[i].Score > docs[i-1].Score {
			t.Fatalf("results not sorted: docs[%d].Score=%v > docs[%d].Score=%v", i, docs[i].Score, i-1, docs[i-1].Score)
		}
	}
}

func TestBM25_KCapsResults(t *testing.T) {
	r := newTestRetriever(t)
	docs, err := r.Retrieve(context.Background(), "Tessera", 2)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(docs) > 2 {
		t.Fatalf("k=2 returned %d docs", len(docs))
	}
}

func TestBM25_EmptyQueryReturnsEmpty(t *testing.T) {
	r := newTestRetriever(t)
	docs, err := r.Retrieve(context.Background(), "", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("empty query: got %d docs, want 0", len(docs))
	}
}

func TestBM25_NoMatchReturnsEmpty(t *testing.T) {
	r := newTestRetriever(t)
	docs, err := r.Retrieve(context.Background(), "zebra giraffe rhinoceros antarctica", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("no-match query: got %d docs (%v), want 0", len(docs), docs)
	}
}

func TestBM25_ConcurrentRetrieveIsSafe(t *testing.T) {
	r := newTestRetriever(t)
	const N = 30
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, err := r.Retrieve(context.Background(), "Tessera warranty", 3)
			if err != nil {
				t.Errorf("Retrieve: %v", err)
			}
		}()
	}
	wg.Wait()
}
