# TODO: Expand the deterministic library

Goal: push as much workflow plumbing as possible into deterministic ops in
`library/`, so the AI-powered ops are reserved for genuine semantic / judgment
gaps (classify, score, summarize, rerank, free-text extraction).

Each group below is self-contained and intended to be executed by one
sub-agent starting with an empty context window. Read the **Shared conventions**
section first, then jump to a group.

---

## Shared conventions (read before any group)

**Repo:** `/mnt/c/Users/albert.kennis/projects/clawdag-go`
**Library package:** `library/` (package name `library`)
**Description:** the project is a DAG-based AI codegen system. The driver
prompts an LLM with a list of registered ops (each described by a
`<OpName>Description` string constant); the LLM wires them into a `dagor`
graph. Every deterministic op we add directly shrinks the AI surface.

**Op contract** — every op must:

1. Be a struct in `library/<topic>_ops.go`.
2. Implement `Setup(*config.Params) error`, `Reset() error`,
   `Run(context.Context) error`.
3. Register itself in an `init()` via `operator.RegisterOp[OpType]()`.
4. Expose a `<OpName>Description` string constant whose body lists Params,
   Inputs, and Outputs in the same style as existing ops (e.g. see
   `library/math_ops.go:121-126`).

**Two op styles in this repo:**

- **Tag-based** — fields marked `dag:"input"` / `dag:"output"`. Boilerplate
  (`InputFields`, `OutputFields`, `SetInputField`, `ResetFields`) is generated
  by `daggen`. Add a `//go:generate daggen -type=<Op> -output=<file>_gen.go`
  line to `library/gen.go` and run `go generate ./library/...`. Tag-based
  works for scalar pointer inputs/outputs (`*float64`, `*string`, `*bool`,
  `*int`) and scalar outputs.
- **Hand-rolled** — used when fields are slice/map types. See `SumOp` in
  `library/math_ops.go:176-205` and `BoolAndOp` in `library/bool_ops.go:43-79`
  for the template. Implement all four IOperator methods inline; do not add
  to `library/gen.go`.

**Build & verify after each group:**

```bash
go generate ./library/...   # only if you added tag-based ops
go build ./...
```

Add a unit test where deterministic behavior is non-trivial (parsing,
edge cases). Tests live in `library/*_test.go`; no `CLAUDE_API_KEY` required
for purely deterministic ops. Run `go test ./library/... -short`.

**Naming:** `<Topic><Verb>Op` (e.g. `StringTrimOp`, `MapKeysOp`,
`IfFloatGtOp`). Boolean-output predicates start with `If` for compatibility
with `dagor` `Condition(...)` wiring (see `llm-hints.md`).

**Do not** add features beyond what each group lists. Do not refactor
existing ops unless that group's notes say to. Do not add comments unless the
why is non-obvious.

---

## Group 1 — Comparison / predicate ops ✅ DONE

**Why:** dagor's conditional branching uses named predicates that read op
outputs (see `llm-hints.md` lines 49-92). Today only `IfStringEqOp`
(`library/routing_ops.go`) exists, so the LLM has to reach for `AIBoolOp` for
trivial decisions like `x > threshold`.

**Add to `library/routing_ops.go`** (or a new `library/predicate_ops.go` if
you prefer — match whichever existing file's style most closely):

Float comparisons (Inputs `A *float64`, `B *float64`, Output `Match bool`):
- `IfFloatGtOp`, `IfFloatLtOp`, `IfFloatEqOp`, `IfFloatGeOp`, `IfFloatLeOp`

Int comparisons (Inputs `A *int`, `B *int`, Output `Match bool`):
- `IfIntGtOp`, `IfIntLtOp`, `IfIntEqOp`, `IfIntGeOp`, `IfIntLeOp`

String predicates (Inputs `A *string`, `B *string`, Output `Match bool`):
- `IfStringContainsOp` — A contains B as a substring.
- `IfStringHasPrefixOp` — A starts with B.
- `IfStringHasSuffixOp` — A ends with B.
- `IfStringRegexMatchOp` — Input `Input *string`, Param `pattern` (required,
  compiled in `Setup`), Output `Match bool`. Mirror `RegexMatchOp` in
  `library/string_ops.go:148-185`.

Emptiness / nil checks:
- `IfEmptyStringOp` (Input `Value *string` → `Match bool`; true when nil or `""`).
- `IfEmptySliceStringOp` (Input `Value *[]string` → `Match bool`).
- `IfEmptySliceFloat64Op` (Input `Value *[]float64` → `Match bool`).

Range:
- `BetweenFloatOp` (Inputs `Value *float64`, `Min *float64`, `Max *float64` →
  `Match bool`; inclusive on both ends).

**Style:** scalar-pointer inputs/outputs work with tag-based generation (add
`//go:generate` lines to `library/gen.go`). The slice-input ones must be
hand-rolled. Each op needs its `<OpName>Description` constant.

**Verify:** `go build ./...` plus a small `library/predicate_ops_test.go`
covering one true case and one false case per op (table-driven is fine).

---

## Group 2 — Coalesce, switch, select, default ✅ DONE

**Why:** `llm-hints.md:85` already tells the LLM to use `CoalesceIntOp`, but
no Coalesce op exists. Without coalesce, conditional branches can't merge
(see the merge example in `llm-hints.md:62-91`). Switch/select/default
eliminates AI calls for trivial value-routing.

**Generic Coalesce** — implement in a new file
`library/coalesce_op.go` modeled on `library/ai_compute_op.go:69-124`:

```go
// CoalesceOp picks the first non-nil input among A, B, C, D.
type CoalesceOp[T any] struct {
    A, B, C, D *T
    Result     T
}
```

- `Run` returns the first non-nil pointer's value; if all nil, return an
  error `"CoalesceOp: all inputs nil"`.
- Implement IOperator methods inline (generics + tags don't mix cleanly).
- Register concrete variants in `init()`:
  - `CoalesceStringOp` (`CoalesceOp[string]`)
  - `CoalesceFloat64Op` (`CoalesceOp[float64]`)
  - `CoalesceIntOp` (`CoalesceOp[int]`)
  - `CoalesceBoolOp` (`CoalesceOp[bool]`)
- Each variant gets its own `<OpName>Description` constant.

**Update `llm-hints.md` line 85 / 101** so the referenced `CoalesceIntOp`
matches what we ship (currently it's described as a built-in but is missing).
Change the example merge node's op name only if our chosen name differs.

**Add to a new `library/select_ops.go`:**

- `SelectStringOp` (Inputs `Cond *bool`, `IfTrue *string`, `IfFalse *string`
  → `Result string`). Ternary.
- `SelectFloat64Op`, `SelectIntOp`, `SelectBoolOp` — same shape, swap types.
- `SwitchStringOp` — Input `Key *string`, Param `cases` (JSON `{"k":"v",...}`),
  Param `default` → `Result string`. Lookup `Key`; on miss, return
  `default`.
- `DefaultStringOp` (Inputs `Value *string`, `Default *string` → `Result string`;
  returns `Default` when `Value` is nil or empty).
- `DefaultFloat64Op` (treats `nil` as missing only — zero is a valid value).
- `DefaultIntOp` (same — nil is missing).

**Style:** Coalesce variants are hand-rolled. Select/Default scalar variants
work as tag-based — add to `library/gen.go`. Switch is hand-rolled (param
parsing in `Setup`, see `StringLookupOp` in `library/string_ops.go:46-72`).

**Verify:** `go generate ./library/... && go build ./...`. Add
`library/coalesce_op_test.go` covering all-nil, first-set, third-set cases
for one variant.

---

## Group 3 — Type conversion ops

**Why:** Today, bridging text→numeric forces `AIParseNumberOp`. A pure
`ParseFloatOp` covers the deterministic majority; AI is reserved for
free-form ("two thousand", "$1.2k") inputs.

**Add to a new `library/convert_ops.go`:**

Parsers (return error on bad parse):
- `ParseFloatOp` (Input `Input *string` → `Result float64`).
- `ParseIntOp` (Input `Input *string` → `Result int`).
- `ParseBoolOp` (Input `Input *string` → `Result bool`; accept
  `true/false/yes/no/1/0` case-insensitive).

Formatters:
- `FormatFloatOp` (Input `Value *float64`, Param `precision` int default `-1`
  → `Result string`; use `strconv.FormatFloat(... 'f', precision, 64)`).
- `FormatIntOp` (Input `Value *int` → `Result string`).
- `BoolToStringOp` (Input `Value *bool` → `Result string`; `"true"`/`"false"`).

Numeric casts:
- `IntToFloatOp` (Input `Value *int` → `Result float64`).
- `FloatToIntOp` (Input `Value *float64`, Param `mode` ∈ `{"trunc","round","floor","ceil"}`
  default `"trunc"` → `Result int`).

> **Cross-ref:** `examples/02-recipe-analyzer/main.go` currently inlines a
> private `IntToFloatOp` because `SliceLenOp` returns `*int` while
> `MulOp`/`AddOp` consume `*float64`. When this group lands, drop the inline
> op from that example and wire the library op instead.

**Style:** all scalar — tag-based. Add `//go:generate` lines to
`library/gen.go`. Each op needs a description constant.

**Verify:** `go generate ./library/... && go build ./...` plus a
`library/convert_ops_test.go` covering one happy-path and one error case per
parser, and the four `FloatToIntOp` modes.

---

## Group 4 — Core string ops

**Why:** Today only `Concat`, `ToLower`, `Split`, `RegexExtract`, `RegexMatch`
exist. Missing the everyday primitives forces the LLM into either generated
glue code or AI calls.

**Add to `library/string_ops.go`:**

Scalar (tag-based, add to `library/gen.go`):
- `StringToUpperOp` (Input `Value *string` → `Result string`).
- `StringTrimSpaceOp` (Input `Value *string` → `Result string`).
- `StringTrimOp` (Input `Value *string`, Param `cutset` → `Result string`).
- `StringTrimPrefixOp` (Input `Value *string`, Param `prefix` → `Result string`).
- `StringTrimSuffixOp` (Input `Value *string`, Param `suffix` → `Result string`).
- `StringReplaceAllOp` (Input `Value *string`, Params `old`, `new` → `Result string`).
- `StringReplaceOp` (Input `Value *string`, Params `old`, `new`, `n` int default `-1`
  → `Result string`).
- `StringContainsOp` (Inputs `A *string`, `B *string` → `Result bool`).
- `StringLenOp` (Input `Value *string` → `Result int`; rune count, not byte length).
- `StringSubstringOp` (Input `Value *string`, Params `start` int default `0`,
  `end` int default `-1` (means "to end") → `Result string`; rune-indexed,
  clamped to bounds, no panic on out-of-range).
- `RegexReplaceOp` (Input `Input *string`, Params `pattern` required,
  `repl` default `""` → `Result string`; compile in `Setup`).

Slice-output (hand-rolled):
- `RegexFindAllOp` (Input `Input *string`, Param `pattern` required,
  Param `n` int default `-1` → `Result []string`).

Format:
- `StringFormatOp` (Inputs `Args *[]string`, Param `template` required like
  `"hello %s, you are %s"` → `Result string`. Use `fmt.Sprintf` with
  `args` re-typed to `[]any`. Hand-rolled.).

**Verify:** `go generate ./library/... && go build ./...` and add
`library/string_ops_extra_test.go` (or extend the existing test file if any)
covering trim variants, substring bounds, regex replace, and format.

---

## Group 5 — Scalar math ops

**Why:** Today only `Add/Sub/Mul/Div/Round/Clamp` are present, so anything
else routes through `AIComputeMathOperandsToFloat64Op`.

**Add to `library/math_ops.go` (all tag-based, add `//go:generate` lines):**

Unary (Input `Value *float64` → `Result float64`):
- `AbsOp`, `NegOp`, `SqrtOp`, `FloorOp`, `CeilOp`, `ExpOp`.

With param:
- `LogOp` (Input `Value *float64`, Param `base` float default `math.E` →
  `Result float64`; error on `Value <= 0` or `base <= 0` or `base == 1`).
- `PowOp` (Inputs `Base *float64`, `Exp *float64` → `Result float64`).
- `ModOp` (Inputs `A *float64`, `B *float64` → `Result float64`; use
  `math.Mod`; error on `B == 0`).

Comparison helper:
- `CompareFloatOp` (Inputs `A *float64`, `B *float64` → `Result int`;
  returns `-1`, `0`, or `1`).

**Verify:** `go generate ./library/... && go build ./...` and add
small unit tests for `LogOp` (base=10 vs base=e), `ModOp` (zero divisor),
`PowOp`, `CompareFloatOp`.

---

## Group 6 — Slice statistics, sort, transforms, typed parallels

**Why:** Today aggregations exist only for `[]float64` (Sum/Min/Max), and
all slice manipulation is `[]string`-only.

**Add to `library/slice_ops.go` (all hand-rolled — slice fields):**

`[]float64` aggregations (Input `Values *[]float64` → `Result float64`):
- `AvgOp` (alias `MeanOp` not needed — just `AvgOp`). Error on empty.
- `MedianOp`. Error on empty.
- `StdDevOp` (population stddev). Error on empty.
- `ProductOp`.

Counts (Input `Values *[]float64` → `Result int`):
- `CountFloat64Op` (length of `[]float64`).
- `CountStringOp` is already covered by `SliceLenOp`; do not duplicate.

Sort / reverse / distinct / range / concat:
- `SortAscFloat64Op`, `SortDescFloat64Op` (Input/Output `*[]float64` /
  `[]float64`).
- `SortAscStringOp`, `SortDescStringOp`.
- `SortAscIntOp`, `SortDescIntOp`.
- `ReverseStringOp`, `ReverseFloat64Op`, `ReverseIntOp`.
- `DistinctStringOp`, `DistinctFloat64Op`, `DistinctIntOp` (preserve order
  of first occurrence).
- `SliceRangeStringOp`, `SliceRangeFloat64Op`, `SliceRangeIntOp` (Inputs
  `Input *[]T`, Params `start`, `end` int with `-1` meaning "to end";
  clamped, no panic).
- `SliceConcatStringOp`, `SliceConcatFloat64Op`, `SliceConcatIntOp` (Inputs
  `A *[]T`, `B *[]T` → `Result []T`).

Typed parallels of existing string-slice ops (skip if already present):
- `SliceLenFloat64Op`, `SliceLenIntOp`.
- `SliceAtFloat64Op`, `SliceAtIntOp`.
- `SliceFirstFloat64Op`, `SliceFirstIntOp`.
- `SliceLastFloat64Op`, `SliceLastIntOp`.
- `SliceContainsFloat64Op`, `SliceContainsIntOp`.

Pairing helper (for use with `AIExtractMapOp`-style flows):
- `ZipOp` (Inputs `Keys *[]string`, `Values *[]string` → `Result []string`
  of `"k=v"` pairs; error if lengths differ).

**Style:** all hand-rolled. Each op needs a `<OpName>Description` constant.
Reuse the boilerplate pattern from existing `SumOp` / `MinOp` / `MaxOp`
exactly.

**Verify:** `go build ./...` and a `library/slice_ops_extra_test.go` covering
empty-input errors for the aggregations, sort stability for one type, and
distinct-preserves-order.

---

## Group 7 — Map ops (whole new category)

**Why:** No map ops exist. Every map workflow today routes through generated
glue or `AIExtractMapOp`.

**Create `library/map_ops.go` (all hand-rolled):**

- `MapGetOp` — Inputs `Map *map[string]string`, `Key *string` → `Result string`
  (empty if missing, no error). This generalizes the params-driven
  `StringLookupOp`.
- `MapHasKeyOp` — Inputs `Map *map[string]string`, `Key *string` → `Result bool`.
- `MapKeysOp` — Input `Map *map[string]string` → `Result []string` (sorted).
- `MapValuesOp` — Input `Map *map[string]string` → `Result []string` (sorted
  by key for determinism).
- `MapSizeOp` — Input `Map *map[string]string` → `Result int`.
- `MapMergeOp` — Inputs `A *map[string]string`, `B *map[string]string` →
  `Result map[string]string` (B wins on key conflict). Output is
  `map[string]string` — needs `OutputFields` returning `&op.Result`.
- `MapToJSONOp` — Input `Map *map[string]string` → `Result string`
  (deterministic key ordering — `encoding/json` sorts map keys, so a plain
  `json.Marshal` is fine).
- `JSONToMapOp` — Input `JSON *string` → `Result map[string]string`. Error
  on parse failure or non-string values.

Each op needs a description constant. Register all in `init()`. Output of
`map[string]string` is unusual — model `OutputFields` after the map case in
`AIExtractMapOp` (`library/ai_ops.go:30-39`).

**Verify:** `go build ./...` and `library/map_ops_test.go` covering deterministic
key ordering for `MapKeysOp` / `MapValuesOp` / `MapToJSONOp`, merge precedence
for `MapMergeOp`, parse failure for `JSONToMapOp`.

---

## Group 8 — JSON ops (parse / stringify / array)

**Why:** Today only `JSONExtractOp` (dot-path) exists. Anything beyond a
single scalar lookup forces an AI call.

**Add to `library/json_ops.go`:**

- `JSONStringifyOp` — Input `Value *string` (already JSON), Param `pretty`
  bool default `false` → `Result string`. If `pretty`, re-marshal with
  `MarshalIndent("", "  ")`. Error on invalid JSON.
- `JSONParseValidateOp` — Input `JSON *string` → `Result bool` (true if
  parses cleanly). Use this instead of erroring so DAGs can branch.
- `JSONArrayLengthOp` — Input `JSON *string` → `Result int`. Error if
  top-level is not a JSON array.
- `JSONArrayAtOp` — Input `JSON *string`, Param `index` int → `Result string`
  (the element re-encoded as JSON; strings are returned as their literal
  string value to mirror `JSONExtractOp`'s convention at
  `library/json_ops.go:48-54`).
- `JSONMergeOp` — Inputs `A *string`, `B *string` → `Result string`.
  Top-level deep merge of two JSON objects (B wins on conflict; arrays are
  replaced not concatenated). Error if either side is not a JSON object.

**Style:** all scalar inputs/outputs, tag-based. Add `//go:generate` lines.
Each op needs a description.

**Verify:** `go generate ./library/... && go build ./...` and
`library/json_ops_extra_test.go` covering: stringify preserves equivalent
JSON, array length on object errors, merge precedence, deep merge nesting.

---

## Group 9 — General-purpose time ops

**Why:** `CityTimeOp` is intentionally a 2-city demo (NY/Tokyo only — see
`library/time_ops.go:17-20`). Real workflows need general time
parsing/formatting/arithmetic.

**Add to `library/time_ops.go` (do not modify or remove `CityTimeOp`):**

- `NowOp` — no inputs, Param `format` string default `time.RFC3339` →
  `Result string`. Use UTC.
- `ParseTimeOp` — Input `Input *string`, Param `layout` default
  `time.RFC3339` → `Result string` (re-emit normalized RFC3339 UTC).
  Error on parse failure.
- `FormatTimeOp` — Input `Input *string` (RFC3339), Param `layout` default
  `time.RFC3339` → `Result string`.
- `TimeInZoneOp` — Input `Input *string` (RFC3339), Param `tz` (IANA name,
  required) → `Result string` (RFC3339 in that zone).
- `TimeAddOp` — Input `Input *string` (RFC3339), Param `duration` (e.g.
  `"24h"`, parsed by `time.ParseDuration`, required) → `Result string`.
- `TimeDiffSecondsOp` — Inputs `A *string`, `B *string` (both RFC3339) →
  `Result float64` (seconds, A−B).
- `IfTimeBeforeOp` — Inputs `A *string`, `B *string` → `Match bool` (A < B).
- `IfTimeAfterOp` — Inputs `A *string`, `B *string` → `Match bool` (A > B).
- `UnixTimestampOp` — Input `Input *string` (RFC3339) → `Result int` (Unix
  seconds).

**Style:** all scalar — tag-based. Add `//go:generate` lines. Each op needs
a description constant.

**Verify:** `go generate ./library/... && go build ./...` and a focused
`library/time_ops_extra_test.go` covering: `NowOp` returns parseable
RFC3339; `TimeAddOp` with a 1h duration; `TimeDiffSecondsOp` sign;
`TimeInZoneOp` with a real tz; error on bad input.

---

## Group 10 — Encoding & hashing

**Why:** Common building blocks for any workflow that touches HTTP, files,
or IDs. Today each one would require generated glue.

**Create `library/encoding_ops.go` (all scalar, tag-based; add to
`library/gen.go`):**

- `Base64EncodeOp` (Input `Value *string` → `Result string`).
- `Base64DecodeOp` (Input `Value *string` → `Result string`; error on bad input).
- `URLEncodeOp` (Input `Value *string` → `Result string`; use
  `url.QueryEscape`).
- `URLDecodeOp` (Input `Value *string` → `Result string`; use
  `url.QueryUnescape`).
- `HexEncodeOp` (Input `Value *string` → `Result string`).
- `HexDecodeOp` (Input `Value *string` → `Result string`).

**Create `library/hash_ops.go`:**

- `MD5Op` (Input `Value *string` → `Result string`; lowercase hex).
- `SHA256Op` (Input `Value *string` → `Result string`; lowercase hex).
- `UUIDOp` — no inputs, no params, → `Result string` (v4). Add the
  `github.com/google/uuid` dependency only if not already present; otherwise
  use `crypto/rand` to produce a v4-shaped UUID inline.

Each op needs a description. Register all in `init()`.

**Verify:** `go generate ./library/... && go build ./...` plus
`library/encoding_ops_test.go` covering round-trip Base64/URL/Hex and pinned
test vectors for MD5/SHA256 (e.g. SHA256 of `"abc"`).

---

## Group 11 — IO ops

**Why:** Today only `FileReadOp`, `EnvOp`, `HTTPGetOp`. Workflows that need
to write files or do an HTTP POST currently can't.

**Add to `library/io_ops.go`:**

- `FileWriteOp` — Inputs `Path *string`, `Content *string`, Param `mode`
  string default `"0644"` → `Result string` (the path written). Create
  parent dirs with `0755` if missing.
- `FileExistsOp` — Input `Path *string` → `Result bool`. Use `os.Stat`;
  treat `os.IsNotExist` as `false`, propagate other errors.
- `ListDirOp` — Input `Path *string` → `Result []string` (filenames only,
  no recursion). Hand-rolled (slice output).
- `HTTPPostOp` — Inputs `URL *string`, `Body *string`, Param `content_type`
  default `"application/json"`, Param `headers` JSON object default `{}`
  → `Body string`, `StatusCode int`. Mirror `HTTPGetOp` boilerplate at
  `library/io_ops.go:49-91` for hand-rolled IOperator methods.
- `DownloadFileOp` — Inputs `URL *string`, `Path *string` → `Result string`
  (the path written). Stream to file; error on non-2xx.

**Style:** scalar-only ops are tag-based (add to `library/gen.go`).
Slice-output and multi-output ops are hand-rolled.

**Verify:** `go generate ./library/... && go build ./...`. Skip live HTTP
tests; for `FileWriteOp` / `FileExistsOp` / `ListDirOp` use `t.TempDir()` in
`library/io_ops_extra_test.go`.

---

## Group 12 — Templating, CSV, vector

**Why:** Three independent areas, each small enough that one sub-agent can
do all three. They're collected here because none warrants its own group.

**Templating — add to a new `library/template_ops.go`:**

- `TemplateRenderOp` — Input `Data *map[string]string`, Param `template`
  required (Go `text/template` syntax) → `Result string`. Compile template
  in `Setup`. Hand-rolled.

**CSV — add to a new `library/csv_ops.go`:**

- `CSVParseOp` — Input `Input *string`, Param `comma` default `","` →
  `Result string` (re-emitted as JSON `[[...],...]` for downstream chaining).
  Use `encoding/csv`.
- `CSVStringifyOp` — Input `Input *string` (JSON-encoded `[][]string`),
  Param `comma` default `","` → `Result string`.
- `CSVColumnOp` — Input `Input *string` (JSON-encoded `[][]string`), Param
  `index` int (column index, 0-based), Param `skip_header` bool default
  `false` → `Result []string`. Hand-rolled (slice output).

**Vector — add to a new `library/vector_ops.go`:**

- `VectorDotOp` — Inputs `A *[]float64`, `B *[]float64` → `Result float64`.
  Error if lengths differ.
- `VectorCosineOp` — Inputs `A *[]float64`, `B *[]float64` → `Result float64`.
  Error on length mismatch or zero norm.
- `VectorL2DistanceOp` — Inputs `A *[]float64`, `B *[]float64` →
  `Result float64`.
- `VectorNormalizeOp` — Input `A *[]float64` → `Result []float64`. Error
  on zero norm.

All vector ops are hand-rolled (slice fields). Each op everywhere needs a
description constant.

**Verify:** `go generate ./library/... && go build ./...` plus a small
test per file (one happy path each).

---

## Group 13 — Structural cleanup

**Why:** Tightens the foundations once the new ops have landed. Run this
group **after** at least Groups 1–4 are in.

1. **Fix `llm-hints.md` references.** Lines 85 and 101 mention
   `CoalesceIntOp` as "built-in"; once Group 2 lands, this is true. If the
   chosen op name differs from `CoalesceIntOp`, update the example
   accordingly.

2. **Audit `daggen` for slice/pointer types.** Today every slice-input op
   in this repo (Sum/Min/Max, all of `bool_ops.go`, all of `slice_ops.go`,
   `IfStringEqOp`) is hand-rolled. Investigate whether `daggen` (in the
   sibling repo at `/mnt/c/Users/albert.kennis/projects/dagor` per
   memory note `reference_dagor_source.md` — actually `daggen` itself, find
   its source) can already handle `*[]T` / `*map[K]V` fields. If yes,
   migrate the hand-rolled ops file by file. If no, file it as a future
   enhancement and stop. Do not migrate ops one-off without first confirming
   `daggen`'s capabilities.

3. **Reconsider `AIComputeMathOperandsToFloat64Op` registration.** With
   Groups 1, 5 landed, this op should no longer be the first reach for the
   LLM. Decide one of:
   - Keep it registered (current behavior), but tighten its description to
     "fallback only — prefer `MulOp`, `PowOp`, `ModOp`, etc. when the
     operation is named in the library".
   - Leave the type registered but remove its description constant from the
     scan output so the LLM doesn't see it. Requires checking how
     `LibraryScanOp` discovers descriptions (see `driver_ops.go`).

4. **Verify `LibraryScanOp` picks up every new description constant.** Read
   `driver_ops.go` to confirm the discovery mechanism (likely reflection or
   a registry). If the new ops aren't auto-discovered, add the missing
   wiring; do not duplicate descriptions by hand.

5. **Fix the silent `CoalesceStringOp` (and siblings) name clash + harden
   `RegisterOp` against silent duplicates.**

   **Symptom.** Building a graph that uses `CoalesceStringOp` with four
   inputs (A, B, C, D) — exactly as documented in
   `library/coalesce_op.go:108-130` — fails at runtime with
   `set input field D error: CoalesceOp: unknown field "D"`. The same
   applies to `CoalesceFloat64Op`, `CoalesceIntOp`, `CoalesceBoolOp`.

   **Root cause.** Two implementations are registered under each of those
   four names:
   - `dagor/operator/builtin/coalesce_op.go:36-95` — generic
     `CoalesceOp[T]` with **2 inputs (A, B)**, registered for
     string/int/float64/bool.
   - `library/coalesce_op.go:20-87` — generic `CoalesceOp[T]` with
     **4 inputs (A, B, C, D)**, registered for the same four types.

   `operator.RegisterOp` (see
   `dagor/operator/registry.go:34`) returns
   `"operator pool already registered for name: …"` on duplicate names, but
   every call site in the repo (`library/coalesce_op.go:132-137`,
   `library/*.go` `init()` blocks) discards the return value. Because
   `library` transitively imports `dagor/operator/builtin`, Go runs the
   builtin's `init()` first; the library's 4-input registration loses the
   race silently, leaving the 2-input variant resolved by name. The
   library's `Coalesce*Op` types are dead at runtime — the description
   constants the LLM sees do **not** match the dispatched op.

   This was uncovered while wiring `examples/01-ticket-triager`
   (`final` vertex). The example was changed to use `CoalesceNStringOp`
   from the dagor builtin (variable-arity) as a workaround; revisit that
   workaround once this item lands.

   **Fix.** Pick **one** of the following. (a) is recommended.

   (a) **Delete `library/coalesce_op.go` outright.** The dagor builtin
       already ships a 2-input `Coalesce<T>Op` (A/B) and an N-input
       `CoalesceN<T>Op` (`Input0…Input(n-1)`, configured via `params.n`)
       for the same four element types. The library variant adds nothing
       except a hard-coded arity of 4. Plan:
       - Remove `library/coalesce_op.go` and `library/coalesce_op_test.go`.
       - Remove the `CoalesceStringOpDescription` /
         `CoalesceFloat64OpDescription` / `CoalesceIntOpDescription` /
         `CoalesceBoolOpDescription` constants and ensure
         `LibraryScanOp.Run` (`driver_ops.go`) instead surfaces the
         dagor-builtin descriptions (write fresh ones if the builtins
         don't expose them; see Group 13 item 4 for the discovery
         mechanism). Cover both 2-input and N-input variants in the LLM
         prompt with concrete examples for `n=3` and `n=4`.
       - Update `llm-hints.md` (which already names `CoalesceIntOp`) and
         the README's library-table row for `CoalesceStringOp` to point at
         the builtin.
       - Update the workaround in
         `examples/01-ticket-triager/main.go` only if (a) changes the
         recommended op name; otherwise leave it on `CoalesceNStringOp`.

   (b) **Rename the library variants** (e.g. `Coalesce4StringOp`,
       `Coalesce4Float64Op`, …) so both arities coexist. Less appealing
       — the N-input builtin already covers `n=4`.

   (c) **Move the library variants into the dagor builtin** as the
       canonical 4-input form, remove the 2-input form from the builtin,
       and update every existing call site. Highest blast radius — only
       do this if you control both repos and want to canonicalize on
       4-input semantics.

   **Guardrails — required regardless of which fix is chosen.**

   - **Promote duplicate registration from a silent error to a hard
     failure.** Pick the least-invasive of:
     - Change `operator.RegisterOp` (and `RegisterOpFactory`) in
       `dagor/operator/registry.go` to `panic` on duplicate names. The
       error path is structural, not recoverable; every existing caller
       already discards the error so behavior changes only for buggy
       code.
     - Or, less invasive: introduce `operator.MustRegisterOp[T]()` that
       wraps the existing function and panics on error, then migrate
       every `init()` in `library/`, `examples/*/main.go`, and the
       driver to call it. Leave `RegisterOp` as the error-returning form
       for callers that genuinely want to recover.
     Either change must come with a unit test in
     `dagor/operator/registry_test.go` that registers the same name
     twice and asserts the panic / error chain.
   - **Add a duplicate-registration audit test** in
     `library/registry_audit_test.go` that imports `library` and
     `dagor/operator/builtin` (matching how the example binaries import
     them), enumerates every name registered by both, and fails with a
     readable diff on overlap. This catches future silent shadows.
   - **Add a registry round-trip test for every Coalesce variant.** For
     each of `CoalesceStringOp`, `CoalesceFloat64Op`, `CoalesceIntOp`,
     `CoalesceBoolOp` (and their `N`-input siblings if (a) is chosen),
     fetch the op from the registry by name, call `InputFields()`, and
     assert the exact field-name set (`{A,B}` for 2-input,
     `{Input0…Input(n-1)}` for N-input, `{A,B,C,D}` only if (b) is
     chosen). This is the test that would have caught the original bug.
   - **Sweep all `init()` blocks in `library/`, `example/`,
     `examples/`, and `driver_ops.go` and stop discarding
     `RegisterOp`'s return value** — switch them to the panic-on-error
     helper from the first guardrail. Run `grep -rn "operator.RegisterOp" .`
     to enumerate; the count today is in the dozens.
   - **Re-run `examples/01-ticket-triager`** end-to-end against all four
     fixtures (`billing`, `bug`, `feature`, `other`) after the change;
     each lane must still gate, fire its own AI ops, and produce a
     non-empty `final_brief`. Capture stdout JSON in
     `examples/01-ticket-triager/README.md`'s expected-output snippet so
     a future regression on the coalesce path is reviewable in diff.

**Verify:** `go build ./... && go test ./...` and run `go run .` against a
prompt that exercises one of the new ops end-to-end (e.g. a simple
`"compute (a + b) * c, then format with 2 decimals"` workflow that should
now wire `AddOp → MulOp → FormatFloatOp` with no AI fallback). Also re-run
`go run ./examples/01-ticket-triager --ticket
examples/01-ticket-triager/testdata/tickets/billing.txt` (and the other
three fixtures) and confirm the JSON output matches the README's
expected-output snippet.
