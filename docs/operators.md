# Operator Library

The following operators are available in `library/` for use in your workflows:

| Op | Kind | Description |
|----|------|-------------|
| `ContextValOp[T]` (via `ContextValFactory`) | deterministic | Reads a typed value from `context.Context` at run time |
| `ConstOp[T]` (via `RegisterConst`) | deterministic | Emits a fixed Go value captured at registration |
| **Math — float64** | | |
| `AddFloatOp` | deterministic | A + B (`float64`) |
| `SubFloatOp` | deterministic | A − B (`float64`) |
| `MulFloatOp` | deterministic | A × B (`float64`) |
| `DivFloatOp` | deterministic | A ÷ B (`float64`) — errors on zero divisor |
| `PowFloatOp` | deterministic | A ^ B (`float64`) |
| `ModFloatOp` | deterministic | `math.Mod(A, B)` |
| `RoundOp` | deterministic | Rounds a `float64` to nearest integer |
| `ClampFloatOp` | deterministic | Clamps `float64` Value to [Min, Max] |
| `SumFloatOp` | deterministic | Sums a `[]float64` slice |
| `MinFloatOp` | deterministic | Minimum of a `[]float64` slice |
| `MaxFloatOp` | deterministic | Maximum of a `[]float64` slice |
| `PackMathOperandsOp` | deterministic | Packs two `float64` inputs into a `MathOperands` struct |
| `AIComputeMathOperandsToFloat64Op` | AI | Performs any binary float64 operation (e.g. multiply) via the AI provider |
| **Math — int** | | |
| `AddIntOp` | deterministic | A + B (`int`) |
| `SubIntOp` | deterministic | A − B (`int`) |
| `MulIntOp` | deterministic | A × B (`int`) |
| `DivIntOp` | deterministic | A ÷ B (`int`) — errors on zero divisor |
| `PowIntOp` | deterministic | A ^ B (`int`) |
| `ModIntOp` | deterministic | A % B (`int`) |
| `ClampIntOp` | deterministic | Clamps `int` Value to [Min, Max] |
| `SumIntOp` | deterministic | Sums an `[]int` slice |
| `MinIntOp` | deterministic | Minimum of an `[]int` slice |
| `MaxIntOp` | deterministic | Maximum of an `[]int` slice |
| **Casts** | | |
| `IntToFloat64Op` | deterministic | `int` → `float64` |
| `Float64ToIntOp` | deterministic | `float64` → `int` (truncation) |
| `Float64ToStringOp` | deterministic | `*float64` → `string` via `%v` |
| `IntToStringOp` | deterministic | `*int` → `string` via `%v` |
| `BoolToStringOp` | deterministic | `*bool` → `"true"` / `"false"` |
| `ToStringOp` | deterministic | Reflection-based `any` → `string` for custom struct wires |
| **Strings** | | |
| `StringConcatOp` | deterministic | Concatenates two strings |
| `StringToLowerOp` | deterministic | Lowercases a string |
| `StringSplitOp` | deterministic | Splits a string by a separator into `[]string` |
| `StringLookupOp` | deterministic | Looks up a key in a params-configured map; returns `""` on miss |
| `RegexMatchOp` | deterministic | Reports whether input matches a compiled regex |
| `RegexExtractOp` | deterministic | Returns first match (or submatch group 1) of a regex |
| `AIComputeStringToStringOp` | AI | Performs any string→string transformation via the AI provider |
| **Booleans** | | |
| `BoolNotOp` | deterministic | Logical NOT |
| `BoolAndOp` | deterministic | Logical AND |
| `BoolOrOp` | deterministic | Logical OR |
| **Predicates — float64** | | |
| `IfFloatGtOp` | deterministic | A > B |
| `IfFloatLtOp` | deterministic | A < B |
| `IfFloatEqOp` | deterministic | A == B |
| `IfFloatGeOp` | deterministic | A >= B |
| `IfFloatLeOp` | deterministic | A <= B |
| `BetweenFloatOp` | deterministic | Min <= Value <= Max (inclusive) |
| **Predicates — int** | | |
| `IfIntGtOp` | deterministic | A > B |
| `IfIntLtOp` | deterministic | A < B |
| `IfIntEqOp` | deterministic | A == B |
| `IfIntGeOp` | deterministic | A >= B |
| `IfIntLeOp` | deterministic | A <= B |
| **Predicates — string** | | |
| `IfStringEqOp` | deterministic | A == B |
| `IfStringContainsOp` | deterministic | A contains B as a substring |
| `IfStringHasPrefixOp` | deterministic | A starts with B |
| `IfStringHasSuffixOp` | deterministic | A ends with B |
| `IfStringRegexMatchOp` | deterministic | Input matches a compiled regex (param: `pattern`) |
| `IfEmptyStringOp` | deterministic | Value is nil or empty string |
| `IfEmptySliceStringOp` | deterministic | `[]string` value is nil or empty |
| `IfEmptySliceFloat64Op` | deterministic | `[]float64` value is nil or empty |
| **Routing / select** | | |
| `SelectStringOp` | deterministic | Ternary: returns IfTrue or IfFalse based on a bool condition |
| `SelectFloat64Op` | deterministic | Ternary over `float64` |
| `SelectIntOp` | deterministic | Ternary over `int` |
| `SelectBoolOp` | deterministic | Ternary over `bool` |
| `SwitchStringOp` | deterministic | Maps Key through a params-configured cases table; returns a default on miss |
| `DefaultStringOp` | deterministic | Returns Default when Value is nil or empty; otherwise Value |
| `DefaultFloat64Op` | deterministic | Returns Default when Value is nil; zero is a valid value |
| `DefaultIntOp` | deterministic | Returns Default when Value is nil; zero is a valid value |
| **Slices** | | |
| `SliceLenOp` | deterministic | Length of a `[]string` |
| `SliceAtOp` | deterministic | Element at index (param or wire) |
| `SliceFirstOp` | deterministic | First element |
| `SliceLastOp` | deterministic | Last element |
| `SliceContainsOp` | deterministic | Reports whether a `[]string` contains a value |
| `SliceJoinOp` | deterministic | Joins `[]string` with a separator |
| `SliceFilterEqOp` | deterministic | Filters `[]string` to elements equal to Value |
| `SliceTopKOp` | deterministic | Indices of the K highest scores in a `[]float64` |
| **JSON** | | |
| `JSONExtractOp` | deterministic | Extracts a value from JSON using a dot-separated path |
| **I/O** | | |
| `FileReadOp` | deterministic | Reads a file from disk |
| `EnvOp` | deterministic | Reads an environment variable |
| `HTTPGetOp` | deterministic | HTTP GET — returns Body and StatusCode |
| **Time** | | |
| `CityTimeOp` | deterministic | Returns the current time for "New York" or "Tokyo" |
| **MCP / external tools** | | |
| `MCPCallOp[In, Out]` | external | Generic single-call MCP tool wrapper. |
| `MCPScriptOp[In, Out]` | external | Generic multi-call MCP scripted session. |
| **Retrieval (RAG)** | | |
| `RetrieveOp` | external | Pulls top-k `library.Document` records from a registered `library.Retriever`. |
| `RetrieveWithFiltersOp` | external | Like `RetrieveOp` but with a `Filters *map[string]string` input wire. |
| `ValidateCitationsOp` | deterministic | Filters LLM-emitted citations against an allow-list of source identifiers. |
| **AI ops** | | |
| `ModeSelectOp` | AI | Classifies input text into one of a fixed set of categories |
| `AIBoolOp` | AI | Yes/no predicate about input text |
| `AIScoreOp` | AI | Scores text against a criterion, returns float64 ∈ [0,1] |
| `AIClassifyMultiLabelOp` | AI | Maps input to zero or more of a fixed set of labels |
| `AIExtractStringSliceOp` | AI | Extracts a list of strings from free-form text |
| `AIExtractMapOp` | AI | Extracts key-value pairs from free-form text |
| `AIParseNumberOp` | AI | Converts free-form text to a `float64` |
| `AISummarizeOp` | AI | Summarizes a `[]string` into a single result string |
| `AIBestMatchOp` | AI | Returns the 0-based index of the best-matching candidate for a query |
| `AIRerankOp` | AI | Returns a permutation of candidate indices, best first |
