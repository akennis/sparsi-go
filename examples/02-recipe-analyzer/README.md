# Example 2 — Recipe Difficulty Analyzer

Fetches a recipe from TheMealDB (or reads a captured fixture), runs three AI
extractors over the instructions text in parallel (ingredients, steps,
estimated cook minutes), computes a deterministic difficulty score
`ingredient_count + step_count*1.5 + cook_minutes*0.1`, and routes the result
through one of three difficulty lanes (easy / medium / hard) merged with
`CoalesceNStringOp`.

## Usage

```bash
export CLAUDE_API_KEY=<your key>

# Live API (TheMealDB; no auth)
go run ./examples/02-recipe-analyzer --meal "Pancakes"
go run ./examples/02-recipe-analyzer --meal "Chicken Curry"
go run ./examples/02-recipe-analyzer --meal "Beef Wellington"

# Offline (captured fixtures)
go run ./examples/02-recipe-analyzer --fixture examples/02-recipe-analyzer/testdata/recipes/pancakes.json
go run ./examples/02-recipe-analyzer --fixture examples/02-recipe-analyzer/testdata/recipes/chicken-curry.json
go run ./examples/02-recipe-analyzer --fixture examples/02-recipe-analyzer/testdata/recipes/beef-wellington.json
```

## Expected output

```json
{
  "meal": "Pancakes",
  "ingredient_count": 6,
  "step_count": 4,
  "cook_minutes": 30,
  "difficulty_score": 15.0,
  "difficulty": "medium",
  "advice": "...one-sentence band-specific advice...",
  "ai_nodes": [
    "AIExtractStringSliceOp(ingredients)",
    "AIExtractStringSliceOp(steps)",
    "AIParseNumberOp(cook_minutes)",
    "AIComputeStringToStringOp(medium.advice)"
  ]
}
```

`difficulty` is `easy` when score < 15, `medium` when 15 ≤ score < 30, `hard`
otherwise. Counts come from the AI extractor lists, so exact values vary run
to run; band assignments are stable for the test fixtures above.
