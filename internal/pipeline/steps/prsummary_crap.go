package steps

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/db"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// This file owns how the built-in CRAP step's result reaches a reviewer.
//
// The machine-readable half rides the PR attestation as crap, so a consumer can
// ask "was this change's CRAP gated, at what threshold, and how bad was the
// worst function?" without parsing prose. The human half is a compact Code
// Quality section listing the worst over-threshold functions with the two
// numbers that explain them: complexity and coverage. Both read the same
// recorded CrapReport, so they can never disagree.
//
// Every accessor is fail-open: a run that never executed the CRAP step, or one
// whose payload predates this field, renders exactly as before.

// collectCrapReport returns the CRAP report recorded for the step, preferring
// the final findings and falling back to the last round that carried one (a
// step whose final findings were cleared by a fix still shows what it last
// measured).
func collectCrapReport(sr *db.StepResult, rounds []*db.StepRound) *types.CrapReport {
	if sr == nil {
		return nil
	}
	if report := crapReportFrom(sr.FindingsJSON); report != nil {
		return report
	}
	for i := len(rounds) - 1; i >= 0; i-- {
		if report := crapReportFrom(rounds[i].FindingsJSON); report != nil {
			return report
		}
	}
	return nil
}

func crapReportFrom(raw *string) *types.CrapReport {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	findings, err := types.ParseFindingsJSON(*raw)
	if err != nil {
		return nil
	}
	return findings.Crap
}

// crapStepResult returns the run's CRAP step result, if the step ran.
func crapStepResult(steps []*db.StepResult, rounds map[string][]*db.StepRound) (*db.StepResult, *types.CrapReport) {
	for _, sr := range steps {
		if sr == nil || !types.IsBuiltinCrapStep(sr.StepName) {
			continue
		}
		return sr, collectCrapReport(sr, rounds[sr.ID])
	}
	return nil, nil
}

// renderCrapSection builds the PR's Code Quality block. It is empty when there
// is nothing to say, so a run without the step adds no section.
func renderCrapSection(report *types.CrapReport, headSHA string, sr *db.StepResult) string {
	if report == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("### Code Quality (CRAP)\n\n")
	b.WriteString(crapHeadline(report))
	b.WriteString("\n\n")

	if len(report.Entries) > 0 {
		b.WriteString("| Function | File | CC | Cov | CRAP |\n")
		b.WriteString("|---|---|---:|---:|---:|\n")
		for _, entry := range report.Entries {
			name := entry.Name
			if name == "" {
				name = "-"
			}
			location := entry.File
			if entry.Line > 0 {
				location = fmt.Sprintf("%s:%d", location, entry.Line)
			}
			marker := ""
			if entry.Over {
				marker = "**"
			}
			fmt.Fprintf(&b, "| `%s` | `%s` | %d | %.0f%% | %s%.1f%s |\n",
				crapCodeSpan(name), crapCodeSpan(location), entry.Complexity, entry.Coverage*100, marker, entry.Score, marker)
		}
	}

	if note := crapNote(report, sr, headSHA); note != "" {
		b.WriteString("\n<sub>")
		b.WriteString(note)
		b.WriteString("</sub>\n")
	}
	return b.String()
}

// crapHeadline is the one-line summary above the table.
func crapHeadline(report *types.CrapReport) string {
	scope := report.Scope
	if scope == "" {
		scope = "changed"
	}
	headline := fmt.Sprintf("Thresholds %s · scope `%s` · %d functions evaluated · worst %.1f",
		formatCrapThresholds(report.Thresholds), scope, report.Evaluated, report.Worst)
	if report.Unmeasured > 0 {
		headline += fmt.Sprintf(" · %d unmeasured", report.Unmeasured)
	}
	return headline
}

// crapNote records the small print: how many entries are actually over
// threshold, how many functions could not be measured, and whether the report
// belongs to the attested head.
func crapNote(report *types.CrapReport, sr *db.StepResult, headSHA string) string {
	over := 0
	for _, entry := range report.Entries {
		if entry.Over {
			over++
		}
	}
	var parts []string
	if over > 0 {
		parts = append(parts, fmt.Sprintf("%d function(s) over threshold", over))
	} else {
		parts = append(parts, "no function over threshold")
	}
	if report.Unmeasured > 0 {
		parts = append(parts, fmt.Sprintf("%d could not be measured and were not gated", report.Unmeasured))
	}
	if sr != nil && headSHA != "" && sr.FindingsJSON != nil {
		if findings, err := types.ParseFindingsJSON(*sr.FindingsJSON); err == nil &&
			findings.TestedHeadSHA != "" && findings.TestedHeadSHA != headSHA {
			parts = append(parts, "measured at a different head; re-run to certify this one")
		}
	}
	return strings.Join(parts, " · ")
}

// crapCodeSpan renders a value safely inside a markdown code span: a backtick
// in a function or file name would otherwise break out of the span.
func crapCodeSpan(value string) string {
	return strings.ReplaceAll(value, "`", "'")
}

// formatCrapThresholds renders the effective threshold map deterministically.
func formatCrapThresholds(thresholds map[string]float64) string {
	if len(thresholds) == 0 {
		return "defaults"
	}
	languages := make([]string, 0, len(thresholds))
	for language := range thresholds {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	parts := make([]string, 0, len(languages))
	for _, language := range languages {
		parts = append(parts, fmt.Sprintf("%s %g", language, thresholds[language]))
	}
	return strings.Join(parts, ", ")
}

// attestedCrap derives the attestation's crap field from the CRAP step's
// recorded report. Like live_validation it is omitted whenever the report was
// measured at a different head than the one being attested, so a consumer
// never reads a stale verdict as current.
func attestedCrap(steps []*db.StepResult, rounds map[string][]*db.StepRound, headSHA string) *pipelineAttestationCrap {
	sr, report := crapStepResult(steps, rounds)
	if sr == nil || report == nil {
		return nil
	}
	if sr.FindingsJSON != nil {
		if findings, err := types.ParseFindingsJSON(*sr.FindingsJSON); err == nil {
			if findings.TestedHeadSHA == "" || findings.TestedHeadSHA != headSHA {
				return nil
			}
		}
	}
	return &pipelineAttestationCrap{
		Thresholds: report.Thresholds,
		Scope:      report.Scope,
		Evaluated:  report.Evaluated,
		Unmeasured: report.Unmeasured,
		Worst:      report.Worst,
		Languages:  report.Languages,
	}
}

// renderCrapSectionFor is renderCrapSection bound to the run's step results.
func renderCrapSectionFor(steps []*db.StepResult, rounds map[string][]*db.StepRound, headSHA string) string {
	sr, report := crapStepResult(steps, rounds)
	if sr == nil || report == nil {
		return ""
	}
	return renderCrapSection(report, headSHA, sr)
}
