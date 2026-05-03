# Example Index

Read the example whose structural pattern most closely matches the workflow you are designing.

| Structural pattern | Example file |
|---|---|
| Multi-lane text classification (free-form input → category → per-lane extraction → coalesce) | `01-ticket-triager.go` |
| Extraction pipeline + deterministic numeric scoring (parse fields → hardcoded scoring → format) | `02-recipe-analyzer.go` |
| Parallel HTTP fetch + status-code fallback + multi-probe scoring (dual GET → select → AI probes) | `03-readme-quality.go` |
| Data parsing + band routing + conditional warning probes (parse → threshold predicates → coalesce + SelectStringOp) | `04-weather-advisor.go` |
| MapOver fan-out + aggregation + routing (slice → map node → per-item sub-graph → collect → summarize) | `05-hn-topic-brief.go` |

## Quick-reference guidance

- **Free-form text → fixed categories → per-lane work**: use `01-ticket-triager` as the structural template.
  ModeSelectOp classifies, per-lane AIExtractMapOp / AIParseNumberOp run only in the matching lane,
  CoalesceNStringOp merges.

- **Extract structured fields + score them deterministically**: use `02-recipe-analyzer`.
  AIExtractMapOp produces a map; deterministic scoring ops (ClampOp, SumOp) compute the final score;
  no AI in the scoring path.

- **Two competing data sources, pick the better one**: use `03-readme-quality`.
  Both HTTPGetOp calls run in parallel; IfIntEqOp + SelectStringOp pick the 200-status body;
  multiple AIScoreOp / AIBoolOp probes score the result independently.

- **Numeric threshold routing + optional warning**: use `04-weather-advisor`.
  IfFloatGtOp / BetweenFloatOp gates branches; SelectStringOp appends a suffix only when a bool probe fires;
  StringConcatOp assembles the final output.

- **Runtime-length list of items, each needing the same pipeline**: use `05-hn-topic-brief`.
  MapOver fans out over the fetched items; a sub-graph of deterministic + AI ops runs per item;
  CollectInto assembles the slice; AISummarizeOp condenses the collected results.
