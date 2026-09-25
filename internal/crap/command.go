package crap

import (
	"encoding/json"
	"fmt"
)

// commandEntry is one function in the normalized JSON a crap.command or a
// user-supplied report emits on stdout. This is the escape hatch for
// languages without a built-in parser.
type commandEntry struct {
	File       string `json:"file"`
	Name       string `json:"name"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Complexity int    `json:"complexity"`
	Covered    int    `json:"covered"`
	Total      int    `json:"total"`
	Measure    string `json:"measure"`
}

type commandReport struct {
	Functions []commandEntry `json:"functions"`
}

// ParseCommandJSON parses the normalized crap-report JSON contract. Every
// field is required (a missing or mistyped field fails closed); Total 0 is the
// explicit "unmeasurable" marker and is carried through. The Language is
// LanguageCommand unless the report sets it via file extension conventions the
// step applies; callers may override with SetLanguage.
func ParseCommandJSON(data []byte) ([]Function, error) {
	var report commandReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse normalized crap report: %w", err)
	}
	fns := make([]Function, 0, len(report.Functions))
	for i, e := range report.Functions {
		if e.File == "" {
			return nil, fmt.Errorf("crap report entry %d: file must not be empty", i)
		}
		if e.Complexity < 1 {
			return nil, fmt.Errorf("crap report entry %d (%s): complexity must be >= 1", i, e.File)
		}
		measure := Measure(e.Measure)
		switch measure {
		case MeasureLine, MeasureBranch, MeasureStatement:
		default:
			return nil, fmt.Errorf("crap report entry %d (%s): measure %q is not one of line|branch|statement", i, e.File, e.Measure)
		}
		fns = append(fns, Function{
			Language:   LanguageCommand,
			File:       normalizeReportPath(e.File),
			Name:       e.Name,
			StartLine:  e.StartLine,
			EndLine:    e.EndLine,
			Complexity: e.Complexity,
			Covered:    e.Covered,
			Total:      e.Total,
			Measure:    measure,
		})
	}
	return fns, nil
}
