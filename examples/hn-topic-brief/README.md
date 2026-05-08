# Example 5 — HackerNews Topic Brief

Fetches up to 10 HN stories for a query, runs per-story AI checks (relevance
filter + multi-label classifier) over two `MapOver` nodes, picks a brief style
with `ModeSelectOp`, and summarises the kept stories in one of three formats
(technical / business / policy).

## Invocations

### Live API

```bash
go run . --query golang
go run . --query "EU AI Act"
go run . --query kubernetes
```

### Cached fixtures (offline)

```bash
go run . --query golang    --cache
go run . --query "EU AI Act" --cache
```

### Explicit fixture file

```bash
go run . --query golang --fixture testdata/hn/golang.json
```

## Expected output (golang, cache)

```json
{
  "query": "golang",
  "story_count": 10,
  "kept_after_filter": 10,
  "label_distribution": {"technical": 8, "human_interest": 4, "other": 2},
  "dominant": "technical",
  "brief_style": "technical_brief",
  "brief": "...",
  "ai_nodes": [
    "ExtractTitlesOp",
    "AIBoolOp(relevance/map)",
    "AIClassifyMultiLabelOp(classify/map)",
    "FilterAndFlattenOp",
    "DominantCategoryOp",
    "ModeSelectOp",
    "AISummarizeOp(technical)"
  ]
}
```

## Expected output (EU AI Act, cache)

```json
{
  "query": "EU AI Act",
  "story_count": 10,
  "kept_after_filter": 10,
  "label_distribution": {"policy": 10, "technical": 10, "business": 9},
  "dominant": "policy",
  "brief_style": "policy_brief",
  "brief": "...",
  "ai_nodes": ["...", "AISummarizeOp(policy)"]
}
```

## Notes

- HN titles can be terse; the relevance filter may occasionally over-reject.
  This is expected behaviour — AI classification of short titles is noisy.
- The `--cache` flag derives the fixture path as
  `testdata/hn/<query-slug>.json`. Fixtures were captured with:
  ```bash
  curl -s 'https://hn.algolia.com/api/v1/search?query=golang&hitsPerPage=10' \
    > testdata/hn/golang.json
  ```
