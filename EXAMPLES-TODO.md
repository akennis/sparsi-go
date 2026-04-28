# EXAMPLES-TODO

Five worked examples that exercise the clawdag-go framework. Each example is
defined to *force* a workflow with: (a) multiple sequential steps, (b) at least
two parallel/conditional branches that are chosen at runtime, and (c) at least
one AI operation (most have 2–4). For every example the test data is either
supplied verbatim below or fetched from a free, no-auth public endpoint.

Each section is self-contained so a fresh Claude Code session — given **only**
this file plus the project's `CLAUDE.md` and `README.md` — can implement and
run the example end-to-end.

The expected delivery for each example is:

1. A new directory `examples/NN-slug/` with a `main.go` and (when needed) a
   `testdata/` subdirectory containing the fixtures listed in this file.
2. The `main.go` builds a dagor `*graph.Graph` with the topology described,
   reads its primary input from CLI args / stdin / a flag, runs the engine,
   and prints a structured JSON result on stdout. Use the same dual-mode
   pattern (`--mode cli|mcp`) as the auto-generated solutions only if it is
   trivial to add; otherwise CLI is sufficient.
3. A README in the example directory (≤30 lines) showing the test invocations
   and the expected output for each fixture.

All examples assume `CLAUDE_API_KEY` is exported. None require any other
secret. Where an HTTP endpoint is used it is a public no-auth endpoint that
has been verified to work with `curl`.

------------------------------------------------------------------------

## Example 1 — Customer Support Ticket Triager

### Goal

Take a free-text customer support email and route it through one of four
category-specific extraction pipelines, then emit a structured triage record.

### Why this exercises the framework

- Multi-step: classify → branch → extract → score → format.
- Multi-branch: four mutually-exclusive lanes selected by a `ModeSelectOp`
  output and gated by `IfStringEqOp` predicates registered against a wire.
- AI ops used (4): `ModeSelectOp`, `AIExtractMapOp`, `AIParseNumberOp`,
  `AIExtractStringSliceOp`, `AIScoreOp`, `AIComputeStringToStringOp`,
  `AISummarizeOp` (subset per branch — at least 3 fire on every input).

### Inputs

A single string: the body of a customer support email.

### Test data (verbatim — write to `testdata/tickets/*.txt`)

```text
testdata/tickets/billing.txt
────────────────────────────
Subject: I was double-charged for January

Hi, my name is Maria Lopez (account: ML-44218). I noticed that my credit card
was charged twice for the January subscription — $49.00 on Jan 3rd and another
$49.00 on Jan 4th. Could you please refund the duplicate charge? You can reach
me at maria.lopez@example.com.

Thanks,
Maria
```

```text
testdata/tickets/bug.txt
────────────────────────
Subject: App crashes on every upload

The Android app (v3.7.1, Pixel 7) crashes consistently when I try to upload a
PDF over 5MB. To reproduce:
  1. Open the app and sign in.
  2. Tap the + button on the home screen.
  3. Choose "Upload PDF" and pick any PDF larger than 5MB.
  4. Tap "Send".
The app freezes for ~3 seconds, then closes with no error message. This is
blocking my entire team from submitting weekly reports — please prioritize.
```

```text
testdata/tickets/feature.txt
────────────────────────────
Subject: Please add dark mode

I love the product but staring at the bright white interface for 8 hours a day
is killing my eyes. Is there any chance you could add a dark mode toggle? Even
a system-default option would be amazing. Several of my coworkers have asked
about this too — it would be a huge quality-of-life win for our whole team.
```

```text
testdata/tickets/other.txt
──────────────────────────
Hey team, just wanted to say thank you for the great onboarding session
yesterday. Really appreciated the time. No questions, just gratitude :)
```

### Workflow

```
StringConstOp (ticket_body)
        │
        ▼
ModeSelectOp  ── categories: "billing,bug,feature,other"
        │
        ├──► IfStringEqOp(=billing) ── billing_lane:
        │      ├─ AIExtractMapOp (extract: name, email, account_id, total_amount, charge_count)
        │      └─ AIParseNumberOp (extract refund amount in USD)
        │
        ├──► IfStringEqOp(=bug)     ── bug_lane:
        │      ├─ AIExtractStringSliceOp (extract reproduction steps)
        │      ├─ AIScoreOp (criterion: severity / urgency)
        │      └─ AIBoolOp (predicate: is this a regression?)
        │
        ├──► IfStringEqOp(=feature) ── feature_lane:
        │      ├─ AISummarizeOp (one-sentence feature description)
        │      └─ AIScoreOp (criterion: business impact)
        │
        └──► IfStringEqOp(=other)   ── other_lane:
               └─ AIComputeStringToStringOp (operation: "write a polite, brief
                                              acknowledgement of this email")
        │
        ▼
CoalesceStringOp (merge per-lane JSON-encoded summaries → final_brief)
```

The four `IfStringEqOp` vertices each compare the `ModeSelectOp` `Result`
output against a constant string fed by `StringConstOp`; their `Match` output
is bound to the lane's downstream vertices via `Condition(...)` predicates
registered against the wire (mirror the pattern in `example/main.go`'s
`registerPredicates`).

Per-lane outputs are serialized to JSON inside the lane (use a tiny
hand-written `EncodeLaneOp` per lane, *or* simpler: have each lane end in
`AIComputeStringToStringOp` with `operation: "format the following fields as
JSON"` so the framework's existing AI library does the formatting).

### Expected output

```json
{
  "category": "billing",
  "summary": "...",
  "details": { ... lane-specific ... },
  "ai_nodes": [ { "op": "...", "reasoning": "..." }, ... ]
}
```

### Implementation notes

- Register predicates `lane_is_billing`, `lane_is_bug`, `lane_is_feature`,
  `lane_is_other` that read the wire `ticket_category` (the
  `ModeSelectOp.Result` wire).
- The four lanes' final string wires feed positions A/B/C/D of a
  `CoalesceStringOp` with `Merge(config.MergeCoalesce)`.
- Driver reads each ticket file with `FileReadOp` (path passed via flag) and
  pipes `Content` into the classifier.

------------------------------------------------------------------------

## Example 2 — Recipe Difficulty Analyzer (live API)

### Goal

Given a meal name, fetch its recipe from a free public API, derive a
deterministic difficulty score from ingredient count + step count + estimated
cooking time, then branch into difficulty-specific advice.

### Why this exercises the framework

- Multi-step: HTTP fetch → JSON extract → AI extraction (×3) → arithmetic →
  threshold branching → AI advice.
- Multi-branch: three difficulty lanes (easy/medium/hard) selected by chained
  `IfFloatLtOp` / `BetweenFloatOp` predicates and merged with
  `CoalesceStringOp`.
- AI ops used (4): `AIExtractStringSliceOp` (×2), `AIParseNumberOp`,
  `AIComputeStringToStringOp`.
- Live data — no API key required.

### Test data — TheMealDB (free, public, no auth)

Endpoint pattern: `https://www.themealdb.com/api/json/v1/1/search.php?s=<name>`

Verified working test invocations:

| Meal name        | Expected difficulty (heuristic) |
|------------------|---------------------------------|
| `Beef Wellington`| hard                            |
| `Chicken Curry`  | medium                          |
| `Fish pie`       | medium                          |
| `Pancakes`       | easy                            |

A representative response (truncated) for `s=Pancakes`:

```json
{
  "meals": [
    {
      "idMeal": "52854",
      "strMeal": "Pancakes",
      "strInstructions": "In a large bowl, sift the flour ...",
      "strIngredient1": "Plain Flour",
      "strIngredient2": "Eggs",
      "...": "..."
    }
  ]
}
```

If the live API call is undesirable during testing, write three sample
responses to `testdata/recipes/*.json` by capturing real responses ahead of
time:

```bash
curl -s 'https://www.themealdb.com/api/json/v1/1/search.php?s=Pancakes' \
  > examples/02-recipe-analyzer/testdata/recipes/pancakes.json
```

### Workflow

```
StringConcatOp (build URL: prefix + meal_name)
        │
        ▼
HTTPGetOp                                     (Body, StatusCode)
        │
        ▼
JSONExtractOp (path: meals.0.strInstructions)  ──► instructions_text
JSONExtractOp (path: meals.0.strMeal)          ──► meal_name
        │                  │
        ▼                  ▼
AIExtractStringSliceOp     AIExtractStringSliceOp
  op: "extract every          op: "extract every
  ingredient name as          discrete cooking step
  a flat list"                 as a flat list"
        │                  │
        ▼                  ▼
SliceLenOp                 SliceLenOp
  →ingredient_count          →step_count
        │                  │
        └──┬───────────────┘
           ▼
AIParseNumberOp (input: instructions_text;
                 op: "estimate total cooking time
                 in minutes from these instructions")
                                 →cook_minutes
           │
           ▼
        DETERMINISTIC SCORE
        (ConstOp, MulOp, AddOp ×3 → "difficulty" float)
        score = ingredient_count*1 + step_count*1.5 + cook_minutes*0.1
           │
           ├──► IfFloatLtOp (score, 15)         ──► easy_lane
           ├──► BetweenFloatOp (score, 15, 30)  ──► medium_lane
           └──► IfFloatGeOp (score, 30)         ──► hard_lane
                          │     │     │
                          ▼     ▼     ▼
                    each lane: AIComputeStringToStringOp
                    with a difficulty-specific operation
                    (e.g. easy → "write a one-sentence
                    encouraging tip for a beginner cook
                    making this recipe").
                          │     │     │
                          └──┬──┴──┬──┘
                             ▼     ▼
                       CoalesceStringOp → advice
                             │
                             ▼
                     final JSON: meal, ingredient_count,
                     step_count, cook_minutes, score,
                     difficulty, advice
```

Bind predicates `score_is_easy`, `score_is_medium`, `score_is_hard` to the
lane gate vertices' `Condition(...)`. Each predicate reads a single float
wire and the threshold(s) are constants via `ConstOp`.

### Expected output

```json
{
  "meal": "Pancakes",
  "ingredient_count": 6,
  "step_count": 4,
  "cook_minutes": 20,
  "difficulty_score": 14.0,
  "difficulty": "easy",
  "advice": "Pancakes are a perfect first recipe — keep the heat medium-low and don't flip too soon!",
  "ai_nodes": [...]
}
```

### Implementation notes

- TheMealDB returns `null` for empty ingredient slots (`strIngredient9`–
  `strIngredient20`). The AI extractor handles this naturally; do **not**
  try to glue them together with deterministic ops.
- `HTTPGetOp.StatusCode` should be checked against 200 via an
  `IfIntEqOp` + a guard branch that emits an error JSON when the API fails.

------------------------------------------------------------------------

## Example 3 — GitHub README Quality Report

### Goal

Given an `owner/repo` slug, fetch the repository's README, run several
AI-powered quality probes in parallel, then produce a graded report.

### Why this exercises the framework

- Multi-step: URL build → HTTP → parallel AI probes → aggregate score → branch.
- Multi-branch: at least three lanes (Excellent / OK / Poor) and an
  *additional* orthogonal branch on the boolean "has_tests" probe that, when
  false, attaches a "missing tests" warning to the report.
- AI ops used (5): `AISummarizeOp`, `AIScoreOp` (×2 in parallel),
  `AIBoolOp` (×2 in parallel), `AIComputeStringToStringOp`.
- Live data — `raw.githubusercontent.com` requires no auth.

### Test data

Use these public repos. Each URL is the canonical raw README path. (Owner and
repo names are stable enough for documentation purposes; if any 404s, swap to
the next entry.)

| Slug                       | Raw URL                                                                 |
|----------------------------|-------------------------------------------------------------------------|
| `golang/go`                | https://raw.githubusercontent.com/golang/go/master/README.md            |
| `torvalds/linux`           | https://raw.githubusercontent.com/torvalds/linux/master/README          |
| `sindresorhus/awesome`     | https://raw.githubusercontent.com/sindresorhus/awesome/main/readme.md   |
| `tj/n`                     | https://raw.githubusercontent.com/tj/n/master/Readme.md                 |
| `wwz16/dagor`              | https://raw.githubusercontent.com/wwz16/dagor/main/README.md            |

The README is fetched as plain text; no JSON parsing is needed.

The driver should accept either a slug (and build the URL) or a direct URL.
For the slug form, also accept an optional `--branch` flag (default `main`,
fall back to `master` automatically using a coalesce of two HTTP fetches).

### Workflow

```
StringConcatOp (URL = "https://raw.githubusercontent.com/" + slug + "/main/README.md")
HTTPGetOp ──► main_body, main_status

StringConcatOp (URL = "https://raw.githubusercontent.com/" + slug + "/master/README.md")
HTTPGetOp ──► master_body, master_status     (always runs in parallel)

IfIntEqOp (main_status == 200) → use_main
SelectStringOp (cond=use_main, ifTrue=main_body, ifFalse=master_body) → readme

      readme
        │
        ├──► AISummarizeOp (op: "summarize purpose in one sentence")  →purpose
        ├──► AIScoreOp     (criterion: "documentation completeness")  →doc_score
        ├──► AIScoreOp     (criterion: "clarity for new contributors") →clarity_score
        ├──► AIBoolOp      (predicate: "does this README mention tests or CI?") →has_tests
        └──► AIBoolOp      (predicate: "does this README contain installation/usage instructions?") →has_install

  (deterministic) avg_score = (doc_score + clarity_score) / 2

  ├──► IfFloatGeOp (avg_score, 0.75)                        →lane: excellent
  ├──► BetweenFloatOp (avg_score, 0.4, 0.75)                →lane: ok
  ├──► IfFloatLtOp (avg_score, 0.4)                          →lane: poor

  Each lane runs AIComputeStringToStringOp with a different operation:
    excellent → "write a one-paragraph endorsement of this README"
    ok        → "write a one-paragraph constructive critique with 2 specific suggestions"
    poor      → "write a one-paragraph improvement plan listing the 3 highest-impact fixes"

  CoalesceStringOp → narrative

  IfBoolEqOp(has_tests, false) → emit "WARNING: tests not mentioned" via SelectStringOp;
  StringConcatOp glues warning (or "") onto narrative → final_narrative
```

Note: `IfBoolEqOp` is not in the library; build it as
`SelectStringOp(cond=has_tests, ifTrue="", ifFalse="WARNING: ...")`.

### Expected output

```json
{
  "slug": "wwz16/dagor",
  "purpose": "A pure-Go DAG execution engine ...",
  "doc_score": 0.78,
  "clarity_score": 0.82,
  "avg_score": 0.80,
  "has_tests": true,
  "has_install": true,
  "verdict": "excellent",
  "narrative": "This README is exemplary because ...",
  "ai_nodes": [...]
}
```

### Implementation notes

- The `main`/`master` fallback is intentionally implemented as two parallel
  HTTP fetches whose results coalesce — this stresses the `OnError(continue)`
  + `Merge(config.MergeCoalesce)` pattern.
- Cap `HTTPGetOp` body usage by trimming `readme` to the first 8 KB before
  passing to AI ops. Add a tiny hand-rolled `StringTruncateOp` if needed.

------------------------------------------------------------------------

## Example 4 — Weather-Aware Outfit Advisor

### Goal

Given a city name, fetch live weather, then compose an outfit recommendation
that depends on temperature band, precipitation, and wind.

### Why this exercises the framework

- Multi-step: HTTP → JSON extract (×4) → triple branching on independent
  thresholds → AI synthesis.
- Multi-branch: temperature has three lanes (cold / mild / hot); precipitation
  is an orthogonal yes/no branch; wind is a third orthogonal threshold check.
  All three orthogonal decisions feed the final synthesis.
- AI ops used (2 minimum, 3 preferred): `AIComputeStringToStringOp` (final
  synthesis), `AIBoolOp` ("is this an unusual weather pattern?"),
  `AIClassifyMultiLabelOp` (clothing categories needed).

### Test data — wttr.in (free, public, no auth)

Endpoint pattern: `https://wttr.in/<CITY>?format=j1`

Verified working invocations:

| City         | Notes                                  |
|--------------|----------------------------------------|
| `Reykjavik`  | usually cold                           |
| `Singapore`  | usually hot + humid + chance of rain   |
| `London`     | usually mild + chance of rain          |
| `Phoenix`    | usually hot, dry                       |
| `Anchorage`  | usually cold                           |

The relevant fields in `j1` format:

```
.current_condition[0].temp_C            string (e.g. "12")
.current_condition[0].precipMM          string (e.g. "0.3")
.current_condition[0].windspeedKmph     string (e.g. "18")
.current_condition[0].weatherDesc[0].value  string (e.g. "Light rain")
```

Capture sample fixtures into `testdata/weather/{city}.json` for offline tests.

### Workflow

```
StringConstOp (city) ──► StringConcatOp (URL) ──► HTTPGetOp (Body)

JSONExtractOp (.current_condition.0.temp_C)            →temp_str
JSONExtractOp (.current_condition.0.precipMM)          →precip_str
JSONExtractOp (.current_condition.0.windspeedKmph)     →wind_str
JSONExtractOp (.current_condition.0.weatherDesc.0.value) →desc_str

AIParseNumberOp (input: temp_str)    →temp_c    (using AI to robustly handle
AIParseNumberOp (input: precip_str)  →precip_mm  weird formatting / units)
AIParseNumberOp (input: wind_str)    →wind_kph

  temp_c branches (chained predicates → SwitchStringOp):
    temp_c < 10           → "cold"
    10 <= temp_c < 22     → "mild"
    temp_c >= 22          → "hot"
  Use SwitchStringOp keyed on a wire fed by 3-way classification.
  *Or* express the band as three guarded ConstOps + CoalesceStringOp.

  precip branch:
    precip_mm > 0.1 → wet=true else wet=false  (IfFloatGtOp)

  wind branch:
    wind_kph > 25 → windy=true else windy=false  (IfFloatGtOp)

  AIClassifyMultiLabelOp
      Input: desc_str
      categories: "rain,snow,fog,sun,cloud,storm"
      → conditions []string

  Final synthesis (AIComputeStringToStringOp):
      operation: "given the temperature band, wet flag, windy flag, and
                  conditions list, write 2 sentences recommending an outfit"
      Input: PackOutfitInputsOp (small hand-written op with all five fields)
      → outfit_advice

  AIBoolOp
      predicate: "is the described weather unusual or extreme?"
      Input: desc_str
      → unusual_flag

  StringConcatOp (advice + (unusual_flag ? "  ⚠ unusual weather" : ""))
      via SelectStringOp on the unusual flag → final_advice
```

`PackOutfitInputsOp` is a tiny custom op (3 strings + 2 bools in, 1 string
out) that formats its inputs into a single description string for the
final AI call. Implement it inline in the example's `main.go`.

### Expected output

```json
{
  "city": "Reykjavik",
  "temp_c": 4.0,
  "precip_mm": 0.0,
  "wind_kph": 32,
  "band": "cold",
  "wet": false,
  "windy": true,
  "conditions": ["cloud"],
  "advice": "Wear a heavy insulated coat, hat, and gloves; the wind makes it feel colder than the temperature suggests."
}
```

### Implementation notes

- wttr.in is occasionally slow; use `context.WithTimeout(15*time.Second)` per
  HTTP call.
- The deterministic temperature-band classifier exists *because* AI
  classification would be wasteful; demonstrate the framework's
  "AI only where determinism falls short" thesis.

------------------------------------------------------------------------

## Example 5 — HackerNews Topic Brief

### Goal

Given a search query, fetch the top recent stories from HackerNews, filter
out off-topic results, classify the remaining stories, and produce a brief
that adapts its structure to the dominant category.

### Why this exercises the framework

- Multi-step: URL build → HTTP → JSON extract → map node fan-out (per-story
  AI checks) → re-collection → category dispatch → AI summarization.
- Multi-branch: a `MapOver` sub-graph fans an AI filter over each story; then
  a top-level `ModeSelectOp` selects one of three brief styles
  (`technical_brief`, `business_brief`, `policy_brief`).
- AI ops used (4): `AIBoolOp` (per-item relevance filter inside the map),
  `AIClassifyMultiLabelOp` (per-item classifier inside the map),
  `ModeSelectOp` (top-level dominant category), `AISummarizeOp` (final brief).

### Test data — HackerNews Algolia API (free, public, no auth)

Endpoint pattern: `https://hn.algolia.com/api/v1/search?query=<Q>&hitsPerPage=10`

Verified working test queries:

| Query              | Expected dominant category   |
|--------------------|------------------------------|
| `golang`           | technical_brief              |
| `nvidia`           | business_brief or technical  |
| `EU AI Act`        | policy_brief                 |
| `kubernetes`       | technical_brief              |
| `rust`             | technical_brief              |

A representative response shape:

```json
{
  "hits": [
    {"objectID": "39201234", "title": "Go 1.22 released", "url": "...", "points": 412},
    {"objectID": "39201235", "title": "Why I left Google", "url": "...", "points": 87},
    ...
  ]
}
```

Capture two fixtures into `testdata/hn/golang.json` and `testdata/hn/eu-ai-act.json`
for offline reproducibility.

### Workflow

```
StringConcatOp (URL = base + query) ──► HTTPGetOp ──► response_json

(small custom op) ExtractTitlesOp:
   parse response_json; emit Result *[]string of hit titles
   (a one-off op — JSONExtractOp can't unpack arrays of objects)

  titles []string
        │
        ▼
  MapOver("title")
      SubVertex("relevant").
          Op("AIBoolOp").
          Params(predicate: "is this story actually about <query>?").
          Input("Input", "title").
          Output("Result", "is_relevant").
      SubVertex("classify").
          Op("AIClassifyMultiLabelOp").
          Params(categories: "technical,business,policy,human_interest,other").
          Input("Input", "title").
          Output("Result", "labels").
      CollectInto("is_relevant", "relevant_flags").
      CollectInto("labels", "label_lists").

  (small custom op) FilterAndFlatten:
      input: titles, relevant_flags, label_lists
      output: kept_titles []string, all_labels []string
      drops titles where relevant_flag is false; flattens labels

  (small custom op) DominantCategoryOp:
      input: all_labels []string
      output: dominant string  (most frequent label, ties broken alphabetically)

  ModeSelectOp:
      categories: "technical_brief,business_brief,policy_brief"
      Input: dominant (passed through StringConcatOp/format hint to bias)
      → brief_style

  Three lanes via IfStringEqOp / Condition:
      brief_style == technical_brief →
          AISummarizeOp (op: "summarize as a technical engineering newsletter:
                              one bullet per story; group by sub-topic")
      brief_style == business_brief →
          AISummarizeOp (op: "summarize as an executive business brief:
                              3-sentence overview + bulleted impact list")
      brief_style == policy_brief →
          AISummarizeOp (op: "summarize as a policy memo: legislative items,
                              affected parties, and likely timeline")

  CoalesceStringOp → final_brief
```

### Expected output

```json
{
  "query": "EU AI Act",
  "story_count": 9,
  "kept_after_filter": 7,
  "label_distribution": {"policy": 5, "technical": 2, "business": 2},
  "dominant": "policy",
  "brief_style": "policy_brief",
  "brief": "Legislative items: ...\nAffected parties: ...\nTimeline: ...",
  "ai_nodes": [...]
}
```

### Implementation notes

- The `MapOver` block is the single most novel construct here — it's
  documented in the project README under "Map nodes". Each iteration runs
  *two* AI calls (relevance + classify) in parallel; the dagor engine
  schedules them concurrently for free.
- Cap `hitsPerPage=10` to keep the AI cost bounded. Even at 10 the example
  fires ~22 AI calls per run, so add a `--cache` flag that skips the network
  fetch and loads from `testdata/hn/<query>.json`.
- HackerNews titles can be sparse; the relevance filter occasionally over-
  rejects on terse titles. Document this in the example's README so future
  Claude sessions don't chase the false positive.

------------------------------------------------------------------------

## Cross-cutting deliverables

For all five examples, also:

- Add a top-level `examples/README.md` that lists the five examples with a
  one-line description and the command to run each.
- Add each example to the workspace `go.work` (or update the module `go.mod`
  with a `replace` directive if the examples live in their own modules).
- For each example, ensure `go vet ./examples/NN-slug/...` and `go build
  ./examples/NN-slug/...` pass cleanly. No tests are required, but a tiny
  table-driven smoke test that loads a fixture and checks a stable scalar
  output (e.g. example 2's `difficulty` band) is welcome.

## Order of implementation

Recommended order (easy → hard):

1. Example 1 (Customer Support Triager) — pure text in / text out, no HTTP.
2. Example 2 (Recipe Analyzer) — adds HTTP + JSON, single linear pipe.
3. Example 4 (Weather Advisor) — adds three orthogonal branches.
4. Example 3 (GitHub README) — adds parallel HTTP fetches + coalesce-on-error.
5. Example 5 (HN Topic Brief) — adds map nodes; do this last.
