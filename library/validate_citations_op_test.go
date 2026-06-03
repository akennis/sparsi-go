package library

import (
	"context"
	"reflect"
	"testing"

	"github.com/akennis/dagor/config"
)

// runOp drives the standard Setup/Reset/Run lifecycle for an op under test.
// Centralizing it makes the table-driven cases below read as pure
// input/expected pairs.
func runValidateCitationsOp(t *testing.T, raw, allowed *[]string) *ValidateCitationsOp {
	t.Helper()
	op := &ValidateCitationsOp{Raw: raw, Allowed: allowed}
	if err := op.Setup(&config.Params{}); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if err := op.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return op
}

func ptrStrSlice(s []string) *[]string {
	return &s
}

func TestValidateCitationsOp_HappyPath_OrderAndAcceptReject(t *testing.T) {
	// Some accepted (b, c), some rejected (x, y). Output order must mirror
	// the Raw input order within each bucket.
	raw := []string{"a", "x", "b", "y", "c"}
	allowed := []string{"a", "b", "c"}
	op := runValidateCitationsOp(t, ptrStrSlice(raw), ptrStrSlice(allowed))

	wantAccepted := []string{"a", "b", "c"}
	wantRejected := []string{"x", "y"}
	if !reflect.DeepEqual(op.Accepted, wantAccepted) {
		t.Errorf("Accepted = %v, want %v", op.Accepted, wantAccepted)
	}
	if !reflect.DeepEqual(op.Rejected, wantRejected) {
		t.Errorf("Rejected = %v, want %v", op.Rejected, wantRejected)
	}
}

func TestValidateCitationsOp_EmptyAllowed_AllRejected(t *testing.T) {
	// An empty allow-list with non-empty Raw must reject everything (this is
	// the "we retrieved no documents but the model still cited something"
	// path; every citation is a hallucination by construction).
	raw := []string{"a", "b", "c"}
	allowed := []string{}
	op := runValidateCitationsOp(t, ptrStrSlice(raw), ptrStrSlice(allowed))

	if len(op.Accepted) != 0 {
		t.Errorf("Accepted = %v, want empty", op.Accepted)
	}
	wantRejected := []string{"a", "b", "c"}
	if !reflect.DeepEqual(op.Rejected, wantRejected) {
		t.Errorf("Rejected = %v, want %v", op.Rejected, wantRejected)
	}
}

func TestValidateCitationsOp_EmptyRaw_EmptyOutputs(t *testing.T) {
	// Nothing to validate. Both outputs are nil/empty regardless of what
	// Allowed contains.
	raw := []string{}
	allowed := []string{"a", "b"}
	op := runValidateCitationsOp(t, ptrStrSlice(raw), ptrStrSlice(allowed))

	if len(op.Accepted) != 0 {
		t.Errorf("Accepted = %v, want empty", op.Accepted)
	}
	if len(op.Rejected) != 0 {
		t.Errorf("Rejected = %v, want empty", op.Rejected)
	}
}

func TestValidateCitationsOp_DuplicateRaw_DedupeAccepted(t *testing.T) {
	// A model that repeats the same source name in its citation list must
	// not produce duplicate Accepted entries — the downstream UI would
	// otherwise render "Sources: foo, foo".
	raw := []string{"a", "a", "a"}
	allowed := []string{"a"}
	op := runValidateCitationsOp(t, ptrStrSlice(raw), ptrStrSlice(allowed))

	wantAccepted := []string{"a"}
	if !reflect.DeepEqual(op.Accepted, wantAccepted) {
		t.Errorf("Accepted = %v, want %v", op.Accepted, wantAccepted)
	}
	if len(op.Rejected) != 0 {
		t.Errorf("Rejected = %v, want empty", op.Rejected)
	}
}

func TestValidateCitationsOp_DuplicateRaw_DedupeRejected(t *testing.T) {
	// Same de-dupe contract on the Rejected side: a model that hallucinates
	// the same fake filename twice should produce one slog Warn line, not N.
	raw := []string{"x", "x", "x"}
	allowed := []string{"a"}
	op := runValidateCitationsOp(t, ptrStrSlice(raw), ptrStrSlice(allowed))

	if len(op.Accepted) != 0 {
		t.Errorf("Accepted = %v, want empty", op.Accepted)
	}
	wantRejected := []string{"x"}
	if !reflect.DeepEqual(op.Rejected, wantRejected) {
		t.Errorf("Rejected = %v, want %v", op.Rejected, wantRejected)
	}
}

func TestValidateCitationsOp_NilRawPointer_NoPanic(t *testing.T) {
	// A disconnected upstream wire materializes as a nil *[]string. The op
	// must treat this as "empty Raw" — no panic, both outputs empty.
	op := runValidateCitationsOp(t, nil, ptrStrSlice([]string{"a"}))
	if len(op.Accepted) != 0 {
		t.Errorf("Accepted = %v, want empty", op.Accepted)
	}
	if len(op.Rejected) != 0 {
		t.Errorf("Rejected = %v, want empty", op.Rejected)
	}
}

func TestValidateCitationsOp_NilAllowedPointer_EverythingRejected(t *testing.T) {
	// A disconnected Allowed wire is the secure-default: with no allow-list
	// installed, every citation falls into Rejected. The op must not panic.
	raw := []string{"a", "b"}
	op := runValidateCitationsOp(t, ptrStrSlice(raw), nil)
	if len(op.Accepted) != 0 {
		t.Errorf("Accepted = %v, want empty", op.Accepted)
	}
	wantRejected := []string{"a", "b"}
	if !reflect.DeepEqual(op.Rejected, wantRejected) {
		t.Errorf("Rejected = %v, want %v", op.Rejected, wantRejected)
	}
}
