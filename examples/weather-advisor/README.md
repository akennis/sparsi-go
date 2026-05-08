# Example 4 — Weather-Aware Outfit Advisor

Fetches live weather from wttr.in (or reads a captured fixture), extracts
temperature, precipitation, and wind; classifies conditions via AI; computes
a deterministic temperature band (`cold` / `mild` / `hot`) and boolean
`wet` / `windy` flags; packs all signals; then calls AI for a 2-sentence
outfit recommendation. An orthogonal `AIBoolOp` probe appends a `⚠ unusual
weather` warning when conditions are extreme.

## Usage

```bash
export CLAUDE_API_KEY=<your key>

# Live API (wttr.in — no auth required)
go run ./examples/04-weather-advisor --city London
go run ./examples/04-weather-advisor --city Reykjavik
go run ./examples/04-weather-advisor --city Singapore

# Offline fixtures
go run ./examples/04-weather-advisor --fixture examples/04-weather-advisor/testdata/weather/london.json
go run ./examples/04-weather-advisor --fixture examples/04-weather-advisor/testdata/weather/reykjavik.json
go run ./examples/04-weather-advisor --fixture examples/04-weather-advisor/testdata/weather/singapore.json
```

## Expected output

```json
{
  "city": "london",
  "temp_c": 10,
  "precip_mm": 0,
  "wind_kph": 22,
  "band": "mild",
  "wet": false,
  "windy": false,
  "conditions": ["cloud"],
  "advice": "Wear a light sweater or long-sleeve shirt paired with ...",
  "ai_nodes": [
    "AIParseNumberOp(temp)",
    "AIParseNumberOp(precip)",
    "AIParseNumberOp(wind)",
    "AIClassifyMultiLabelOp(conditions)",
    "AIComputeStringToStringOp(outfit)",
    "AIBoolOp(unusual)"
  ]
}
```

`band` is `cold` when temp < 10°C, `mild` when 10–22°C, `hot` when ≥ 22°C.
`wet` is true when precip > 0.1 mm; `windy` is true when wind > 25 km/h.
The `⚠ unusual weather` suffix is appended to `advice` when the AI probe
detects extreme conditions.
