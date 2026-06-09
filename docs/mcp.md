# MCP Integration

`MCPCallOp[In, Out]` and `MCPScriptOp[In, Out]` invoke an MCP server (local subprocess via stdio, or remote via streamable HTTP) as a workflow step.

## Warm-replenish pool

By default each `Run` spawns a fresh MCP subprocess and tears it down on completion. When the cold start dominates wall time, opt into the warm-replenish pool by setting `pool_size: "N"` on the vertex params.

The pool keeps N pre-started sessions ready and refills the slot in a background goroutine after each borrow, so subsequent vertices skip the cold start.

## Custom argument and response shapes

By default, `MCPCallOp` JSON-marshals the dereferenced `*In` value as the tool's `arguments` object. Two optional interfaces let In/Out types override these defaults:

- **`MCPArgsFormatter`** — implement `FormatMCPArgs() (any, error)` on the ``In` type.
- **`MCPResponseParser`** — implement `ParseMCPResponse(text string, structured json.RawMessage) error` on `*Out`.
