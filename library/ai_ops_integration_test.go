package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wwz16/dagor/config"
)

func skipIfNoAPIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("CLAUDE_API_KEY") == "" {
		t.Skip("CLAUDE_API_KEY not set")
	}
}

func mustParams(t *testing.T, kv map[string]string) *config.Params {
	t.Helper()
	raw, err := json.Marshal(kv)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	p, err := config.NewFromRaw(raw)
	if err != nil {
		t.Fatalf("new params: %v", err)
	}
	return p
}

func strPtr(s string) *string { return &s }

func containsAll(t *testing.T, got []string, want []string) {
	t.Helper()
	index := make(map[string]bool, len(got))
	for _, g := range got {
		index[g] = true
	}
	for _, w := range want {
		if !index[w] {
			t.Errorf("result %v does not contain %q", got, w)
		}
	}
}

// ---- AIExtractStringSliceOp ----

func TestAIExtractStringSliceOp_Ingredients(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIExtractStringSliceOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "extract all ingredient names from this recipe",
	})); err != nil {
		t.Fatal(err)
	}
	input := "To make a banana smoothie, blend together 1 banana, a handful of strawberries, 200ml of milk, and a spoonful of honey."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	containsAll(t, op.Result, []string{"banana", "strawberries", "milk", "honey"})
}

func TestAIExtractStringSliceOp_Cities(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIExtractStringSliceOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "extract all city names from this travel text",
	})); err != nil {
		t.Fatal(err)
	}
	input := "Start your European trip in Paris, then take a train to Amsterdam, spend a day in Brussels, and fly home from Frankfurt."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	containsAll(t, op.Result, []string{"Paris", "Amsterdam", "Brussels", "Frankfurt"})
}

func TestAIExtractStringSliceOp_ProgrammingLanguages(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIExtractStringSliceOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "extract all programming language names mentioned in this text",
	})); err != nil {
		t.Fatal(err)
	}
	input := "Our backend is written in Go, our data pipeline uses Python, our frontend is TypeScript, and some legacy services run on Java."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	containsAll(t, op.Result, []string{"Go", "Python", "TypeScript", "Java"})
}

// ---- AIExtractMapOp ----

func TestAIExtractMapOp_ContactInfo(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIExtractMapOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "extract name, email, and city from this contact info",
	})); err != nil {
		t.Fatal(err)
	}
	input := "Hi, I'm John Smith. You can reach me at john@example.com. I live in Seattle."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := op.Result["name"]; got != "John Smith" {
		t.Errorf("name: got %q, want %q", got, "John Smith")
	}
	if got := op.Result["email"]; got != "john@example.com" {
		t.Errorf("email: got %q, want %q", got, "john@example.com")
	}
	if got := op.Result["city"]; got != "Seattle" {
		t.Errorf("city: got %q, want %q", got, "Seattle")
	}
}

func TestAIExtractMapOp_ProductOrder(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIExtractMapOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "extract product, quantity, and price from this order",
	})); err != nil {
		t.Fatal(err)
	}
	input := "Order: 3 units of Widget Pro at $9.99 each."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := op.Result["product"]; got != "Widget Pro" {
		t.Errorf("product: got %q, want %q", got, "Widget Pro")
	}
	if got := op.Result["quantity"]; got != "3" {
		t.Errorf("quantity: got %q, want %q", got, "3")
	}
}

// ---- AIParseNumberOp ----

func TestAIParseNumberOp_WordNumber(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIParseNumberOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("twenty-three")
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 23 {
		t.Errorf("got %v, want 23", op.Result)
	}
}

func TestAIParseNumberOp_ThousandWord(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIParseNumberOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("one thousand")
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 1000 {
		t.Errorf("got %v, want 1000", op.Result)
	}
}

func TestAIParseNumberOp_EmbeddedDigit(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIParseNumberOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("the answer is 42")
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 42 {
		t.Errorf("got %v, want 42", op.Result)
	}
}

func TestAIParseNumberOp_Decimal(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIParseNumberOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("3.14")
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 3.14 {
		t.Errorf("got %v, want 3.14", op.Result)
	}
}

// ---- AISummarizeOp ----

func TestAISummarizeOp_BulletPoints(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AISummarizeOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "summarize into one concise sentence",
	})); err != nil {
		t.Fatal(err)
	}
	items := []string{
		"The sky is blue during the day.",
		"Clouds form when water vapor condenses.",
		"Rain falls from clouds when droplets become heavy enough.",
	}
	op.Input = &items
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result == "" {
		t.Error("expected non-empty summary")
	}
}

func TestAISummarizeOp_ShoppingList(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AISummarizeOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "summarize what kind of shopping trip this is",
	})); err != nil {
		t.Fatal(err)
	}
	items := []string{"apples", "bananas", "oranges", "grapes", "blueberries"}
	op.Input = &items
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result == "" {
		t.Error("expected non-empty summary")
	}
}

// ---- AIClassifyMultiLabelOp ----

func TestAIClassifyMultiLabelOp_BillingInquiry(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIClassifyMultiLabelOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"categories": "billing,bug,feature,spam",
	})); err != nil {
		t.Fatal(err)
	}
	input := "I was charged twice for my subscription this month. Please refund the duplicate charge."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, label := range op.Result {
		if label == "billing" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'billing' in result %v", op.Result)
	}
}

func TestAIClassifyMultiLabelOp_BugReport(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIClassifyMultiLabelOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"categories": "billing,bug,feature,spam",
	})); err != nil {
		t.Fatal(err)
	}
	input := "The app crashes every time I try to upload a photo. Steps to reproduce: open app, tap upload, select image, app freezes."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, label := range op.Result {
		if label == "bug" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'bug' in result %v", op.Result)
	}
}

func TestAIClassifyMultiLabelOp_Spam(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIClassifyMultiLabelOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"categories": "billing,bug,feature,spam",
	})); err != nil {
		t.Fatal(err)
	}
	input := "CONGRATULATIONS! You have been selected to win a $1000 gift card! Click here now to claim your prize!!!"
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, label := range op.Result {
		if label == "spam" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'spam' in result %v", op.Result)
	}
}

func TestAIClassifyMultiLabelOp_NoMatch(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIClassifyMultiLabelOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"categories": "billing,bug,feature,spam",
	})); err != nil {
		t.Fatal(err)
	}
	input := "Thank you for the great service!"
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Result should be empty or not contain invalid categories.
	catSet := map[string]bool{"billing": true, "bug": true, "feature": true, "spam": true}
	for _, label := range op.Result {
		if !catSet[label] {
			t.Errorf("unexpected label %q in result %v", label, op.Result)
		}
	}
}

// ---- AIScoreOp ----

func TestAIScoreOp_HighToxicity(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIScoreOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"criterion": "toxicity",
	})); err != nil {
		t.Fatal(err)
	}
	input := "You are a complete idiot and I hate everything about you. Go away and never come back!"
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result < 0.6 {
		t.Errorf("expected high toxicity score (>= 0.6), got %v", op.Result)
	}
}

func TestAIScoreOp_LowToxicity(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIScoreOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"criterion": "toxicity",
	})); err != nil {
		t.Fatal(err)
	}
	input := "Thank you so much for your help today! I really appreciate your patience and kindness."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result > 0.2 {
		t.Errorf("expected low toxicity score (<= 0.2), got %v", op.Result)
	}
}

func TestAIScoreOp_HighRelevance(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIScoreOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"criterion": "relevance to the topic of machine learning",
	})); err != nil {
		t.Fatal(err)
	}
	input := "Neural networks and gradient descent are fundamental concepts in machine learning and deep learning."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result < 0.7 {
		t.Errorf("expected high relevance score (>= 0.7), got %v", op.Result)
	}
}

func TestAIScoreOp_AlwaysInRange(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIScoreOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"criterion": "formality",
	})); err != nil {
		t.Fatal(err)
	}
	input := "hey whats up lol yeah sure whatever"
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result < 0 || op.Result > 1 {
		t.Errorf("score %v out of [0,1]", op.Result)
	}
}

// ---- AIBoolOp ----

func TestAIBoolOp_TrueCase(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBoolOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"predicate": "is this text about France or a French landmark?",
	})); err != nil {
		t.Fatal(err)
	}
	input := "The Eiffel Tower is one of the most visited monuments in France, located in the heart of Paris."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !op.Result {
		t.Errorf("expected true, got false")
	}
}

func TestAIBoolOp_FalseCaseEmailAbsent(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBoolOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"predicate": "does this text contain an email address?",
	})); err != nil {
		t.Fatal(err)
	}
	input := "I love my new laptop. It has a great screen and fast processor."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result {
		t.Errorf("expected false, got true")
	}
}

func TestAIBoolOp_TrueCaseEmailPresent(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBoolOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"predicate": "does this text contain an email address?",
	})); err != nil {
		t.Fatal(err)
	}
	input := "Please reach me at contact@example.com for any questions."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !op.Result {
		t.Errorf("expected true, got false")
	}
}

func TestAIBoolOp_FalseCaseUnrelated(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBoolOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"predicate": "does this text mention any animals?",
	})); err != nil {
		t.Fatal(err)
	}
	input := "The stock market rose by 2% today amid positive economic data."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result {
		t.Errorf("expected false, got true")
	}
}

// ---- AIBestMatchOp ----

func TestAIBestMatchOp_WebLanguage(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBestMatchOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	query := "the primary programming language for web browser scripting"
	candidates := []string{"Python", "JavaScript", "Rust", "COBOL"}
	op.Query = &query
	op.Candidates = &candidates
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 1 {
		t.Errorf("expected index 1 (JavaScript), got %d (%s)", op.Result, candidates[op.Result])
	}
}

func TestAIBestMatchOp_FastestAnimal(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBestMatchOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	query := "fastest land animal"
	candidates := []string{"elephant", "cheetah", "turtle", "sloth"}
	op.Query = &query
	op.Candidates = &candidates
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 1 {
		t.Errorf("expected index 1 (cheetah), got %d (%s)", op.Result, candidates[op.Result])
	}
}

func TestAIBestMatchOp_LargestPlanet(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBestMatchOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	query := "largest planet in our solar system"
	candidates := []string{"Mars", "Venus", "Jupiter", "Saturn"}
	op.Query = &query
	op.Candidates = &candidates
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 2 {
		t.Errorf("expected index 2 (Jupiter), got %d (%s)", op.Result, candidates[op.Result])
	}
}

// ---- AIRerankOp ----

func TestAIRerankOp_MLFrameworks(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIRerankOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	query := "machine learning frameworks"
	candidates := []string{"TensorFlow", "Flask", "NumPy", "PyTorch"}
	op.Query = &query
	op.Candidates = &candidates
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(op.Result) != 4 {
		t.Fatalf("expected 4 indices, got %d", len(op.Result))
	}
	// TensorFlow (0) or PyTorch (3) should be ranked first.
	first := op.Result[0]
	if first != 0 && first != 3 {
		t.Errorf("expected TensorFlow(0) or PyTorch(3) ranked first, got index %d (%s)", first, candidates[first])
	}
}

func TestAIRerankOp_LargestOcean(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIRerankOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	query := "largest ocean by area"
	candidates := []string{"Atlantic Ocean", "Indian Ocean", "Arctic Ocean", "Pacific Ocean"}
	op.Query = &query
	op.Candidates = &candidates
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(op.Result) != 4 {
		t.Fatalf("expected 4 indices, got %d", len(op.Result))
	}
	// Pacific Ocean is index 3 and should be ranked first.
	if op.Result[0] != 3 {
		t.Errorf("expected Pacific Ocean (3) ranked first, got index %d (%s)", op.Result[0], candidates[op.Result[0]])
	}
}

func TestAIRerankOp_IsPermutation(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIRerankOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	query := "capital cities of Europe"
	candidates := []string{"Berlin", "Tokyo", "Paris", "Sydney", "Rome"}
	op.Query = &query
	op.Candidates = &candidates
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(op.Result) != 5 {
		t.Fatalf("expected 5 indices, got %d", len(op.Result))
	}
	seen := make(map[int]bool)
	for _, idx := range op.Result {
		if idx < 0 || idx >= 5 {
			t.Errorf("index %d out of range [0,5)", idx)
		}
		if seen[idx] {
			t.Errorf("duplicate index %d in result %v", idx, op.Result)
		}
		seen[idx] = true
	}
}

func TestAIBestMatchOp_EmptyCandidatesError(t *testing.T) {
	op := &AIBestMatchOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Query = strPtr("anything")
	empty := []string{}
	op.Candidates = &empty
	err := op.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for empty candidates, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error message to contain %q, got %q", "empty", err.Error())
	}
}

func TestAIRerankOp_EmptyCandidatesError(t *testing.T) {
	op := &AIRerankOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Query = strPtr("anything")
	empty := []string{}
	op.Candidates = &empty
	err := op.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for empty candidates, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error message to contain %q, got %q", "empty", err.Error())
	}
}

func TestAIBestMatchOp_SingleCandidate(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBestMatchOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Query = strPtr("anything")
	candidates := []string{"foo"}
	op.Candidates = &candidates
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 0 {
		t.Errorf("expected index 0, got %d", op.Result)
	}
}

func TestAIRerankOp_SingleCandidate(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIRerankOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Query = strPtr("anything")
	candidates := []string{"foo"}
	op.Candidates = &candidates
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(op.Result) != 1 {
		t.Fatalf("expected 1 index, got %d", len(op.Result))
	}
	if op.Result[0] != 0 {
		t.Errorf("expected index 0, got %d", op.Result[0])
	}
}

// ---- Empty input no-panic tests ----

func TestAIBoolOp_EmptyInput(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBoolOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"predicate": "is this text about France?",
	})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("")
	err := op.Run(context.Background())
	if err != nil {
		if err.Error() == "" {
			t.Errorf("expected non-empty error message on failure")
		}
		t.Logf("AIBoolOp empty input failed cleanly: %v", err)
	} else {
		t.Logf("AIBoolOp empty input succeeded with result: %v", op.Result)
	}
}

func TestAIScoreOp_EmptyInput(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIScoreOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"criterion": "toxicity",
	})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("")
	err := op.Run(context.Background())
	if err != nil {
		if err.Error() == "" {
			t.Errorf("expected non-empty error message on failure")
		}
		t.Logf("AIScoreOp empty input failed cleanly: %v", err)
	} else {
		t.Logf("AIScoreOp empty input succeeded with result: %v", op.Result)
	}
}

func TestAIClassifyMultiLabelOp_EmptyInput(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIClassifyMultiLabelOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"categories": "billing,bug,feature,spam",
	})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("")
	err := op.Run(context.Background())
	if err != nil {
		if err.Error() == "" {
			t.Errorf("expected non-empty error message on failure")
		}
		t.Logf("AIClassifyMultiLabelOp empty input failed cleanly: %v", err)
	} else {
		t.Logf("AIClassifyMultiLabelOp empty input succeeded with result: %v", op.Result)
	}
}

func TestAIParseNumberOp_EmptyInput(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIParseNumberOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("")
	err := op.Run(context.Background())
	if err != nil {
		if err.Error() == "" {
			t.Errorf("expected non-empty error message on failure")
		}
		t.Logf("AIParseNumberOp empty input failed cleanly: %v", err)
	} else {
		t.Logf("AIParseNumberOp empty input succeeded with result: %v", op.Result)
	}
}

func TestAIExtractStringSliceOp_UnicodeMultilingualNames(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIExtractStringSliceOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "extract every person name from this text",
	})); err != nil {
		t.Fatal(err)
	}
	input := "Guests at the dinner: 王伟 🍣, José García 🌮, Müller 🍺, محمد علي 🥙, Søren Kierkegaard ☕, さくら 🍵."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Logf("op.Result: %v", op.Result)
	containsAll(t, op.Result, []string{"José García", "Müller", "Søren Kierkegaard"})
}

func TestAIParseNumberOp_NegativeWord(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIParseNumberOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("minus seventeen")
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != -17 {
		t.Errorf("got %v, want -17", op.Result)
	}
}

func TestAIParseNumberOp_ScientificShorthand(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIParseNumberOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("1.2k")
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 1200 {
		t.Errorf("got %v, want 1200", op.Result)
	}
}

func TestAIParseNumberOp_Currency(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIParseNumberOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("$1,234.56")
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 1234.56 {
		t.Errorf("got %v, want 1234.56", op.Result)
	}
}

func TestAIParseNumberOp_BareIntegerInNoise(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIParseNumberOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("After hours of debate, careful consideration, and several rounds of voting among the committee members, the chairperson finally announced that the official count was 73, and the meeting concluded.")
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 73 {
		t.Errorf("got %v, want 73", op.Result)
	}
}

func TestAIExtractMapOp_MissingKey(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIExtractMapOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "extract product, quantity, and price from this text",
	})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("Order: 3 units of Widget Pro.")
	if err := op.Run(context.Background()); err != nil {
		if err.Error() == "" {
			t.Fatal("expected non-empty error message on failure")
		}
		t.Fatalf("AIExtractMapOp missing-key Run failed: %v", err)
	}
	t.Logf("AIExtractMapOp missing-key full result: %#v", op.Result)
	if val, ok := op.Result["price"]; ok {
		t.Logf("missing-key behavior: key present, value=%q", val)
		if val != "" {
			t.Logf("note: model produced non-empty value for absent field (possible fabrication): %q", val)
		}
	} else {
		t.Logf("missing-key behavior: key absent from result map")
	}
	for _, k := range []string{"product", "quantity"} {
		if _, ok := op.Result[k]; !ok {
			t.Logf("note: present-in-input key %q is missing from result", k)
		}
	}
}

func TestAIClassifyMultiLabelOp_MultipleLabels(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIClassifyMultiLabelOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"categories": "billing,bug,feature,spam",
	})); err != nil {
		t.Fatal(err)
	}
	input := "I was charged twice this month, and on top of that the app crashes whenever I try to view my invoices."
	op.Input = &input
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Logf("op.Result: %v", op.Result)
	if len(op.Result) < 2 {
		t.Errorf("expected at least 2 labels, got %d: %v", len(op.Result), op.Result)
	}
	containsAll(t, op.Result, []string{"billing", "bug"})
}

// ---- Large prompts ----

func TestAISummarizeOp_LargeBulletList(t *testing.T) {
	skipIfNoAPIKey(t)
	if testing.Short() {
		t.Skip("skipping large-prompt test in short mode")
	}
	op := &AISummarizeOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "summarize this list of items into one concise paragraph",
	})); err != nil {
		t.Fatal(err)
	}
	items := make([]string, 0, 200)
	for i := 1; i <= 200; i++ {
		items = append(items, fmt.Sprintf("Item %d: some short detail.", i))
	}
	op.Input = &items
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result == "" {
		t.Error("expected non-empty summary")
	}
	t.Logf("summary length: %d", len(op.Result))
}

func TestAIExtractStringSliceOp_LongCSVOutput(t *testing.T) {
	skipIfNoAPIKey(t)
	if testing.Short() {
		t.Skip("skipping large-prompt test in short mode")
	}
	op := &AIExtractStringSliceOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"operation": "extract every distinct city name mentioned in this text",
	})); err != nil {
		t.Fatal(err)
	}
	cities := []string{
		"Tokyo", "Paris", "London", "New York", "Sydney",
		"Cairo", "Mumbai", "Berlin", "Rome", "Madrid",
		"Moscow", "Beijing", "Seoul", "Bangkok", "Istanbul",
		"Dubai", "Toronto", "Vancouver", "Mexico City", "Buenos Aires",
		"Rio de Janeiro", "Lagos", "Nairobi", "Cape Town", "Athens",
		"Vienna", "Prague", "Stockholm", "Helsinki", "Lisbon",
	}
	filler := "The journey was filled with countless memorable moments, each more vivid than the last, as the traveler wandered through bustling streets, quiet alleys, vibrant marketplaces, and serene parks under skies that shifted from gold to violet to deep indigo. "
	var sb strings.Builder
	sb.WriteString("This is a long travelogue describing a year of nonstop adventure across the globe. ")
	for i, city := range cities {
		sb.WriteString(fmt.Sprintf("On stop %d, she visited %s, where she sampled local cuisine, met fascinating people, and recorded a dozen pages of notes about the architecture and customs. ", i+1, city))
		sb.WriteString(filler)
	}
	for sb.Len() < 5000 {
		sb.WriteString(filler)
	}
	builtInput := sb.String()
	op.Input = &builtInput
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Logf("input length: %d, extracted count: %d", len(builtInput), len(op.Result))
	if len(op.Result) < 25 {
		t.Errorf("expected at least 25 cities extracted, got %d: %v", len(op.Result), op.Result)
	}
}

func TestAIRerankOp_TwentyCandidates(t *testing.T) {
	skipIfNoAPIKey(t)
	if testing.Short() {
		t.Skip("skipping large-prompt test in short mode")
	}
	op := &AIRerankOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	query := "famous works of Shakespeare"
	candidates := []string{
		"Hamlet", "Macbeth", "Othello", "King Lear", "Romeo and Juliet",
		"The Tempest", "A Midsummer Night's Dream", "Julius Caesar", "Twelfth Night", "Much Ado About Nothing",
		"The Great Gatsby", "Moby Dick", "Pride and Prejudice", "1984", "Crime and Punishment",
		"Don Quixote", "War and Peace", "Ulysses", "Brave New World", "The Catcher in the Rye",
	}
	op.Query = &query
	op.Candidates = &candidates
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(op.Result) != 20 {
		t.Fatalf("expected 20 indices, got %d", len(op.Result))
	}
	seen := make(map[int]bool)
	for _, idx := range op.Result {
		if idx < 0 || idx >= 20 {
			t.Errorf("index %d out of range [0,20)", idx)
		}
		if seen[idx] {
			t.Errorf("duplicate index %d in result %v", idx, op.Result)
		}
		seen[idx] = true
	}
	t.Logf("top 5: %v", op.Result[:5])
}

func TestAIBestMatchOp_TwentyCandidates(t *testing.T) {
	skipIfNoAPIKey(t)
	if testing.Short() {
		t.Skip("skipping large-prompt test in short mode")
	}
	op := &AIBestMatchOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	query := "author of A Brief History of Time"
	candidates := []string{
		"Albert Einstein", "Isaac Newton", "Charles Darwin", "Marie Curie", "Niels Bohr",
		"Werner Heisenberg", "Erwin Schrödinger", "Richard Feynman", "Carl Sagan", "Neil deGrasse Tyson",
		"Brian Greene", "Roger Penrose", "Lawrence Krauss", "Michio Kaku", "Edwin Hubble",
		"Galileo Galilei", "Johannes Kepler", "Stephen Hawking", "Edward Witten", "Paul Dirac",
	}
	op.Query = &query
	op.Candidates = &candidates
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if op.Result != 17 {
		t.Errorf("expected index 17 (Stephen Hawking), got %d (%s)", op.Result, candidates[op.Result])
	}
	if op.Result < 0 || op.Result >= 20 {
		t.Errorf("index %d out of range [0,20)", op.Result)
	}
	t.Logf("op.Result: %d, candidates[op.Result]: %s", op.Result, candidates[op.Result])
}

func TestAIBoolOp_ZeroRetriesImpossibleTaskBottomsOut(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBoolOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"predicate":   "respond with the word banana",
		"max_retries": "0",
	})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("anything")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	err := op.Run(ctx)
	elapsed := time.Since(start)

	if elapsed >= 30*time.Second {
		t.Fatalf("Run did not bottom out within 30s budget; elapsed=%v", elapsed)
	}
	t.Logf("AIBoolOp zero-retries Run elapsed=%v err=%v", elapsed, err)

	if err == nil {
		t.Logf("note: model produced valid true/false output; retry path not exercised this run")
		return
	}
	msg := err.Error()
	if !strings.Contains(msg, "AIBoolOp") {
		t.Errorf("expected error to contain %q, got %q", "AIBoolOp", msg)
	}
	if !strings.Contains(msg, "all 1 attempts failed") && !strings.Contains(msg, "failed") {
		t.Errorf("expected error to contain %q or %q, got %q", "all 1 attempts failed", "failed", msg)
	}
	if !strings.Contains(msg, "last error") {
		t.Errorf("expected error to contain %q, got %q", "last error", msg)
	}
}

func TestAIBoolOp_CancelledContext(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBoolOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"predicate": "is this a sentence?",
	})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("Hello world.")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := op.Run(ctx)
	elapsed := time.Since(start)

	t.Logf("AIBoolOp cancelled-context Run elapsed=%v err=%v", elapsed, err)

	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if elapsed >= 5*time.Second {
		t.Errorf("expected prompt return (<5s), got elapsed=%v", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		msg := err.Error()
		if !strings.Contains(msg, "context canceled") && !strings.Contains(msg, "canceled") {
			t.Errorf("expected error to be context.Canceled or contain %q/%q, got %q", "context canceled", "canceled", msg)
		}
	}
}

func TestModeSelectOp_ArithmeticVsCityDispatch(t *testing.T) {
	skipIfNoAPIKey(t)

	t.Run("arithmetic", func(t *testing.T) {
		op := &ModeSelectOp{}
		if err := op.Setup(mustParams(t, map[string]string{
			"categories": "arithmetic expression,city name",
		})); err != nil {
			t.Fatal(err)
		}
		op.Input = strPtr("3 + 4 * 2")
		if err := op.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if op.Result != "arithmetic expression" {
			t.Errorf("got %q, want %q", op.Result, "arithmetic expression")
		}
	})

	t.Run("city", func(t *testing.T) {
		op := &ModeSelectOp{}
		if err := op.Setup(mustParams(t, map[string]string{
			"categories": "arithmetic expression,city name",
		})); err != nil {
			t.Fatal(err)
		}
		op.Input = strPtr("Paris")
		if err := op.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if op.Result != "city name" {
			t.Errorf("got %q, want %q", op.Result, "city name")
		}
	})
}

func TestModeSelectOp_AmbiguousInput(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &ModeSelectOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"categories": "arithmetic expression,city name",
	})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("Bath")
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	switch op.Result {
	case "arithmetic expression", "city name":
		// ok — model selected one of the configured categories
	default:
		t.Errorf("op.Result %q not in configured categories {arithmetic expression, city name}", op.Result)
	}
	t.Logf("op.Result: %q", op.Result)
}

// ---- Reasoning mode (WithReasoningLog) ----

// assertReasoningEntry checks that a ReasoningEntry has the expected op name,
// non-empty reasoning, non-nil output, and that every key in wantInputs is
// present with the correct value.
func assertReasoningEntry(t *testing.T, e ReasoningEntry, wantOp string, wantInputs map[string]any) {
	t.Helper()
	if e.Op != wantOp {
		t.Errorf("Op: got %q, want %q", e.Op, wantOp)
	}
	if e.Reasoning == "" {
		t.Error("Reasoning is empty")
	}
	if e.Output == nil {
		t.Error("Output is nil")
	}
	for k, want := range wantInputs {
		got, ok := e.Inputs[k]
		if !ok {
			t.Errorf("Inputs[%q] missing", k)
			continue
		}
		if got != want {
			t.Errorf("Inputs[%q]: got %v, want %v", k, got, want)
		}
	}
}

func TestAIScoreOp_ReasoningMode(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIScoreOp{}
	if err := op.Setup(mustParams(t, map[string]string{"criterion": "toxicity"})); err != nil {
		t.Fatal(err)
	}
	input := "You are a complete idiot and I hate everything about you."
	op.Input = &input

	ctx, log := WithReasoningLog(context.Background())
	if err := op.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 reasoning entry, got %d", len(entries))
	}
	assertReasoningEntry(t, entries[0], "AIScoreOp", map[string]any{
		"Input":     input,
		"Criterion": "toxicity",
	})
}

func TestAIBoolOp_ReasoningMode(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBoolOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"predicate": "does this text mention a programming language?",
	})); err != nil {
		t.Fatal(err)
	}
	input := "Go is a compiled language designed at Google."
	op.Input = &input

	ctx, log := WithReasoningLog(context.Background())
	if err := op.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 reasoning entry, got %d", len(entries))
	}
	assertReasoningEntry(t, entries[0], "AIBoolOp", map[string]any{
		"Input":     input,
		"Predicate": "does this text mention a programming language?",
	})
}

func TestAIClassifyMultiLabelOp_ReasoningMode(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIClassifyMultiLabelOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"categories": "billing,bug,feature,spam",
	})); err != nil {
		t.Fatal(err)
	}
	input := "The app crashes every time I try to upload a photo."
	op.Input = &input

	ctx, log := WithReasoningLog(context.Background())
	if err := op.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 reasoning entry, got %d", len(entries))
	}
	assertReasoningEntry(t, entries[0], "AIClassifyMultiLabelOp", map[string]any{
		"Input": input,
	})
}

func TestAIBestMatchOp_ReasoningMode(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIBestMatchOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	query := "fastest land animal"
	candidates := []string{"elephant", "cheetah", "turtle", "sloth"}
	op.Query = &query
	op.Candidates = &candidates

	ctx, log := WithReasoningLog(context.Background())
	if err := op.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 reasoning entry, got %d", len(entries))
	}
	assertReasoningEntry(t, entries[0], "AIBestMatchOp", map[string]any{
		"Query": query,
	})
}

func TestAIRerankOp_ReasoningMode(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIRerankOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	query := "machine learning frameworks"
	candidates := []string{"TensorFlow", "Flask", "NumPy", "PyTorch"}
	op.Query = &query
	op.Candidates = &candidates

	ctx, log := WithReasoningLog(context.Background())
	if err := op.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 reasoning entry, got %d", len(entries))
	}
	assertReasoningEntry(t, entries[0], "AIRerankOp", map[string]any{
		"Query": query,
	})
}

func TestAIComputeOp_ReasoningMode(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIParseNumberOp{}
	if err := op.Setup(mustParams(t, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	input := "twenty-three"
	op.Input = &input

	ctx, log := WithReasoningLog(context.Background())
	if err := op.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 reasoning entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Op != "AIComputeOp" {
		t.Errorf("Op: got %q, want %q", e.Op, "AIComputeOp")
	}
	if e.Reasoning == "" {
		t.Error("Reasoning is empty")
	}
	if e.Inputs["Operation"] == nil {
		t.Error("Inputs[Operation] is missing")
	}
	if op.Result != 23 {
		t.Errorf("result: got %v, want 23", op.Result)
	}
}

// TestReasoningLog_MultipleInvocations_SameOpType runs the same op instance
// twice with different inputs and asserts that both invocations are recorded as
// separate entries with distinct Inputs snapshots.
func TestReasoningLog_MultipleInvocations_SameOpType(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIScoreOp{}
	if err := op.Setup(mustParams(t, map[string]string{"criterion": "toxicity"})); err != nil {
		t.Fatal(err)
	}

	ctx, log := WithReasoningLog(context.Background())

	inputs := []string{
		"You are a complete idiot and I hate everything about you!",
		"Thank you so much for your kind help today.",
	}
	for _, in := range inputs {
		s := in
		op.Input = &s
		if err := op.Run(ctx); err != nil {
			t.Fatalf("Run(%q): %v", in, err)
		}
	}

	entries := log.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	in0, _ := entries[0].Inputs["Input"].(string)
	in1, _ := entries[1].Inputs["Input"].(string)
	if in0 == in1 {
		t.Errorf("both entries recorded the same Input %q — invocations not tracked independently", in0)
	}
}

// TestReasoningLog_NoEntryWhenDisabled confirms that running an op with a plain
// context (no WithReasoningLog) records nothing and does not panic.
func TestReasoningLog_NoEntryWhenDisabled(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &AIScoreOp{}
	if err := op.Setup(mustParams(t, map[string]string{"criterion": "toxicity"})); err != nil {
		t.Fatal(err)
	}
	input := "some text"
	op.Input = &input

	// Run without a reasoning log in context.
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// No assertion about a log — just confirming no panic and correct result.
	if op.Result < 0 || op.Result > 1 {
		t.Errorf("score %v out of [0,1]", op.Result)
	}
}

func TestModeSelectOp_EmptyInput(t *testing.T) {
	skipIfNoAPIKey(t)
	op := &ModeSelectOp{}
	if err := op.Setup(mustParams(t, map[string]string{
		"categories": "arithmetic expression,city name",
	})); err != nil {
		t.Fatal(err)
	}
	op.Input = strPtr("")
	err := op.Run(context.Background())
	t.Logf("ModeSelectOp empty input: op.Result=%q err=%v", op.Result, err)
	if err != nil {
		if err.Error() == "" {
			t.Errorf("expected non-empty error message on failure")
		}
		return
	}
	switch op.Result {
	case "arithmetic expression", "city name":
		// ok — model produced a sensible classification
	default:
		t.Errorf("op.Result %q not in configured categories {arithmetic expression, city name}", op.Result)
	}
}
