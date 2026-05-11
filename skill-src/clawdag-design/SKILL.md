---
name: clawdag-design
description: Design a maximally deterministic clawdag-go DAG workflow
version: 0.1.0
library_version: github.com/akennis/clawdag-go v0.1.0
triggers: [clawdag design, design dag workflow, dag workflow design]
input:
  task: {type: string, description: "Task description", required: true}
---

# Context

You are designing a DAG workflow using the clawdag-go library. Your goal is a maximally
deterministic design: every step that can be a library op or custom deterministic Go op MUST be.
AI calls are reserved for genuine natural-language parsing or subjective judgment where no
deterministic alternative exists.

Read the following references before producing any output:
1. `references/library.md` — all 89 op descriptions grouped by category
2. `references/design-rules.md` — design constraints, anti-patterns, and required patterns
3. `references/examples/README.md` — pick the most structurally similar example
4. Read that example file in `references/examples/`

# Example selection guide

| Workflow pattern | Example |
|---|---|
| Free-form text → fixed categories → per-lane extraction → coalesce | `ticket-triager.go` |
| Parse fields + deterministic numeric scoring | `recipe-analyzer.go` |
| Parallel HTTP fetch + status-code fallback + multi-probe scoring | `readme-quality.go` |
| Parsed data + threshold routing + conditional warning suffix | `weather-advisor.go` |
| Runtime slice → MapOver fan-out → per-item sub-graph → aggregation | `hn-topic-brief.go` |
| Two AI models in series — Claude generates, Gemini independently verifies | `faithful-summary.go` |
| Strict parse/validate op + AI-driven minimal-mutation retry on bad input (`WithRepair`) | `with-repair.go` |

# AI recovery wrapper (WithRepair) placement

WithRepair is most suitable at the **upstream boundary** of the DAG — wrap the op
that first ingests outside input (user text, fetched payloads, untrusted JSON,
third-party API responses) so the workflow validates and, if necessary, repairs
that input before anything downstream depends on it. Once a value has passed a
WithRepair stage, downstream vertices can treat it as well-formed and skip
defensive re-parsing.

**Do not** wrap an AI op (`AIComputeOp` and its embedders) with `WithRepair` to
validate its output. AI ops support in-conversation self-repair: have the `Out`
type's `ParseAIResponse` return `*library.ErrRepairable` on a fixable miss, and
the op will append a follow-up turn in the same LLM conversation rather than
opening a fresh repair call.

When an AI op is self-validating, the design's **AI Ops Used** entry MUST spell
out the validation rules — codegen translates each rule into one
`*library.ErrRepairable` return in `ParseAIResponse`. Examples:

- `score (AIScoreOp, self-repair: must be float in [0, 1])`
- `category (AICategoryOp, self-repair: must be one of {bug, feature, question})`
- `summary (AISummaryOp, self-repair: must be wrapped in <summary>…</summary>)`

Do not add a separate `[AI:WithRepair]` vertex for any of these.

# AIClientFactory params (optional — enterprise credential routing)

Every AI op sources its SDK client from a `library.AIClientFactory`. The default
(`library.EnvAIClientFactory`) reads `CLAUDE_API_KEY` / `GEMINI_API_KEY` from the
process environment. Two optional vertex params let a workflow opt into a
different credential source:

- `credential_ref` — opaque string forwarded to the configured factory (Vault
  path, tenant id, region, anything the implementation maps onto a credential).
- `client_factory_id` — selects a named factory registered in `main()`;
  vertices that omit it fall back to the process-wide default.

Include these params in the design **only** when the task explicitly involves:

- Multi-tenant routing where different vertices need different credentials.
- Non-env credential sources (Vault, AWS Secrets Manager, GCP Secret Manager,
  Azure Key Vault, workload identity, egress proxy).
- Per-vertex credential rotation policy.

Single-tenant workflows that "just need to call Claude" must NOT mention these
params — leave the default factory in place. Adding them speculatively forces
codegen to write unnecessary registration plumbing in `main()`.

When relevant, list them in the vertex's **Params** line alongside `provider` /
`model`, and note in **Design Rationale** which `main()`-side wiring is required
(one of `SetDefaultAIClientFactory` for a process-wide swap or
`RegisterAIClientFactory("<id>", …)` for each named factory).

Example vertex line for a multi-tenant design:

```
3. **classify_tenant_a** — `AIBoolOp` — Params: predicate="is this in English?", client_factory_id="tenant-a", credential_ref="secret/tenant-a/anthropic"
   - In: Input ← `text_a`
   - Out: Result → `is_english_a`
```

# Steps

1. Read `references/library.md` and identify every op that is relevant to the task.
2. Read `references/design-rules.md` fully — especially the BRANCHING and BOOLEAN SELECTION sections.
3. Select the structurally closest example from `references/examples/README.md` and read it.
4. Draft a complete DAG design in the output format below.
5. Present the design to the user. Ask: "Does this design look right? Any changes before I hand it to codegen?"
6. If the user provides feedback, incorporate it and redraft. Repeat until explicit approval.
7. The final approved design is the output — do not proceed to code generation.

# Refinement loop

After presenting a design, wait for user feedback. Refine based on feedback and re-present.
Only mark the design as approved when the user explicitly says so (e.g. "looks good", "approved", "yes").

# Output format

Respond ONLY with the following structured document. No Go code. No markdown outside this format.

## Workflow: [short name]

### ASCII DAG
[diagram showing vertices and data flow with → arrows; vertices wrapped by
`library.WithRepair` carry a trailing `[AI:WithRepair]` tag — see
"AI-WRAPPED VERTICES — RENDERER HINT" in `references/design-rules.md`]

### Vertices
List each vertex in topological order:
N. **vertex_name** — `OpName` — [Condition: pred_name] — Params: key=value, ...
   - Wrapper: `WithRepair` (input_field=FieldName, max_attempts=N)   — only when WithRepair-wrapped
   - In: FieldName ← `wire_name`
   - Out: FieldName → `wire_name`

For map vertices (no Op), use this format instead:
N. **vertex_name** — `[MAP]` — item_wire: `item`
   - In: Items ← `slice_wire`
   - Sub-graph:
     N.a. **sub_vertex** — `OpName`
          - In: FieldName ← `item` (or intermediate wire)
          - Out: FieldName → `wire_name`
   - CollectInto: `result_wire` → `output_wire` ([]any)

For MCP vertices (`MCPCallOp`, `MCPScriptOp`, or concrete variants thereof), the `transport`
Param selects how the server is reached:
- `transport: "stdio"` (default — back-compat) requires `command` and accepts optional `args` /
  `env` (CSV `KEY=VALUE`).
- `transport: "http"` requires `url` and accepts optional `headers` (CSV `KEY=VALUE` for
  static auth — e.g. `Authorization=Bearer ${TOKEN}`).

`pool_size: N` is a valid optional Param indicating the warm-replenish pool capacity. **Pooling
is only supported for `transport: "stdio"` in v1** — Setup rejects `pool_size > 0` when
`transport: "http"` because remote sessions can be killed by server-side idle timeouts and
static `headers` tokens may expire while sessions sit warm. Include `pool_size` for stdio
vertices that sit in a fan-out (MapOver sub-graph) or otherwise run repeatedly with the same
spec, since subprocess cold-start cost (launch, MCP handshake, browser/server init) is
otherwise paid every Run.

The default MCP Out dispatch handles `string`, `float64`, `int`, `bool`, `[]string`,
`[]float64`, `[]int`, `map[string]string`, and any struct decodable via `json.Unmarshal`.
When the tool's argument schema doesn't fit the natural JSON shape of the In struct, or the
response can't be decoded by the default dispatch, flag this in **Custom Ops Needed**: the
In type will implement `MCPArgsFormatter` (`FormatMCPArgs`) and/or the Out type will
implement `MCPResponseParser` (`ParseMCPResponse`). For `MCPScriptOp` scripts that need to
recover from anticipated tool errors (e.g. element-not-found on a click), note that the
script `errors.As`-checks `*MCPToolError`.

### Predicates
- `pred_name`: which wire it reads, what value triggers it

### Custom Ops Needed
For each op not found in the library:
- **OpName**: inputs (name: type), outputs (name: type), what Run() must compute

### AI Ops Used
For each AI op in the design:
- **vertex_name** (`OpName`): the `operation` param text — phrase it so it
  unambiguously identifies the task. Pair it with the validation rules listed
  above (for self-validating ops) so the codegen step can write an
  `ExpectedFormat()` precise enough that parsing succeeds on the first turn.

### Design Rationale
Key decisions: why certain operations are deterministic vs AI, any tradeoffs
