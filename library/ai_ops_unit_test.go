package library

import (
	"reflect"
	"strings"
	"testing"
)

// ---- Setup() validation tests (no API key required) ----

func TestModeSelectOp_Setup_MissingCategories(t *testing.T) {
	op := &ModeSelectOp{}
	err := op.Setup(mustParams(t, map[string]string{}))
	if err == nil {
		t.Fatal("expected error when 'categories' param is missing, got nil")
	}
	if !strings.Contains(err.Error(), "categories") {
		t.Errorf("expected error to mention 'categories', got %q", err.Error())
	}
}

func TestModeSelectOp_Setup_SingleCategory(t *testing.T) {
	op := &ModeSelectOp{}
	err := op.Setup(mustParams(t, map[string]string{
		"categories": "only_one",
	}))
	if err == nil {
		t.Fatal("expected error when only one category is supplied, got nil")
	}
	if !strings.Contains(err.Error(), "at least 2") {
		t.Errorf("expected error to mention 'at least 2', got %q", err.Error())
	}
}

func TestModeSelectOp_Setup_WhitespaceOnlyCategories(t *testing.T) {
	op := &ModeSelectOp{}
	err := op.Setup(mustParams(t, map[string]string{
		"categories": " , , ",
	}))
	if err == nil {
		t.Fatal("expected error when 'categories' contains only whitespace, got nil")
	}
	if !strings.Contains(err.Error(), "at least 2") {
		t.Errorf("expected error to mention 'at least 2', got %q", err.Error())
	}
}

func TestAIClassifyMultiLabelOp_Setup_MissingCategories(t *testing.T) {
	op := &AIClassifyMultiLabelOp{}
	err := op.Setup(mustParams(t, map[string]string{}))
	if err == nil {
		t.Fatal("expected error when 'categories' param is missing, got nil")
	}
	if !strings.Contains(err.Error(), "categories") {
		t.Errorf("expected error to mention 'categories', got %q", err.Error())
	}
}

func TestAIClassifyMultiLabelOp_Setup_SingleCategory(t *testing.T) {
	op := &AIClassifyMultiLabelOp{}
	err := op.Setup(mustParams(t, map[string]string{
		"categories": "only_one",
	}))
	if err == nil {
		t.Fatal("expected error when only one category is supplied, got nil")
	}
	if !strings.Contains(err.Error(), "at least 2") {
		t.Errorf("expected error to mention 'at least 2', got %q", err.Error())
	}
}

func TestAIClassifyMultiLabelOp_Setup_WhitespaceOnlyCategories(t *testing.T) {
	op := &AIClassifyMultiLabelOp{}
	err := op.Setup(mustParams(t, map[string]string{
		"categories": " , , ",
	}))
	if err == nil {
		t.Fatal("expected error when 'categories' contains only whitespace, got nil")
	}
	if !strings.Contains(err.Error(), "at least 2") {
		t.Errorf("expected error to mention 'at least 2', got %q", err.Error())
	}
}

func TestAIScoreOp_Setup_MissingCriterion(t *testing.T) {
	op := &AIScoreOp{}
	err := op.Setup(mustParams(t, map[string]string{}))
	if err == nil {
		t.Fatal("expected error when 'criterion' param is missing, got nil")
	}
	if !strings.Contains(err.Error(), "criterion") {
		t.Errorf("expected error to mention 'criterion', got %q", err.Error())
	}
}

func TestAIBoolOp_Setup_MissingPredicate(t *testing.T) {
	op := &AIBoolOp{}
	err := op.Setup(mustParams(t, map[string]string{}))
	if err == nil {
		t.Fatal("expected error when 'predicate' param is missing, got nil")
	}
	if !strings.Contains(err.Error(), "predicate") {
		t.Errorf("expected error to mention 'predicate', got %q", err.Error())
	}
}

// TestMaxRetries_NonNumericFallsBackToDefault verifies that every op which
// accepts a `max_retries` param silently falls back to the default value of 3
// when the supplied value is non-numeric. Setup should NOT return an error.
func TestMaxRetries_NonNumericFallsBackToDefault(t *testing.T) {
	const defaultMaxRetries = 3

	t.Run("ModeSelectOp", func(t *testing.T) {
		op := &ModeSelectOp{}
		err := op.Setup(mustParams(t, map[string]string{
			"max_retries": "abc",
			"categories":  "a,b",
		}))
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.maxRetries != defaultMaxRetries {
			t.Errorf("expected maxRetries=%d, got %d", defaultMaxRetries, op.maxRetries)
		}
	})

	t.Run("AIClassifyMultiLabelOp", func(t *testing.T) {
		op := &AIClassifyMultiLabelOp{}
		err := op.Setup(mustParams(t, map[string]string{
			"max_retries": "abc",
			"categories":  "a,b",
		}))
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.maxRetries != defaultMaxRetries {
			t.Errorf("expected maxRetries=%d, got %d", defaultMaxRetries, op.maxRetries)
		}
	})

	t.Run("AIScoreOp", func(t *testing.T) {
		op := &AIScoreOp{}
		err := op.Setup(mustParams(t, map[string]string{
			"max_retries": "abc",
			"criterion":   "x",
		}))
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.maxRetries != defaultMaxRetries {
			t.Errorf("expected maxRetries=%d, got %d", defaultMaxRetries, op.maxRetries)
		}
	})

	t.Run("AIBoolOp", func(t *testing.T) {
		op := &AIBoolOp{}
		err := op.Setup(mustParams(t, map[string]string{
			"max_retries": "abc",
			"predicate":   "x",
		}))
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.maxRetries != defaultMaxRetries {
			t.Errorf("expected maxRetries=%d, got %d", defaultMaxRetries, op.maxRetries)
		}
	})

	t.Run("AIBestMatchOp", func(t *testing.T) {
		op := &AIBestMatchOp{}
		err := op.Setup(mustParams(t, map[string]string{
			"max_retries": "abc",
		}))
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.maxRetries != defaultMaxRetries {
			t.Errorf("expected maxRetries=%d, got %d", defaultMaxRetries, op.maxRetries)
		}
	})

	t.Run("AIRerankOp", func(t *testing.T) {
		op := &AIRerankOp{}
		err := op.Setup(mustParams(t, map[string]string{
			"max_retries": "abc",
		}))
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.maxRetries != defaultMaxRetries {
			t.Errorf("expected maxRetries=%d, got %d", defaultMaxRetries, op.maxRetries)
		}
	})

	t.Run("AIComputeOp", func(t *testing.T) {
		// AIComputeOp[string, float64] is generic; use AIParseNumberOp as the
		// concrete instance (it embeds AIComputeOp[string, float64]).
		op := &AIParseNumberOp{}
		err := op.Setup(mustParams(t, map[string]string{
			"max_retries": "abc",
		}))
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.maxRetries != defaultMaxRetries {
			t.Errorf("expected maxRetries=%d, got %d", defaultMaxRetries, op.maxRetries)
		}
	})
}

// TestSetInputField_WrongType verifies that every op's SetInputField returns a
// non-nil, non-empty error when called with a wrong-typed value. This is pure
// validation logic and requires no API key or Setup() call.
func TestSetInputField_WrongType(t *testing.T) {
	assertWrongType := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("expected non-nil error for wrong-typed value, got nil")
		}
		if err.Error() == "" {
			t.Errorf("expected non-empty error message, got empty string")
		}
	}

	t.Run("ModeSelectOp", func(t *testing.T) {
		op := &ModeSelectOp{}
		assertWrongType(t, op.SetInputField("Input", 42))
	})

	t.Run("AIClassifyMultiLabelOp", func(t *testing.T) {
		op := &AIClassifyMultiLabelOp{}
		assertWrongType(t, op.SetInputField("Input", 42))
	})

	t.Run("AIScoreOp", func(t *testing.T) {
		op := &AIScoreOp{}
		assertWrongType(t, op.SetInputField("Input", 42))
	})

	t.Run("AIBoolOp", func(t *testing.T) {
		op := &AIBoolOp{}
		assertWrongType(t, op.SetInputField("Input", 42))
	})

	t.Run("AIBestMatchOp_Query", func(t *testing.T) {
		op := &AIBestMatchOp{}
		assertWrongType(t, op.SetInputField("Query", 42))
	})

	t.Run("AIBestMatchOp_Candidates", func(t *testing.T) {
		op := &AIBestMatchOp{}
		s := "not a slice"
		assertWrongType(t, op.SetInputField("Candidates", &s))
	})

	t.Run("AIRerankOp_Query", func(t *testing.T) {
		op := &AIRerankOp{}
		assertWrongType(t, op.SetInputField("Query", 42))
	})

	t.Run("AIRerankOp_Candidates", func(t *testing.T) {
		op := &AIRerankOp{}
		assertWrongType(t, op.SetInputField("Candidates", 42))
	})

	t.Run("AIComputeOp", func(t *testing.T) {
		// AIParseNumberOp embeds AIComputeOp[string, float64], so Input is *string.
		op := &AIParseNumberOp{}
		i := 42
		assertWrongType(t, op.SetInputField("Input", &i))
	})
}

// TestResetFields verifies that every op's ResetFields() method nils out its
// input pointer fields and zeros all output fields. ResetFields is pure — no
// API key required.
func TestResetFields(t *testing.T) {
	assertNilPtr := func(t *testing.T, name string, got any) {
		t.Helper()
		// Compare the interface to nil directly; for typed nil pointers the
		// caller passes the dereferenced check via reflect-style equality.
		switch v := got.(type) {
		case *string:
			if v != nil {
				t.Errorf("expected %s to be nil, got %v", name, v)
			}
		case *[]string:
			if v != nil {
				t.Errorf("expected %s to be nil, got %v", name, v)
			}
		default:
			if got != nil {
				t.Errorf("expected %s to be nil, got %v", name, got)
			}
		}
	}

	t.Run("ModeSelectOp", func(t *testing.T) {
		op := &ModeSelectOp{}
		s := "x"
		op.Input = &s
		op.Result = "foo"
		op.ResetFields()
		assertNilPtr(t, "Input", op.Input)
		if op.Result != "" {
			t.Errorf("expected Result to be \"\", got %q", op.Result)
		}
	})

	t.Run("AIClassifyMultiLabelOp", func(t *testing.T) {
		op := &AIClassifyMultiLabelOp{}
		s := "x"
		op.Input = &s
		op.Result = []string{"a", "b"}
		op.Reasoning = "bar"
		op.ResetFields()
		assertNilPtr(t, "Input", op.Input)
		if len(op.Result) != 0 {
			t.Errorf("expected Result to be nil/empty, got %v", op.Result)
		}
		if op.Reasoning != "" {
			t.Errorf("expected Reasoning to be \"\", got %q", op.Reasoning)
		}
	})

	t.Run("AIScoreOp", func(t *testing.T) {
		op := &AIScoreOp{}
		s := "x"
		op.Input = &s
		op.Result = 1.5
		op.Reasoning = "bar"
		op.ResetFields()
		assertNilPtr(t, "Input", op.Input)
		if op.Result != 0 {
			t.Errorf("expected Result to be 0, got %v", op.Result)
		}
		if op.Reasoning != "" {
			t.Errorf("expected Reasoning to be \"\", got %q", op.Reasoning)
		}
	})

	t.Run("AIBoolOp", func(t *testing.T) {
		op := &AIBoolOp{}
		s := "x"
		op.Input = &s
		op.Result = true
		op.Reasoning = "bar"
		op.ResetFields()
		assertNilPtr(t, "Input", op.Input)
		if op.Result != false {
			t.Errorf("expected Result to be false, got %v", op.Result)
		}
		if op.Reasoning != "" {
			t.Errorf("expected Reasoning to be \"\", got %q", op.Reasoning)
		}
	})

	t.Run("AIBestMatchOp", func(t *testing.T) {
		op := &AIBestMatchOp{}
		q := "x"
		c := []string{"a", "b"}
		op.Query = &q
		op.Candidates = &c
		op.Result = 7
		op.Reasoning = "bar"
		op.ResetFields()
		assertNilPtr(t, "Query", op.Query)
		assertNilPtr(t, "Candidates", op.Candidates)
		if op.Result != 0 {
			t.Errorf("expected Result to be 0, got %v", op.Result)
		}
		if op.Reasoning != "" {
			t.Errorf("expected Reasoning to be \"\", got %q", op.Reasoning)
		}
	})

	t.Run("AIRerankOp", func(t *testing.T) {
		op := &AIRerankOp{}
		q := "x"
		c := []string{"a", "b"}
		op.Query = &q
		op.Candidates = &c
		op.Result = []int{1, 2, 3}
		op.Reasoning = "bar"
		op.ResetFields()
		assertNilPtr(t, "Query", op.Query)
		assertNilPtr(t, "Candidates", op.Candidates)
		if len(op.Result) != 0 {
			t.Errorf("expected Result to be nil/empty, got %v", op.Result)
		}
		if op.Reasoning != "" {
			t.Errorf("expected Reasoning to be \"\", got %q", op.Reasoning)
		}
	})

	t.Run("AIComputeOp", func(t *testing.T) {
		// AIParseNumberOp embeds AIComputeOp[string, float64].
		op := &AIParseNumberOp{}
		s := "x"
		op.Input = &s
		op.Result = 3.14
		op.Reasoning = "bar"
		op.ResetFields()
		assertNilPtr(t, "Input", op.Input)
		if op.Result != 0 {
			t.Errorf("expected Result to be 0, got %v", op.Result)
		}
		if op.Reasoning != "" {
			t.Errorf("expected Reasoning to be \"\", got %q", op.Reasoning)
		}
	})
}

// TestOutputFields_NoReasoningWire verifies that no AI op exposes "Reasoning"
// as an output wire. This guards against accidentally re-adding it, which would
// silently break reasoning-mode callers who read from the ReasoningLog instead.
func TestOutputFields_NoReasoningWire(t *testing.T) {
	check := func(t *testing.T, fields map[string]any) {
		t.Helper()
		if _, ok := fields["Reasoning"]; ok {
			t.Error("OutputFields() must not contain 'Reasoning'")
		}
	}

	t.Run("AIClassifyMultiLabelOp", func(t *testing.T) { check(t, (&AIClassifyMultiLabelOp{}).OutputFields()) })
	t.Run("AIScoreOp", func(t *testing.T) { check(t, (&AIScoreOp{}).OutputFields()) })
	t.Run("AIBoolOp", func(t *testing.T) { check(t, (&AIBoolOp{}).OutputFields()) })
	t.Run("AIBestMatchOp", func(t *testing.T) { check(t, (&AIBestMatchOp{}).OutputFields()) })
	t.Run("AIRerankOp", func(t *testing.T) { check(t, (&AIRerankOp{}).OutputFields()) })
	t.Run("AIComputeOp", func(t *testing.T) { check(t, (&AIParseNumberOp{}).OutputFields()) })
	t.Run("ModeSelectOp", func(t *testing.T) { check(t, (&ModeSelectOp{}).OutputFields()) })
}

// TestAIComputeOp_parseResult_Float64 drives the unexported parseResult method
// directly via AIParseNumberOp (which embeds AIComputeOp[string, float64]).
// Verifies the *float64 case: TrimSpace + strconv.ParseFloat semantics.
func TestAIComputeOp_parseResult_Float64(t *testing.T) {
	t.Run("3.14", func(t *testing.T) {
		op := &AIParseNumberOp{}
		if err := op.parseResult("3.14"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != 3.14 {
			t.Errorf("expected Result=3.14, got %v", op.Result)
		}
	})

	t.Run("3.14e2", func(t *testing.T) {
		op := &AIParseNumberOp{}
		if err := op.parseResult("3.14e2"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != 314.0 {
			t.Errorf("expected Result=314.0, got %v", op.Result)
		}
	})

	t.Run("whitespace", func(t *testing.T) {
		op := &AIParseNumberOp{}
		if err := op.parseResult("  3.14  "); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != 3.14 {
			t.Errorf("expected Result=3.14, got %v", op.Result)
		}
	})

	t.Run("abc", func(t *testing.T) {
		op := &AIParseNumberOp{}
		err := op.parseResult("abc")
		if err == nil {
			t.Fatal("expected non-nil error for non-numeric input, got nil")
		}
		if !strings.Contains(err.Error(), "expected float64") {
			t.Errorf("expected error to contain 'expected float64', got %q", err.Error())
		}
	})
}

// TestAIComputeOp_parseResult_Int drives the unexported parseResult method
// directly on a generic AIComputeOp[string, int] instance. Verifies the *int
// case: TrimSpace + strconv.Atoi semantics.
func TestAIComputeOp_parseResult_Int(t *testing.T) {
	t.Run("42", func(t *testing.T) {
		op := &AIComputeOp[string, int]{}
		if err := op.parseResult("42"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != 42 {
			t.Errorf("expected Result=42, got %v", op.Result)
		}
	})

	t.Run("-17", func(t *testing.T) {
		op := &AIComputeOp[string, int]{}
		if err := op.parseResult("-17"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != -17 {
			t.Errorf("expected Result=-17, got %v", op.Result)
		}
	})

	t.Run("3.14", func(t *testing.T) {
		op := &AIComputeOp[string, int]{}
		err := op.parseResult("3.14")
		if err == nil {
			t.Fatal("expected non-nil error for non-integer input, got nil")
		}
		if !strings.Contains(err.Error(), "expected int") {
			t.Errorf("expected error to contain 'expected int', got %q", err.Error())
		}
	})
}

// TestAIComputeOp_parseResult_Float64Slice drives the unexported parseResult
// method directly on a generic AIComputeOp[string, []float64] instance.
// Verifies the *[]float64 case: empty string yields nil, otherwise CSV split
// with TrimSpace + strconv.ParseFloat per non-empty part.
func TestAIComputeOp_parseResult_Float64Slice(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		op := &AIComputeOp[string, []float64]{}
		if err := op.parseResult(""); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != nil {
			t.Errorf("expected Result to be nil, got %v", op.Result)
		}
	})

	t.Run("1, 2, 3", func(t *testing.T) {
		op := &AIComputeOp[string, []float64]{}
		if err := op.parseResult("1, 2, 3"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		want := []float64{1, 2, 3}
		if !reflect.DeepEqual(op.Result, want) {
			t.Errorf("expected Result=%v, got %v", want, op.Result)
		}
	})

	t.Run("1,abc,3", func(t *testing.T) {
		op := &AIComputeOp[string, []float64]{}
		err := op.parseResult("1,abc,3")
		if err == nil {
			t.Fatal("expected non-nil error for non-numeric part, got nil")
		}
		if !strings.Contains(err.Error(), "expected []float64") {
			t.Errorf("expected error to contain 'expected []float64', got %q", err.Error())
		}
	})

	t.Run("trailing comma", func(t *testing.T) {
		op := &AIComputeOp[string, []float64]{}
		if err := op.parseResult("1,2,3,"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		want := []float64{1, 2, 3}
		if !reflect.DeepEqual(op.Result, want) {
			t.Errorf("expected Result=%v, got %v", want, op.Result)
		}
	})
}

// TestAIComputeOp_parseResult_IntSlice drives the unexported parseResult
// method directly on a generic AIComputeOp[string, []int] instance.
// Verifies the *[]int case: empty string yields nil, otherwise CSV split
// with TrimSpace + strconv.Atoi per non-empty part.
func TestAIComputeOp_parseResult_IntSlice(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		op := &AIComputeOp[string, []int]{}
		if err := op.parseResult(""); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != nil {
			t.Errorf("expected Result to be nil, got %v", op.Result)
		}
	})

	t.Run("1, 2, 3", func(t *testing.T) {
		op := &AIComputeOp[string, []int]{}
		if err := op.parseResult("1, 2, 3"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		want := []int{1, 2, 3}
		if !reflect.DeepEqual(op.Result, want) {
			t.Errorf("expected Result=%v, got %v", want, op.Result)
		}
	})

	t.Run("1,abc,3", func(t *testing.T) {
		op := &AIComputeOp[string, []int]{}
		err := op.parseResult("1,abc,3")
		if err == nil {
			t.Fatal("expected non-nil error for non-integer part, got nil")
		}
		if !strings.Contains(err.Error(), "expected []int") {
			t.Errorf("expected error to contain 'expected []int', got %q", err.Error())
		}
	})

	t.Run("trailing comma", func(t *testing.T) {
		op := &AIComputeOp[string, []int]{}
		if err := op.parseResult("1,2,3,"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		want := []int{1, 2, 3}
		if !reflect.DeepEqual(op.Result, want) {
			t.Errorf("expected Result=%v, got %v", want, op.Result)
		}
	})
}

// TestAIComputeOp_parseResult_StringSlice drives the unexported parseResult
// method directly on a generic AIComputeOp[string, []string] instance.
// Verifies the *[]string case: empty string yields nil, otherwise CSV split
// with TrimSpace per part and empty parts skipped. There is NO quote handling
// — embedded commas inside quoted items still split.
func TestAIComputeOp_parseResult_StringSlice(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		op := &AIComputeOp[string, []string]{}
		if err := op.parseResult(""); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != nil {
			t.Errorf("expected Result to be nil, got %v", op.Result)
		}
	})

	t.Run("a, b, c", func(t *testing.T) {
		op := &AIComputeOp[string, []string]{}
		if err := op.parseResult("a, b, c"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		want := []string{"a", "b", "c"}
		if !reflect.DeepEqual(op.Result, want) {
			t.Errorf("expected Result=%v, got %v", want, op.Result)
		}
	})

	t.Run("empty entries skipped", func(t *testing.T) {
		op := &AIComputeOp[string, []string]{}
		if err := op.parseResult("a,,b,"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		want := []string{"a", "b"}
		if !reflect.DeepEqual(op.Result, want) {
			t.Errorf("expected Result=%v, got %v", want, op.Result)
		}
	})

	t.Run("quoting limitation", func(t *testing.T) {
		// Pin the current behavior: parseResult does NOT honor quoted commas.
		// Input is the literal string: hello, "world, foo"
		// After splitting on comma and trimming each part, literal quotes are
		// preserved, yielding 3 elements: ["hello", "\"world", "foo\""].
		op := &AIComputeOp[string, []string]{}
		if err := op.parseResult("hello, \"world, foo\""); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if len(op.Result) != 3 {
			t.Errorf("expected len(Result)=3 (quoting NOT honored), got %d: %v", len(op.Result), op.Result)
		}
		want := []string{"hello", "\"world", "foo\""}
		if !reflect.DeepEqual(op.Result, want) {
			t.Errorf("expected Result=%v, got %v", want, op.Result)
		}
	})
}

// TestAIComputeOp_parseResult_Bool drives the unexported parseResult method
// directly on a generic AIComputeOp[string, bool] instance. Verifies the *bool
// case: lowercased trimmed input, "true"/"yes" -> true, "false"/"no" -> false,
// anything else returns an error containing "expected bool".
func TestAIComputeOp_parseResult_Bool(t *testing.T) {
	t.Run("true", func(t *testing.T) {
		op := &AIComputeOp[string, bool]{}
		if err := op.parseResult("true"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != true {
			t.Errorf("expected Result=true, got %v", op.Result)
		}
	})

	t.Run("TRUE", func(t *testing.T) {
		op := &AIComputeOp[string, bool]{}
		if err := op.parseResult("TRUE"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != true {
			t.Errorf("expected Result=true, got %v", op.Result)
		}
	})

	t.Run("yes", func(t *testing.T) {
		op := &AIComputeOp[string, bool]{}
		if err := op.parseResult("yes"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != true {
			t.Errorf("expected Result=true, got %v", op.Result)
		}
	})

	t.Run("Yes", func(t *testing.T) {
		op := &AIComputeOp[string, bool]{}
		if err := op.parseResult("Yes"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != true {
			t.Errorf("expected Result=true, got %v", op.Result)
		}
	})

	t.Run("false", func(t *testing.T) {
		op := &AIComputeOp[string, bool]{}
		if err := op.parseResult("false"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != false {
			t.Errorf("expected Result=false, got %v", op.Result)
		}
	})

	t.Run("FALSE", func(t *testing.T) {
		op := &AIComputeOp[string, bool]{}
		if err := op.parseResult("FALSE"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != false {
			t.Errorf("expected Result=false, got %v", op.Result)
		}
	})

	t.Run("no", func(t *testing.T) {
		op := &AIComputeOp[string, bool]{}
		if err := op.parseResult("no"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != false {
			t.Errorf("expected Result=false, got %v", op.Result)
		}
	})

	t.Run("No", func(t *testing.T) {
		op := &AIComputeOp[string, bool]{}
		if err := op.parseResult("No"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result != false {
			t.Errorf("expected Result=false, got %v", op.Result)
		}
	})

	t.Run("maybe", func(t *testing.T) {
		op := &AIComputeOp[string, bool]{}
		err := op.parseResult("maybe")
		if err == nil {
			t.Fatal("expected non-nil error for non-bool input, got nil")
		}
		if !strings.Contains(err.Error(), "expected bool") {
			t.Errorf("expected error to contain 'expected bool', got %q", err.Error())
		}
	})
}

// TestAIComputeOp_parseResult_MapStringString drives the unexported parseResult
// method directly on a generic AIComputeOp[string, map[string]string] instance.
// Verifies the *map[string]string case: empty string yields a non-nil empty
// map, otherwise CSV split with each pair trimmed; first '=' separates key from
// value (both trimmed); empty pairs skipped; pair without '=' returns an error.
func TestAIComputeOp_parseResult_MapStringString(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		op := &AIComputeOp[string, map[string]string]{}
		if err := op.parseResult(""); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if op.Result == nil {
			t.Fatal("expected Result to be non-nil empty map, got nil")
		}
		if len(op.Result) != 0 {
			t.Errorf("expected len(Result)=0, got %d: %v", len(op.Result), op.Result)
		}
	})

	t.Run("k=v,a=b", func(t *testing.T) {
		op := &AIComputeOp[string, map[string]string]{}
		if err := op.parseResult("k=v,a=b"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		want := map[string]string{"k": "v", "a": "b"}
		if !reflect.DeepEqual(op.Result, want) {
			t.Errorf("expected Result=%v, got %v", want, op.Result)
		}
	})

	t.Run("keyonly", func(t *testing.T) {
		op := &AIComputeOp[string, map[string]string]{}
		err := op.parseResult("keyonly")
		if err == nil {
			t.Fatal("expected non-nil error for pair without '=', got nil")
		}
		if !strings.Contains(err.Error(), "expected key=value pair") {
			t.Errorf("expected error to contain 'expected key=value pair', got %q", err.Error())
		}
	})

	t.Run("trim around equals and pair", func(t *testing.T) {
		op := &AIComputeOp[string, map[string]string]{}
		if err := op.parseResult("  k  =  v  "); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		want := map[string]string{"k": "v"}
		if !reflect.DeepEqual(op.Result, want) {
			t.Errorf("expected Result=%v, got %v", want, op.Result)
		}
	})
}
