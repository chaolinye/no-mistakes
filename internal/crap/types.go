// Package crap implements CRAP (Change Risk Anti-Patterns) scoring for the
// pipeline's crap step: a per-function metric combining cyclomatic complexity
// with test coverage, evaluated against a per-language threshold.
//
// The package is deliberately I/O-free: every parser takes report bytes and
// returns normalized Function values, and the step orchestrating the feature
// (internal/pipeline/steps/crap.go) owns the config, the git diff, and the
// shell commands. Keeping the metric engine pure makes it golden-testable and
// keeps the language differences contained to one directory.
package crap

import (
	"math"
	"slices"
)

// Measure is the coverage unit a Function's coverage fraction is measured in.
type Measure string

const (
	// MeasureLine counts executed vs. executable source lines.
	MeasureLine Measure = "line"
	// MeasureBranch counts exercised vs. total branches (JaCoCo).
	MeasureBranch Measure = "branch"
	// MeasureStatement counts executed vs. total instrumented statements
	// (Istanbul coverage-final.json).
	MeasureStatement Measure = "statement"
)

// Language identifiers used by Function.Language and by config thresholds.
const (
	LanguagePython     = "python"
	LanguageJava       = "java"
	LanguageJavaScript = "javascript"
	LanguageTypeScript = "typescript"
	LanguageCommand    = "command"
)

// Function is one scoreable unit: a function or method with its cyclomatic
// complexity and its coverage measured in Measure units.
type Function struct {
	// Language is one of the Language* constants; it selects the effective
	// threshold and the report parser that produced the entry.
	Language string
	// File is a repository-relative path, always "/"-separated.
	File string
	// Name is the function/method name (qualified per parser where useful).
	Name string
	// StartLine and EndLine are 1-based inclusive source lines. StartLine 0
	// means the report did not carry a source line (e.g. JaCoCo without
	// debug info); such entries cannot match a changed-line scope.
	StartLine int
	EndLine   int
	// Complexity is the cyclomatic complexity; a primitive function is 1.
	Complexity int
	// Covered and Total are the measured coverage units. Total 0 means the
	// function is unmeasured: it neither passes nor fails the threshold gate,
	// but it is counted in the result's Unmeasured report.
	Covered int
	Total   int
	// Measure is the unit Covered/Total are counted in.
	Measure Measure
}

// Coverage returns the function's coverage fraction in [0,1] and whether it is
// measurable. Unmeasured functions (Total 0) report false.
func (f Function) Coverage() (float64, bool) {
	if f.Total <= 0 {
		return 0, false
	}
	return float64(f.Covered) / float64(f.Total), true
}

// Score computes the CRAP score of a function with the given cyclomatic
// complexity and coverage fraction:
//
//	CRAP = CC^2 * (1 - coverage)^3 + CC
//
// Coverage is the fraction of the function's measured units that were
// exercised, in [0,1]. A fully covered function scores exactly CC; an
// uncovered primitive function (CC 1) scores 2.
func Score(complexity int, coverage float64) float64 {
	cc := float64(complexity)
	return cc*cc*math.Pow(1-coverage, 3) + cc
}

// Scored pairs a Function with its computed CRAP score.
type Scored struct {
	Function Function
	Score    float64
}

// Result is the outcome of Evaluate.
type Result struct {
	// Functions is the full in-scope set that was measured.
	Functions []Function
	// Over lists every in-scope, measured function whose CRAP strictly
	// exceeds its language's effective threshold, worst first.
	Over []Scored
	// Evaluated counts measured, in-scope functions.
	Evaluated int
	// Unmeasured counts in-scope functions the reports could not measure
	// (Total 0). They are reported, never silently dropped, and never gated.
	Unmeasured int
	// Worst is the highest CRAP score across measured in-scope functions, or
	// 0 when none were measured.
	Worst float64
	// Thresholds is the per-language effective threshold map used, keyed by
	// the Language* identifiers that appeared in the scored set.
	Thresholds map[string]float64
}

// OverThreshold reports whether the function's CRAP strictly exceeds limit.
// The comparison is strict so that "score equals threshold" passes, matching
// the convention of crap4j, crap4ts and pytest-crap.
func OverThreshold(f Function, limit float64) bool {
	coverage, ok := f.Coverage()
	if !ok {
		return false
	}
	return Score(f.Complexity, coverage) > limit
}

// Evaluate computes the Result for a function set under per-language
// thresholds. Functions whose language has no threshold entry use the given
// fallback. In-scope filtering (changed lines, test files, ignore patterns) is
// the step's job; Evaluate assumes the set already is in scope.
func Evaluate(fns []Function, thresholds map[string]float64, fallback float64) Result {
	res := Result{
		Functions:  fns,
		Over:       nil,
		Thresholds: make(map[string]float64),
	}
	seen := make(map[string]bool)
	for _, f := range fns {
		limit, ok := thresholds[f.Language]
		if !ok {
			limit = fallback
		}
		if !seen[f.Language] {
			res.Thresholds[f.Language] = limit
			seen[f.Language] = true
		}
		coverage, measurable := f.Coverage()
		if !measurable {
			res.Unmeasured++
			continue
		}
		res.Evaluated++
		score := Score(f.Complexity, coverage)
		if score > res.Worst {
			res.Worst = score
		}
		if score > limit {
			res.Over = append(res.Over, Scored{Function: f, Score: score})
		}
	}
	sortOver(res.Over)
	return res
}

// sortOver sorts over-threshold findings worst first (stable tie-break by
// file then line so output is deterministic).
func sortOver(over []Scored) {
	slices.SortStableFunc(over, func(a, b Scored) int {
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		if a.Function.File != b.Function.File {
			if a.Function.File < b.Function.File {
				return -1
			}
			return 1
		}
		switch {
		case a.Function.StartLine < b.Function.StartLine:
			return -1
		case a.Function.StartLine > b.Function.StartLine:
			return 1
		default:
			return 0
		}
	})
}
