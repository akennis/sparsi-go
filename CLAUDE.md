# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build

# Run
go run .

# Manage dependencies
go mod tidy

# Regenerate driver op boilerplate AND skills/ distribution (after modifying driver_ops.go or any source in skill-src/, prompts/, or examples/)
go generate .

# Regenerate library op boilerplate only (after modifying library op struct tags)
go generate ./library/...
```

`go generate .` regenerates `driver_*_gen.go` files via `daggen` and assembles the `skills/` distribution directory via `tools/genskills/main.go`. Do not edit generated files manually. The `skills/` directory is gitignored and must be regenerated before packaging a release.

## Environment

- `CLAUDE_API_KEY` — required for all Claude API calls (design, codegen, AI library ops)

## Architecture

This is a DAG-based AI code generation system. Workflows are maximally deterministic DAGs built from a library of pure-function ops; AI only fills gaps where no deterministic op exists. The driver itself is expressed as a dagor DAG. A math-ops demo (add, sub, div — but intentionally no multiply) showcases AI-powered fallback nodes.

**Driver DAG data flow:**
```
PromptOp ──────────────────────────────────────────────────────────┐
                                                                    ▼
LibraryScanOp ──────────────────────────────────────► GenerateOp ► WriteFilesOp ► CodegenOp ► CompileOp ► RunOp ► OutputOp
```

**Key concepts:**

- **Library ops** (`AddOp`, `SubOp`, `DivOp`) are pure deterministic functions in `library/math_ops.go`.
- **Driver ops** (8 total in `driver_ops.go`) orchestrate the AI code generation pipeline.
- **Operators** are structs with `dag:"input"` / `dag:"output"` field tags. They implement the dagor operator interface: `Setup`, `Reset`, `Run`.
- **Code generation**: `daggen` reads those tags and generates `InputFields`, `OutputFields`, `SetInputField`, and `ResetFields` methods.
- **GenerateOp** calls Claude with structured JSON output to produce `main.go` for the solution binary. The solution DAG is built via the fluent builder API (`graph.NewBuilder`).
- **WriteFilesOp** writes the generated files to a temp dir and runs `go mod tidy`.
- **CodegenOp / CompileOp / RunOp** all return `nil` errors so `OutputOp` always executes.
- **Retry loop** in `main()` retries up to 5 times, feeding the previous error back to `GenerateOp`.

**Solution binary output contract:**
```json
{"result": "17", "ai_nodes": [{"op": "MultiplyOp", "inputs": {...}, "output": 8, "reasoning": "..."}]}
```

**File layout:**
```
dag-ai/
├── main.go               — driver DAG + retry loop + SolutionOutput types
├── driver_ops.go         — 8 driver op structs
├── gen.go                — //go:generate directives for driver ops
├── driver_*_gen.go       — generated (do not edit)
├── library/
│   ├── math_ops.go       — AddOp, SubOp, DivOp + description constants
│   ├── gen.go            — //go:generate directives for library ops
│   └── math_*_gen.go     — generated (do not edit)
└── CLAUDE.md
```

**Dependencies:**
- [`dagor`](https://github.com/wwz16/dagor) — DAG execution engine
- [`ants/v2`](https://github.com/panjf2000/ants) — goroutine worker pool
