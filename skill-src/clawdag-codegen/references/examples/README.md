# Example Index

Read the example whose structural pattern most closely matches the workflow you are designing.

| Structural pattern | Example file |
|---|---|
| Multi-lane text classification (free-form input → category → per-lane extraction → coalesce) | `ticket-triager.go` |
| Extraction pipeline + deterministic numeric scoring (parse fields → hardcoded scoring → format) | `recipe-analyzer.go` |
| Parallel HTTP fetch + status-code fallback + multi-probe scoring (dual GET → select → AI probes) | `readme-quality.go` |
| Data parsing + band routing + conditional warning probes (parse → threshold predicates → coalesce + SelectStringOp) | `weather-advisor.go` |
| MapOver fan-out + aggregation + routing (slice → map node → per-item sub-graph → collect → summarize) | `hn-topic-brief.go` |
| Cross-model verification (Claude generates, Gemini independently checks faithfulness) | `faithful-summary.go` |
| Scripted multi-call MCP session (one DAG step holds a long-lived MCP session and issues N tool calls in sequence; per-URL screenshot fan-out via MapOver + warm-replenish pool) | `local-mcp-server.go` |
| Single MCP tool call against a remote (HTTP) MCP server (declare a concrete MCPCallOp variant, point it at a streamable HTTP endpoint) | `remote-mcp-server.go` |

## Quick-reference guidance

- **Free-form text → fixed categories → per-lane work**: use `ticket-triager` as the structural template.
  ModeSelectOp classifies, per-lane AIExtractMapOp / AIParseNumberOp run only in the matching lane,
  CoalesceNStringOp merges.

- **Extract structured fields + score them deterministically**: use `recipe-analyzer`.
  AIExtractMapOp produces a map; deterministic scoring ops (ClampOp, SumOp) compute the final score;
  no AI in the scoring path.

- **Two competing data sources, pick the better one**: use `readme-quality`.
  Both HTTPGetOp calls run in parallel; IfIntEqOp + SelectStringOp pick the 200-status body;
  multiple AIScoreOp / AIBoolOp probes score the result independently.

- **Numeric threshold routing + optional warning**: use `weather-advisor`.
  IfFloatGtOp / BetweenFloatOp gates branches; SelectStringOp appends a suffix only when a bool probe fires;
  StringConcatOp assembles the final output.

- **Runtime-length list of items, each needing the same pipeline**: use `hn-topic-brief`.
  MapOver fans out over the fetched items; a sub-graph of deterministic + AI ops runs per item;
  CollectInto assembles the slice; AISummarizeOp condenses the collected results.

- **Two AI models in series for cross-model verification**: use `faithful-summary`.
  Claude generates a summary via AIComputeStringToStringOp; a custom deterministic op formats
  the source + summary into a verification prompt; AIBoolOp backed by Gemini checks faithfulness.
  Use this pattern when the generating model should not also judge its own output.

- **Driving an MCP server through multiple tool calls in one DAG step**: use `local-mcp-server`.
  Declare an `MCPScriptOp[In, Out]` variant; the Script callback receives an `MCPSession` and issues
  CallTool any number of times against one long-lived session. Pair with `pool_size: N` and
  `defer library.ShutdownMCPPool(...)` when the vertex sits in a fan-out (MapOver sub-graph) and
  pays subprocess cold-start every Run.

- **Calling a remote (HTTP) MCP server**: use `remote-mcp-server`.
  Declare a concrete MCPCallOp variant by embedding `library.MCPCallOp[In, Out]` in a named
  struct; the In type's `json:"…"` tags define the tool's argument shape. Set `transport: "http"`
  and `url: "https://..."` on the vertex. Optional `headers: "Authorization=Bearer ${TOKEN}"` for
  authenticated remote servers. Pooling is stdio-only in v1 — do not set `pool_size > 0` for
  http transport.
