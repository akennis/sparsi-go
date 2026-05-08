# Example 8 — MCP Google Search & Per-URL Screenshots (fan-out)

Demonstrates two `MCPScriptOp` variants composed via dagor's `MapOver` fan-out:

1. **One** playwright-mcp session searches Google and extracts the first 3
   result URLs.
2. **N=3 parallel** playwright-mcp sessions each open one URL and save a
   screenshot.

```
search_input ──► find_results ──► shoot_each (MapOver) ──► screenshot_paths
 (context-       (one MCP             (3× MCPScreenshotURLOp,
  val const)      session,             own subprocess each,
                  emits []string)      runs in parallel)
```

## What this example shows

- **Two `MCPScriptOp` variants in one workflow.** The search step needs
  session continuity (navigate → type → snapshot on one browser);
  per-URL screenshots are independent, so each one gets its own
  subprocess and runs in parallel via `MapOver`.
- **Custom `Setup` on the per-URL op.** `MCPScreenshotURLOp` extends
  `library.MCPScriptOp[string, string]` with an extra `out_dir` vertex param
  by overriding `Setup` to call the embedded base first, then read its own
  param. The `Script` closure captures `op`, so it can read `op.outDir` at
  Run time.
- **`browser_evaluate` for URL extraction.** The search script runs JS in the
  page to read the first three off-Google `<a>` hrefs from the DOM. More
  robust than scraping the accessibility snapshot, but still tied to the
  page's structure — if Google rearranges things, update `extractURLsJS`.
- **`MapOver` over a `[]string`.** `find_results` outputs `Result *[]string`;
  the downstream MapOver vertex iterates each URL into its sub-vertex's
  `Input *string`. `CollectInto` gathers the per-iteration `Result string`
  back into a `*[]any` (each element a `*string`) on the parent wire.

## Prerequisites

- `npx` on PATH (Node.js).
- First run downloads `@playwright/mcp@latest` plus a browser binary;
  subsequent runs reuse the cache. The fan-out launches three independent
  `playwright-mcp` subprocesses concurrently, so plan for some memory headroom.
- No `CLAUDE_API_KEY` is required.

## Usage

```bash
# Default: search "Shizuoka", write screenshots into <cwd>/shots/
go run ./examples/local-mcp-server

# Customize.
go run ./examples/local-mcp-server \
  --query "Mt Fuji" \
  --out-dir "$PWD/fuji-shots"
```

`--out-dir` must be absolute **and inside the current working directory**
(or `<cwd>/.playwright-mcp/`). playwright-mcp sandboxes file writes to those
two roots — paths under `/tmp`, `$HOME`, etc. are rejected with
`File access denied: … is outside allowed roots`.

## Expected output

```json
{
  "query": "Shizuoka",
  "out_dir": "/path/to/cwd/shots",
  "screenshots": [
    { "url": "https://en.wikipedia.org/wiki/Shizuoka", "path": ".../shots/shot-3a4b9c12.png", "bytes": 412310 },
    { "url": "https://www.japan-guide.com/...",         "path": ".../shots/shot-7f1d2e08.png", "bytes": 287443 },
    { "url": "https://www.lonelyplanet.com/...",        "path": ".../shots/shot-c0a8b1ee.png", "bytes": 359022 }
  ]
}
```

Screenshot filenames are derived from `sha1(url)[:8]` so they're deterministic
per URL and don't collide across iterations.

## Notes & limitations

- `extractURLsJS` greedily picks the first 3 distinct off-Google `http(s)`
  hrefs it finds — that means sponsored/ad results may be included if Google
  renders them as plain anchors. If you need to exclude ads, tighten the
  selector inside that JS string.
- The two ops share the same playwright-mcp launch params; only the
  per-URL op carries the additional `out_dir` param.
- `MCPScriptOp` retries **session start** failures only; the `Script` runs
  at most once per `Run`. For a transient navigation failure inside one of
  the per-URL screenshots, the whole sub-vertex Run will fail (the other
  parallel iterations are unaffected).
- playwright-mcp's tool argument schema has shifted between versions. If a
  call fails with a zod validation error, the path/field name in the message
  tells you exactly which arg the new server expects.
