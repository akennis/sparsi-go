package library

import "strings"

// AllDescriptions returns a formatted string listing all library op descriptions,
// organized by group. Used by LibraryScanOp and the genlibdesc tool.
func AllDescriptions() string {
	groups := []struct {
		header string
		descs  []string
	}{
		{"## Math — float", []string{
			AddFloatOpDescription,
			SubFloatOpDescription,
			MulFloatOpDescription,
			DivFloatOpDescription,
			PowFloatOpDescription,
			ModFloatOpDescription,
			RoundOpDescription,
			ClampFloatOpDescription,
			SumFloatOpDescription,
			MinFloatOpDescription,
			MaxFloatOpDescription,
			PackMathOperandsOpDescription,
		}},
		{"## Math — int", []string{
			AddIntOpDescription,
			SubIntOpDescription,
			MulIntOpDescription,
			DivIntOpDescription,
			PowIntOpDescription,
			ModIntOpDescription,
			SumIntOpDescription,
			ClampIntOpDescription,
			MinIntOpDescription,
			MaxIntOpDescription,
		}},
		{"## Math — cast", []string{
			IntToFloat64OpDescription,
			Float64ToIntOpDescription,
		}},
		{"## String — cast", []string{
			Float64ToStringOpDescription,
			IntToStringOpDescription,
			BoolToStringOpDescription,
			ToStringOpDescription,
		}},
		{"## String", []string{
			StringLookupOpDescription,
			StringToLowerOpDescription,
			StringConcatOpDescription,
			StringSplitOpDescription,
			RegexMatchOpDescription,
			RegexExtractOpDescription,
		}},
		{"## Bool", []string{
			BoolNotOpDescription,
			BoolAndOpDescription,
			BoolOrOpDescription,
		}},
		{"## Predicate — float", []string{
			IfFloatGtOpDescription,
			IfFloatLtOpDescription,
			IfFloatEqOpDescription,
			IfFloatGeOpDescription,
			IfFloatLeOpDescription,
		}},
		{"## Predicate — int", []string{
			IfIntGtOpDescription,
			IfIntLtOpDescription,
			IfIntEqOpDescription,
			IfIntGeOpDescription,
			IfIntLeOpDescription,
		}},
		{"## Predicate — string", []string{
			IfStringContainsOpDescription,
			IfStringHasPrefixOpDescription,
			IfStringHasSuffixOpDescription,
			IfStringRegexMatchOpDescription,
			IfStringEqOpDescription,
		}},
		{"## Predicate — empty / range", []string{
			IfEmptyStringOpDescription,
			IfEmptySliceStringOpDescription,
			IfEmptySliceFloat64OpDescription,
			BetweenFloatOpDescription,
		}},
		{"## Select / Switch / Default", []string{
			SelectStringOpDescription,
			SelectFloat64OpDescription,
			SelectIntOpDescription,
			SelectBoolOpDescription,
			SwitchStringOpDescription,
			DefaultStringOpDescription,
			DefaultFloat64OpDescription,
			DefaultIntOpDescription,
		}},
		{"## Slice", []string{
			SliceLenOpDescription,
			SliceAtOpDescription,
			SliceFirstOpDescription,
			SliceLastOpDescription,
			SliceContainsOpDescription,
			SliceJoinOpDescription,
			SliceFilterEqOpDescription,
			SliceTopKOpDescription,
		}},
		{"## AI", []string{
			ModeSelectOpDescription,
			AIComputeStringToStringOpDescription,
			AIComputeMathOperandsToFloat64OpDescription,
			AIExtractStringSliceOpDescription,
			AIExtractMapOpDescription,
			AIParseNumberOpDescription,
			AISummarizeOpDescription,
			AIClassifyMultiLabelOpDescription,
			AIScoreOpDescription,
			AIBoolOpDescription,
			AIBestMatchOpDescription,
			AIRerankOpDescription,
			WithRepairDescription,
		}},
		{"## Time", []string{
			CityTimeOpDescription,
		}},
		{"## IO", []string{
			FileReadOpDescription,
			EnvOpDescription,
			HTTPGetOpDescription,
		}},
		{"## JSON", []string{
			JSONExtractOpDescription,
		}},
		{"## MCP", []string{
			MCPCallOpDescription,
			MCPScriptOpDescription,
		}},
	}

	var sb strings.Builder
	for i, g := range groups {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(g.header)
		sb.WriteString("\n")
		for _, d := range g.descs {
			sb.WriteString(d)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
