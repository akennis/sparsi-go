# clawdag-go Skills

Two skill packages for designing and generating clawdag-go DAG workflows inside Claude Code
(or any AI assistant that supports the `SKILL.md / references/` convention).

**This bundle targets `github.com/akennis/clawdag-go v0.1.0`.**
Download the matching bundle version from the [clawdag-go releases page](https://github.com/akennis/clawdag-go/releases)
when you upgrade to a new library version.

## Packages

| Package | Purpose |
|---|---|
| `clawdag-design` | Design a maximally deterministic DAG workflow from a task description |
| `clawdag-codegen` | Generate compilable Go code from an approved DAG design |

## Typical workflow

1. `/clawdag-design` — describe your task; the skill produces a structured design document.
2. Refine the design until you approve it.
3. `/clawdag-codegen` — provide the approved design and an output directory; the skill writes `main.go` + `go.mod`, runs `go mod tidy`, and fixes any build errors automatically.
4. Run the compiled binary.

## Installation

These skills work with **Claude Code** — available as a CLI, desktop app, and IDE extension
(VS Code, JetBrains). They do not work with the Claude.ai chat app.

### Global install (available in every Claude Code session)

**macOS / Linux:**
```
cp -r clawdag-design  ~/.claude/skills/
cp -r clawdag-codegen ~/.claude/skills/
```

**Windows (PowerShell):**
```
Copy-Item -Recurse clawdag-design  "$env:USERPROFILE\.claude\skills\"
Copy-Item -Recurse clawdag-codegen "$env:USERPROFILE\.claude\skills\"
```

### Project-local install (available only in the current project)

```
cp -r clawdag-design  .claude/skills/
cp -r clawdag-codegen .claude/skills/
```

Then invoke with `/clawdag-design` or `/clawdag-codegen` from any Claude Code session.

## Updating

When a new bundle is released, download the new zip and replace the old directories:

**macOS / Linux:**
```
rm -rf ~/.claude/skills/clawdag-design ~/.claude/skills/clawdag-codegen
cp -r clawdag-design  ~/.claude/skills/
cp -r clawdag-codegen ~/.claude/skills/
```

**Windows (PowerShell):**
```
Remove-Item -Recurse "$env:USERPROFILE\.claude\skills\clawdag-design"
Remove-Item -Recurse "$env:USERPROFILE\.claude\skills\clawdag-codegen"
Copy-Item -Recurse clawdag-design  "$env:USERPROFILE\.claude\skills\"
Copy-Item -Recurse clawdag-codegen "$env:USERPROFILE\.claude\skills\"
```

No other steps are required — `library.md` is pre-generated in the bundle.

## Required environment variable

`CLAUDE_API_KEY` — needed at **runtime** by any compiled workflow that includes AI ops.
Not needed during design or code generation.
