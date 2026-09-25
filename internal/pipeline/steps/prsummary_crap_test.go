package steps

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/db"
	"github.com/kunchenguid/no-mistakes/internal/scm"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

const crapTestHeadSHA = "craphead0000000000000000000000000000000000"

func crapFindingsJSON(t *testing.T, report *types.CrapReport, headSHA string) string {
	t.Helper()
	raw, err := json.Marshal(types.Findings{
		Summary:       "CRAP (changed scope): 12 functions evaluated, 1 over threshold, worst 118.0",
		Tested:        []string{"typescript"},
		TestedHeadSHA: headSHA,
		Crap:          report,
		Items: []types.Finding{{
			ID:       "crap-1",
			Severity: types.FindingSeverityError,
			Category: types.FindingCategoryCrap,
			File:     "src/billing/pricing.ts",
			Line:     42,
			Action:   types.ActionAskUser,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func crapStepWithFindings(t *testing.T, findingsJSON string) ([]*db.StepResult, map[string][]*db.StepRound) {
	t.Helper()
	steps := []*db.StepResult{{
		ID:           "c1",
		StepName:     types.CustomGateStepName(types.StepLint, "crap"),
		Status:       types.StepStatusAwaitingApproval,
		FindingsJSON: &findingsJSON,
	}}
	return steps, map[string][]*db.StepRound{}
}

func sampleCrapReport() *types.CrapReport {
	return &types.CrapReport{
		Thresholds: map[string]float64{"typescript": 30},
		Scope:      "changed",
		Evaluated:  12,
		Unmeasured: 4,
		Worst:      118,
		Languages:  []string{"typescript"},
		Entries: []types.CrapEntry{
			{File: "src/billing/pricing.ts", Name: "pricingFor", Line: 42, Language: "typescript", Complexity: 12, Coverage: 0.45, Score: 118, Over: true},
			{File: "src/order/route.ts", Name: "routeOrder", Line: 15, Language: "typescript", Complexity: 7, Coverage: 1.0, Score: 7},
		},
	}
}

func TestBuildPipelineSummary_RendersCrapSection(t *testing.T) {
	t.Parallel()
	findingsJSON := crapFindingsJSON(t, sampleCrapReport(), crapTestHeadSHA)
	steps, rounds := crapStepWithFindings(t, findingsJSON)

	body, _ := BuildPipelineSummaryFor(steps, rounds, crapTestHeadSHA, scm.ProviderGitHub)

	for _, want := range []string{
		"### Code Quality (CRAP)",
		"Thresholds typescript 30",
		"12 functions evaluated",
		"worst 118.0",
		"4 unmeasured",
		"| Function | File | CC | Cov | CRAP |",
		"`pricingFor`",
		"`src/billing/pricing.ts:42`",
		"**118.0**",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("PR body missing %q:\n%s", want, body)
		}
	}
}

func TestBuildPipelineSummary_NoCrapStepRendersNothing(t *testing.T) {
	t.Parallel()
	steps := []*db.StepResult{{ID: "r1", StepName: types.StepReview, Status: types.StepStatusCompleted}}
	body, _ := BuildPipelineSummaryFor(steps, nil, crapTestHeadSHA, scm.ProviderGitHub)
	if strings.Contains(body, "Code Quality (CRAP)") {
		t.Fatalf("a run without the CRAP step must not gain a section:\n%s", body)
	}
}

func TestPipelineAttestation_CarriesCrapAtMatchingHead(t *testing.T) {
	t.Parallel()
	findingsJSON := crapFindingsJSON(t, sampleCrapReport(), crapTestHeadSHA)
	steps, rounds := crapStepWithFindings(t, findingsJSON)

	attestationJSON := buildPipelineAttestation(steps, rounds, crapTestHeadSHA)
	if !strings.Contains(attestationJSON, `"crap"`) {
		t.Fatalf("attestation must carry the crap field:\n%s", attestationJSON)
	}
	for _, want := range []string{`"evaluated":12`, `"unmeasured":4`, `"worst":118`, `"typescript":30`} {
		if !strings.Contains(attestationJSON, want) {
			t.Fatalf("attestation missing %q:\n%s", want, attestationJSON)
		}
	}
}

func TestPipelineAttestation_OmitsCrapAtDifferentHead(t *testing.T) {
	t.Parallel()
	findingsJSON := crapFindingsJSON(t, sampleCrapReport(), "someOtherHead")
	steps, rounds := crapStepWithFindings(t, findingsJSON)

	attestationJSON := buildPipelineAttestation(steps, rounds, crapTestHeadSHA)
	if strings.Contains(attestationJSON, `"crap"`) {
		t.Fatalf("a report measured at another head must be omitted from the attestation:\n%s", attestationJSON)
	}
}

func TestCrapStepDisplayName(t *testing.T) {
	t.Parallel()
	name := types.CustomGateStepName(types.StepLint, "crap")
	if got := stepDisplayName(name); got != "CRAP" {
		t.Fatalf("stepDisplayName(%q) = %q, want CRAP", name, got)
	}
	// A repository-declared gate keeps its own label.
	other := types.CustomGateStepName(types.StepLint, "arch-fitness")
	if got := stepDisplayName(other); got == "CRAP" {
		t.Fatal("a declared gate must not be labelled CRAP")
	}
}
