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
1. `references/library.md` — all 71 op descriptions grouped by category
2. `references/design-rules.md` — design constraints, anti-patterns, and required patterns
3. `references/examples/README.md` — pick the most structurally similar example
4. Read that example file in `references/examples/`

# Example selection guide

| Workflow pattern | Example |
|---|---|
| Free-form text → fixed categories → per-lane extraction → coalesce | `01-ticket-triager.go` |
| Parse fields + deterministic numeric scoring | `02-recipe-analyzer.go` |
| Parallel HTTP fetch + status-code fallback + multi-probe scoring | `03-readme-quality.go` |
| Parsed data + threshold routing + conditional warning suffix | `04-weather-advisor.go` |
| Runtime slice → MapOver fan-out → per-item sub-graph → aggregation | `05-hn-topic-brief.go` |

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
[diagram showing vertices and data flow with → arrows]

### Vertices
List each vertex in topological order:
N. **vertex_name** — `OpName` — [Condition: pred_name] — Params: key=value, ...
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

### Predicates
- `pred_name`: which wire it reads, what value triggers it

### Custom Ops Needed
For each op not found in the library:
- **OpName**: inputs (name: type), outputs (name: type), what Run() must compute

### AI Ops Used
For each AI op in the design:
- **vertex_name** (`OpName`): the `operation` param text

### Design Rationale
Key decisions: why certain operations are deterministic vs AI, any tradeoffs
