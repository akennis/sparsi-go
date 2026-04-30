package library

import (
	"context"
	"sync"

	"github.com/wwz16/dagor"
)

// ReasoningEntry records the reasoning produced by one AI op invocation.
// Inputs holds a snapshot of the op's input values at the time of the call,
// keyed by field name (e.g. "Input", "Query", "Candidates").
// Output holds the value the op produced (e.g. float64 for AIScoreOp, bool for AIBoolOp).
type ReasoningEntry struct {
	Op        string         // e.g. "AIScoreOp", "AIComputeOp"
	RunID     string         // dagor workflow run ID for correlation with logs
	Inputs    map[string]any // snapshot of input values at invocation time
	Output    any            // the value the op produced
	Reasoning string
}

// ReasoningLog collects ReasoningEntry values written by AI ops during a
// workflow run. It is safe for concurrent use.
type ReasoningLog struct {
	mu      sync.Mutex
	entries []ReasoningEntry
}

// Entries returns a snapshot of all collected entries in recording order.
func (l *ReasoningLog) Entries() []ReasoningEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]ReasoningEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

func (l *ReasoningLog) record(op, runID string, inputs map[string]any, output any, reasoning string) {
	l.mu.Lock()
	l.entries = append(l.entries, ReasoningEntry{Op: op, RunID: runID, Inputs: inputs, Output: output, Reasoning: reasoning})
	l.mu.Unlock()
}

type reasoningLogKey struct{}

// WithReasoningLog derives a new context carrying a fresh ReasoningLog and
// returns both. Pass the derived context to eng.Run; AI ops that run inside
// it will request and record reasoning. Read entries from the returned log
// pointer after eng.Run returns — no context lookup required.
//
// When the log is absent from context (the default), AI ops skip reasoning
// entirely and behave as if the feature does not exist.
func WithReasoningLog(ctx context.Context) (context.Context, *ReasoningLog) {
	l := &ReasoningLog{}
	return context.WithValue(ctx, reasoningLogKey{}, l), l
}

// logFromCtx returns the ReasoningLog stored in ctx, or nil if reasoning is
// not enabled for this run.
func logFromCtx(ctx context.Context) *ReasoningLog {
	l, _ := ctx.Value(reasoningLogKey{}).(*ReasoningLog)
	return l
}

// recordReasoning appends an entry to the log in ctx if one is present.
// inputs should be a shallow copy of the op's input field values (not pointers).
// output should be the value the op produced (already set on the op struct).
// Safe to call unconditionally — it is a no-op when reasoning is disabled.
func recordReasoning(ctx context.Context, op string, inputs map[string]any, output any, reasoning string) {
	if l := logFromCtx(ctx); l != nil {
		l.record(op, dagor.RunID(ctx), inputs, output, reasoning)
	}
}
