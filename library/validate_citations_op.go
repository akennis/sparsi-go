package library

import (
	"context"
	"log/slog"

	"github.com/akennis/dagor"
	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

const ValidateCitationsOpDescription = `ValidateCitationsOp: filters LLM-emitted citations against an allow-list of source identifiers — a security control. Hallucinated source names (anything the model invents or imports from its training corpus) are dropped; only entries that exactly match a member of the allow-list survive into Accepted. Use this op to enforce citation integrity at the boundary between an untrusted AI op and a downstream surface (UI, audit log, database write).
  Inputs:   Raw *[]string — the citations parsed out of the model's response (typically the Sources slice produced by your example's ParseCitations step over the AI op's raw output). Order is preserved into Accepted / Rejected.
            Allowed *[]string — the allow-list of source identifiers the model was permitted to cite. The canonical pattern is: pipe the retrieved []library.Document slice (from RetrieveOp or RetrieveWithFiltersOp) into a sources-extraction step that emits []string of identifiers (e.g. each Document's Metadata[library.MetadataSource]), then wire that []string here. Build the allow-list from the documents the model actually saw, NOT from the full loaded corpus — a model that hallucinates the filename of a real-but-unretrieved document would otherwise slip past the check.
  Outputs:  Accepted []string — citations from Raw that appear in Allowed. De-duplicated (a model that repeats the same source twice yields one Accepted entry) and ordered by first appearance in Raw.
            Rejected []string — citations from Raw that do NOT appear in Allowed. De-duplicated and ordered by first appearance in Raw. Surface these via slog at Warn (not Error) for observability — they are signal about model behavior, not graph failure.
  Membership is an exact string match against the Allowed set; no normalization, no case-folding, no whitespace trimming (upstream parsing is expected to trim already). Nil/empty Raw -> both outputs empty. Nil/empty Allowed with non-empty Raw -> every citation rejected. Run never returns an error; this is a pure deterministic op (no I/O, no parsing).
  WARNING: this op is the right place to enforce citation integrity. Do NOT route unfiltered citations (the Raw input, or ParseCitations output upstream of this op) to user-facing surfaces, audit logs, or downstream tooling where they will be treated as trustworthy — ALWAYS route the Accepted output instead, and slog-warn the Rejected output.`

// ValidateCitationsOp filters a list of model-emitted citations against an
// allow-list of source identifiers. The canonical wiring is:
//
//	RetrieveOp.Documents  -> <your sources-extraction step>.Documents
//	                                  └──> ValidateCitationsOp.Allowed
//	AI op .Result -> ParseCitations.Raw -> ParseCitations.Sources
//	                                  └──> ValidateCitationsOp.Raw
//
// Outputs are de-duplicated and order-preserving. The op is deterministic,
// I/O-free, and never returns an error in normal operation.
type ValidateCitationsOp struct {
	Raw      *[]string `dag:"input"`
	Allowed  *[]string `dag:"input"`
	Accepted []string  `dag:"output"`
	Rejected []string  `dag:"output"`
}

func (op *ValidateCitationsOp) Setup(_ *config.Params) error { return nil }
func (op *ValidateCitationsOp) Reset() error                 { return nil }

func (op *ValidateCitationsOp) Run(ctx context.Context) error {
	// Nil/empty Raw -> both outputs empty. Honor the "preserve order, de-dupe"
	// contract by always producing non-nil zero-length slices when Raw is
	// present but empty (callers can distinguish "ran" vs "didn't run" from
	// the op's vertex state, not from a nil/empty distinction).
	if op.Raw == nil || len(*op.Raw) == 0 {
		op.Accepted = nil
		op.Rejected = nil
		return nil
	}

	// Build the allow-set once. A nil/empty Allowed pointer is fine: the set
	// is empty and every citation falls into Rejected.
	var allowSet map[string]struct{}
	if op.Allowed != nil {
		allowSet = make(map[string]struct{}, len(*op.Allowed))
		for _, s := range *op.Allowed {
			allowSet[s] = struct{}{}
		}
	}

	accepted := make([]string, 0, len(*op.Raw))
	rejected := make([]string, 0, len(*op.Raw))
	seenAccepted := make(map[string]struct{}, len(*op.Raw))
	seenRejected := make(map[string]struct{}, len(*op.Raw))

	for _, s := range *op.Raw {
		if _, ok := allowSet[s]; ok {
			if _, dup := seenAccepted[s]; dup {
				continue
			}
			seenAccepted[s] = struct{}{}
			accepted = append(accepted, s)
			continue
		}
		if _, dup := seenRejected[s]; dup {
			continue
		}
		seenRejected[s] = struct{}{}
		rejected = append(rejected, s)
	}

	op.Accepted = accepted
	op.Rejected = rejected

	slog.DebugContext(ctx, "ValidateCitationsOp.run",
		"run_id", dagor.RunID(ctx),
		"raw_count", len(*op.Raw),
		"allowed_count", len(allowSet),
		"accepted_count", len(accepted),
		"rejected_count", len(rejected),
	)
	return nil
}

func init() {
	operator.RegisterOp[ValidateCitationsOp]()
}
