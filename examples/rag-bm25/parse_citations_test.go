package main

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func runParse(t *testing.T, raw string) *ParseCitationsOp {
	t.Helper()
	op := &ParseCitationsOp{Raw: &raw}
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return op
}

func TestParseCitations_StandardTrailer(t *testing.T) {
	op := runParse(t, "To return an item, sign in to your account.\n\nSources: returns.txt")
	if op.Body != "To return an item, sign in to your account." {
		t.Fatalf("Body = %q", op.Body)
	}
	if !reflect.DeepEqual(op.Sources, []string{"returns.txt"}) {
		t.Fatalf("Sources = %v, want [returns.txt]", op.Sources)
	}
}

func TestParseCitations_MultipleSources(t *testing.T) {
	op := runParse(t, "Shipping takes 3-5 days. Returns are accepted within 30 days.\nSources: shipping.txt, returns.txt")
	if !reflect.DeepEqual(op.Sources, []string{"shipping.txt", "returns.txt"}) {
		t.Fatalf("Sources = %v, want [shipping.txt returns.txt]", op.Sources)
	}
}

func TestParseCitations_None(t *testing.T) {
	op := runParse(t, "I don't know based on the provided context.\n\nSources: none")
	if op.Body != "I don't know based on the provided context." {
		t.Fatalf("Body = %q", op.Body)
	}
	if op.Sources != nil {
		t.Fatalf("Sources = %v, want nil", op.Sources)
	}
}

func TestParseCitations_NoTrailer(t *testing.T) {
	op := runParse(t, "This response forgot to cite anything.")
	if op.Body != "This response forgot to cite anything." {
		t.Fatalf("Body = %q", op.Body)
	}
	if op.Sources != nil {
		t.Fatalf("Sources = %v, want nil when no trailer present", op.Sources)
	}
}

func TestParseCitations_CaseInsensitiveMarker(t *testing.T) {
	op := runParse(t, "Answer body.\nSOURCES: a.txt, b.txt")
	if !reflect.DeepEqual(op.Sources, []string{"a.txt", "b.txt"}) {
		t.Fatalf("Sources = %v", op.Sources)
	}
}

func TestParseCitations_WhitespaceAroundCommas(t *testing.T) {
	op := runParse(t, "Answer.\nSources:   returns.txt ,   shipping.txt  ,warranty.txt")
	if !reflect.DeepEqual(op.Sources, []string{"returns.txt", "shipping.txt", "warranty.txt"}) {
		t.Fatalf("Sources = %v", op.Sources)
	}
}

func TestParseCitations_LastTrailerWins(t *testing.T) {
	// If the body itself contains the word "Sources:" earlier, the LAST occurrence
	// is the canonical citation line.
	op := runParse(t, "I'll list the Sources: section below.\n\nThe answer is X.\n\nSources: kb1.txt")
	if !reflect.DeepEqual(op.Sources, []string{"kb1.txt"}) {
		t.Fatalf("Sources = %v, want [kb1.txt] (last trailer wins)", op.Sources)
	}
	if op.Body == "" || !strings.Contains(op.Body, "The answer is X.") {
		t.Fatalf("Body = %q, expected to contain answer text", op.Body)
	}
}

func TestParseCitations_EmptyTrailer(t *testing.T) {
	op := runParse(t, "Some answer.\nSources:")
	if op.Body != "Some answer." {
		t.Fatalf("Body = %q", op.Body)
	}
	if op.Sources != nil {
		t.Fatalf("Sources = %v, want nil for empty trailer", op.Sources)
	}
}

// TestParseCitations_NonASCIIBodyPreserved guards against the bug where the
// op derived indices from strings.ToLower(raw) and used them to slice the
// original raw. Some runes change byte length under lowercasing (Turkish
// capital I-with-dot 'İ' lowers to 'i̇' = 'i' + combining dot above, +1 byte;
// German 'ß' lowers to "ss", +1 byte), so indices into the lowercased copy
// no longer align with raw and the slice can cut a UTF-8 sequence in half,
// producing mojibake. Both characters appear here in the body. After the
// fix Body must be valid UTF-8 and contain the originals intact.
func TestParseCitations_NonASCIIBodyPreserved(t *testing.T) {
	body := "İstanbul has straße names that change byte length when lowercased."
	raw := body + "\n\nSources: city.txt"
	op := runParse(t, raw)

	if !utf8.ValidString(op.Body) {
		t.Fatalf("Body is not valid UTF-8: %q", op.Body)
	}
	if op.Body != body {
		t.Fatalf("Body = %q, want %q", op.Body, body)
	}
	if !strings.Contains(op.Body, "İstanbul") {
		t.Fatalf("Body lost the Turkish capital İ: %q", op.Body)
	}
	if !strings.Contains(op.Body, "straße") {
		t.Fatalf("Body lost the German ß: %q", op.Body)
	}
	if !reflect.DeepEqual(op.Sources, []string{"city.txt"}) {
		t.Fatalf("Sources = %v, want [city.txt]", op.Sources)
	}
}

// TestParseCitations_SourcesListCapped verifies that ParseCitationsOp truncates
// excessively long source lists to maxParsedCitations — protecting against
// memory exhaustion from a crafted LLM response that emits a Sources: line
// with millions of comma-separated entries.
func TestParseCitations_SourcesListCapped(t *testing.T) {
	const total = 250
	parts := make([]string, total)
	for i := 0; i < total; i++ {
		parts[i] = fmt.Sprintf("s%d", i)
	}
	raw := "Body: an answer derived from a flood of citations.\n\nSources: " + strings.Join(parts, ", ")
	op := runParse(t, raw)

	if len(op.Sources) != maxParsedCitations {
		t.Fatalf("len(Sources) = %d, want %d", len(op.Sources), maxParsedCitations)
	}
	if op.Sources[0] != "s0" {
		t.Fatalf("Sources[0] = %q, want s0 (prefix order preserved)", op.Sources[0])
	}
	if op.Sources[99] != "s99" {
		t.Fatalf("Sources[99] = %q, want s99", op.Sources[99])
	}
}

// TestParseCitations_BelowCapUnchanged is a sanity check that a small Sources
// list passes through untouched (i.e. the cap only triggers above the
// threshold).
func TestParseCitations_BelowCapUnchanged(t *testing.T) {
	op := runParse(t, "Answer.\nSources: a.txt, b.txt, c.txt, d.txt, e.txt")
	want := []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"}
	if !reflect.DeepEqual(op.Sources, want) {
		t.Fatalf("Sources = %v, want %v", op.Sources, want)
	}
}
