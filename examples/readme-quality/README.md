# 03 — GitHub README Quality Report

Fetches a repository's README from `raw.githubusercontent.com` (main and master
branches in parallel, selecting whichever returns HTTP 200), runs five parallel
AI quality probes, computes an average score, routes through one of three
quality lanes (excellent / ok / poor), and appends a "tests not mentioned"
warning when no tests or CI are mentioned.

## Requirements

- `CLAUDE_API_KEY` env var set.

## Test invocations

### Live slug (main branch)
```
go run ./examples/03-readme-quality/ --slug wwz16/dagor
```

### Live slug (master branch fallback)
```
go run ./examples/03-readme-quality/ --slug tj/n
```

### Offline fixture
```
go run ./examples/03-readme-quality/ --fixture examples/03-readme-quality/testdata/dagor.md
```

## Expected output shape

```json
{
  "slug": "wwz16/dagor",
  "purpose": "...",
  "doc_score": 0.72,
  "clarity_score": 0.62,
  "avg_score": 0.67,
  "has_tests": false,
  "has_install": true,
  "verdict": "ok",
  "narrative": "... constructive critique ...\n\nWARNING: tests not mentioned",
  "ai_nodes": ["AIComputeStringToStringOp(purpose)", "AIScoreOp(doc_score)", "..."]
}
```

Verdict thresholds: **excellent** ≥ 0.75 · **ok** ≥ 0.40 · **poor** < 0.40
