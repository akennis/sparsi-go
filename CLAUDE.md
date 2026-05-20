# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repository is

`sparsi-go` is a **framework library**, not an application. It exports Go packages (most notably `library/`) that consumers import to build their own DAG-based workflow programs. The repo also publishes two Claude Code **skills** (`sparsi-design`, `sparsi-codegen`) that an AI assistant uses to generate workflow `main.go` files for end users — codegen is handled entirely by the LLM over the bundled skills, not by any binary in this repo.

The root Go package is intentionally empty (`package sparsi`, no exports). All useful code lives in subpackages.

## Commands

```bash
# Compile-check everything (library + tools + examples) without producing artifacts
go build ./...

# Build all example binaries into the gitignored bin/ directory
go build -o bin/ ./examples/...

# Manage dependencies
go mod tidy

# Regenerate library op boilerplate (after modifying op struct tags in library/)
go generate ./library/...

# Assemble the skills/ distribution from skill-src/, examples/, and library descriptions
go generate .

# Both
go generate ./...
```

`go build -o bin/ ./...` writes one binary per main package into `/bin/`, named after the containing directory (e.g. `bin/01-ticket-triager`). The `/bin/` directory is gitignored — never commit binaries. As an alternative to in-repo `bin/`, `go install ./...` writes binaries to `$GOBIN` (typically `~/go/bin/`).

`go generate .` runs `tools/genskills/main.go` and writes into the gitignored `skills/` directory. Do not edit generated files (`*_gen.go`, anything under `skills/` or `bin/`) manually.

`daggen` is a `go install`-managed tool. Ensure `$GOPATH/bin` (typically `~/go/bin`) is on `PATH` before running `go generate ./library/...`.

## Environment

- `CLAUDE_API_KEY` — required at runtime for AI library ops (the default Claude provider).
- `GEMINI_API_KEY` — only needed for ops or examples that select `provider: "gemini"`.

Neither is needed to build the library or regenerate the skills bundle.

## Architecture

Workflows are DAGs built from operators (ops). Each op is a Go struct with `dag:"input"` / `dag:"output"` field tags and implements the dagor operator interface (`Setup`, `Reset`, `Run`). The `daggen` tool reads those tags and generates boilerplate (`InputFields`, `OutputFields`, `SetInputField`, `ResetFields`).

**Library ops** are pure deterministic functions in `library/` (math, string, predicate, select, slice, JSON, IO, time). **AI ops** (`AIComputeOp[In, Out]` variants, `ModeSelectOp`, `AIBoolOp`, `AIScoreOp`, …) call Claude via `anthropic-sdk-go` and are the escape hatch for steps that have no deterministic implementation.

End users do not write the `main.go` themselves — they invoke the `sparsi-design` and `sparsi-codegen` skills in Claude Code, which produce a `main.go` + `go.mod` consuming this library. The generated program follows a `UserInput` / `buildGraph` / dual-mode (`--mode cli|mcp`) pattern documented in `skill-src/sparsi-codegen/SKILL.md`.

## File layout

```
sparsi-go/
├── gen.go                  — package sparsi (empty); only //go:generate directive lives here
├── library/                — the framework (importable subpackage)
│   ├── descriptions.go     — AllDescriptions() — joins op description constants for the codegen skill
│   ├── ai_compute_op.go    — generic AIComputeOp[In, Out] base
│   ├── ai_client.go        — Claude/Gemini client wiring
│   ├── *_ops.go            — math, string, predicate, select, slice, JSON, IO, time ops
│   ├── gen.go              — //go:generate directives for library ops
│   └── *_gen.go            — daggen output (do not edit)
├── tools/
│   ├── genlibdesc/main.go  — standalone library.md generator
│   └── genskills/main.go   — assembles skills/ from skill-src/, examples/, and library descriptions
├── skill-src/              — canonical sources for the skill bundle
│   ├── README.md
│   ├── sparsi-design/
│   │   ├── SKILL.md
│   │   └── references/{design-rules.md, examples/README.md}
│   └── sparsi-codegen/
│       ├── SKILL.md
│       └── references/{dagor-api.md, examples/README.md}
├── skills/                 — gitignored build artifact (see go generate .)
├── bin/                    — gitignored compiled binaries (see go build -o bin/ ./...)
├── examples/               — six end-to-end workflow examples (consume library/ as a dependency)
└── CLAUDE.md
```

## When editing

- **Adding or changing a library op** → edit `library/<topic>_ops.go`, ensure it implements `Setup`/`Reset`/`Run`, register it in `init()`, add a `<OpName>Description` constant, include it in `AllDescriptions()`, then `go generate ./library/...`. If the op is meant to be visible to the codegen skill, also re-run `go generate .` so `skills/<skill>/references/library.md` is refreshed.
- **Updating skill content** → edit files under `skill-src/`. Never edit `skills/` directly. Run `go generate .` to refresh.
- **Adding an example** → drop it under `examples/NN-slug/`, then update the `exampleDirs` slice in `tools/genskills/main.go` so the skill bundle picks it up. Run `go generate .`.

## Dependencies

- [`dagor`](https://github.com/wwz16/dagor) — DAG execution engine (replace-directive-pinned to `github.com/akennis/dagor`)
- [`anthropic-sdk-go`](https://github.com/anthropics/anthropic-sdk-go) — Claude API client
- [`ants/v2`](https://github.com/panjf2000/ants) — goroutine worker pool used by examples
