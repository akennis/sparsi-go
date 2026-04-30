package library

import (
	"context"
	"sync"
	"testing"
)

// ---- WithReasoningLog / logFromCtx ----

func TestWithReasoningLog_InjectsLog(t *testing.T) {
	ctx := context.Background()
	derived, log := WithReasoningLog(ctx)

	if log == nil {
		t.Fatal("WithReasoningLog returned nil log")
	}
	got := logFromCtx(derived)
	if got != log {
		t.Errorf("logFromCtx returned different pointer: got %p, want %p", got, log)
	}
}

func TestLogFromCtx_NilOnPlainContext(t *testing.T) {
	if l := logFromCtx(context.Background()); l != nil {
		t.Errorf("expected nil on plain context, got %p", l)
	}
}

func TestWithReasoningLog_DoesNotMutateParentContext(t *testing.T) {
	parent := context.Background()
	_, _ = WithReasoningLog(parent)
	if l := logFromCtx(parent); l != nil {
		t.Error("parent context was mutated by WithReasoningLog")
	}
}

// ---- ReasoningLog.Entries ----

func TestReasoningLog_EmptyByDefault(t *testing.T) {
	ctx, log := WithReasoningLog(context.Background())
	_ = ctx
	if entries := log.Entries(); len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestReasoningLog_SingleEntry(t *testing.T) {
	ctx, log := WithReasoningLog(context.Background())

	recordReasoning(ctx, "TestOp", map[string]any{"k": "v"}, "result-value", "the reason")

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Op != "TestOp" {
		t.Errorf("Op: got %q, want %q", e.Op, "TestOp")
	}
	if e.Reasoning != "the reason" {
		t.Errorf("Reasoning: got %q, want %q", e.Reasoning, "the reason")
	}
	if got, ok := e.Inputs["k"].(string); !ok || got != "v" {
		t.Errorf("Inputs[k]: got %v, want %q", e.Inputs["k"], "v")
	}
	if got, ok := e.Output.(string); !ok || got != "result-value" {
		t.Errorf("Output: got %v, want %q", e.Output, "result-value")
	}
}

func TestReasoningLog_MultipleEntriesAllAppended(t *testing.T) {
	ctx, log := WithReasoningLog(context.Background())

	recordReasoning(ctx, "OpA", map[string]any{"n": 1}, 10, "reason A")
	recordReasoning(ctx, "OpB", map[string]any{"n": 2}, 20, "reason B")
	recordReasoning(ctx, "OpC", map[string]any{"n": 3}, 30, "reason C")

	entries := log.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(entries), entries)
	}
	for i, want := range []string{"OpA", "OpB", "OpC"} {
		if entries[i].Op != want {
			t.Errorf("entries[%d].Op: got %q, want %q", i, entries[i].Op, want)
		}
	}
}

func TestReasoningLog_EntriesIsSnapshot(t *testing.T) {
	ctx, log := WithReasoningLog(context.Background())
	recordReasoning(ctx, "Op1", nil, nil, "r1")

	snap := log.Entries()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}

	// Mutate the snapshot — should not affect the log.
	snap[0].Op = "MUTATED"
	snap = append(snap, ReasoningEntry{Op: "EXTRA"})

	fresh := log.Entries()
	if len(fresh) != 1 {
		t.Errorf("log grew after mutating snapshot: got %d entries", len(fresh))
	}
	if fresh[0].Op != "Op1" {
		t.Errorf("log entry was mutated through snapshot: got Op=%q", fresh[0].Op)
	}
}

// ---- recordReasoning ----

func TestRecordReasoning_NoopOnPlainContext(t *testing.T) {
	// Must not panic and must not affect any log.
	recordReasoning(context.Background(), "Op", map[string]any{"x": 1}, nil, "reason")
}

func TestRecordReasoning_InputsPreserved(t *testing.T) {
	ctx, log := WithReasoningLog(context.Background())

	inputs := map[string]any{
		"Text":      "hello world",
		"Criterion": "toxicity",
	}
	recordReasoning(ctx, "AIScoreOp", inputs, 0.1, "low toxicity")

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if got, ok := entries[0].Inputs["Text"].(string); !ok || got != "hello world" {
		t.Errorf("Inputs[Text]: got %v, want %q", entries[0].Inputs["Text"], "hello world")
	}
	if got, ok := entries[0].Inputs["Criterion"].(string); !ok || got != "toxicity" {
		t.Errorf("Inputs[Criterion]: got %v, want %q", entries[0].Inputs["Criterion"], "toxicity")
	}
	if got, ok := entries[0].Output.(float64); !ok || got != 0.1 {
		t.Errorf("Output: got %v, want 0.1", entries[0].Output)
	}
}

// ---- Concurrency ----

func TestReasoningLog_ConcurrentRecord(t *testing.T) {
	ctx, log := WithReasoningLog(context.Background())

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			recordReasoning(ctx, "Op", map[string]any{"i": i}, i, "reason")
		}()
	}
	wg.Wait()

	entries := log.Entries()
	if len(entries) != n {
		t.Errorf("expected %d entries after concurrent writes, got %d", n, len(entries))
	}
}
