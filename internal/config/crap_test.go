package config

import (
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/crap"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

func TestLoadRepoConfig_CrapParses(t *testing.T) {
	data := []byte(`crap:
  enabled: true
  scope: changed
  languages: [python, java]
  python:
    threshold: 20
    collect: "python -m radon cc -j ."
    complexity: reports/radon.json
    coverage: reports/coverage.json
  java:
    jacoco: target/site/jacoco/jacoco.xml
`)
	cfg, err := LoadRepoFromBytes(data)
	if err != nil {
		t.Fatalf("LoadRepoFromBytes: %v", err)
	}
	resolved := ResolveCrap(cfg.Crap)
	if !resolved.Enabled {
		t.Fatal("expected enabled")
	}
	if got := resolved.ThresholdFor(crap.LanguagePython); got != 20 {
		t.Fatalf("python threshold = %v, want 20", got)
	}
	// java has no explicit threshold -> its language default.
	if got := resolved.ThresholdFor(crap.LanguageJava); got != DefaultCrapThresholdJava {
		t.Fatalf("java threshold = %v, want %v", got, DefaultCrapThresholdJava)
	}
	if got := resolved.Lang[crap.LanguagePython].Collect; got == "" {
		t.Fatal("python collect not resolved")
	}
	if got := resolved.Lang[crap.LanguageJava].Jacoco; got != "target/site/jacoco/jacoco.xml" {
		t.Fatalf("java jacoco = %q", got)
	}
}

func TestResolveCrapDefaults(t *testing.T) {
	resolved := ResolveCrap(CrapRaw{Enabled: boolPtr(true), Languages: []string{crap.LanguageTypeScript}})
	if resolved.Scope != "changed" {
		t.Fatalf("scope = %q, want changed", resolved.Scope)
	}
	if resolved.MaxFindings != DefaultCrapMaxFindings {
		t.Fatalf("max_findings = %d, want %d", resolved.MaxFindings, DefaultCrapMaxFindings)
	}
	if !resolved.RequireFreshReports {
		t.Fatal("require_fresh_reports must default true")
	}
	if resolved.IncludeTests {
		t.Fatal("include_tests must default false")
	}
	if got := resolved.Lang[crap.LanguageTypeScript].Complexity; got != defaultCrapESLintPath {
		t.Fatalf("typescript complexity default = %q", got)
	}
	if got := resolved.Lang[crap.LanguageTypeScript].Coverage; got != defaultCrapIstanbulPath {
		t.Fatalf("typescript coverage default = %q", got)
	}
	if got := resolved.ThresholdFor(crap.LanguageTypeScript); got != DefaultCrapThresholdTypeScript {
		t.Fatalf("typescript threshold = %v", got)
	}
}

func TestResolveCrapGlobalThresholdOverriddenByLanguage(t *testing.T) {
	resolved := ResolveCrap(CrapRaw{
		Enabled:   boolPtr(true),
		Threshold: floatPtr(25),
		Languages: []string{crap.LanguagePython, crap.LanguageJava},
		Java:      &CrapLangRaw{Threshold: floatPtr(12)},
	})
	if got := resolved.ThresholdFor(crap.LanguagePython); got != 25 {
		t.Fatalf("python threshold = %v, want global 25", got)
	}
	if got := resolved.ThresholdFor(crap.LanguageJava); got != 12 {
		t.Fatalf("java threshold = %v, want language 12", got)
	}
}

func TestResolveCrapLanguagesIncludeDeclaredBlocks(t *testing.T) {
	// A language block alone enables its language even without the list.
	resolved := ResolveCrap(CrapRaw{Enabled: boolPtr(true), Java: &CrapLangRaw{}})
	if len(resolved.Languages) != 1 || resolved.Languages[0] != crap.LanguageJava {
		t.Fatalf("languages = %v, want [java]", resolved.Languages)
	}
}

func TestValidateCrapRaw_RejectsMalformed(t *testing.T) {
	cases := []struct {
		name string
		raw  CrapRaw
		want string
	}{
		{"threshold too low", CrapRaw{Threshold: floatPtr(1)}, "crap.threshold must be > 1"},
		{"bad scope", CrapRaw{Scope: "files"}, "crap.scope"},
		{"bad max_findings", CrapRaw{MaxFindings: intPtr(0)}, "crap.max_findings"},
		{"unknown language", CrapRaw{Languages: []string{"ruby"}}, "not supported"},
		{"duplicate language", CrapRaw{Languages: []string{"python", "python"}}, "duplicates"},
		{"lang threshold too low", CrapRaw{Python: &CrapLangRaw{Threshold: floatPtr(0.5)}}, "crap.python.threshold"},
		{"absolute report path", CrapRaw{Python: &CrapLangRaw{Coverage: "/etc/passwd"}}, "repository-relative"},
		{"escaping report path", CrapRaw{Java: &CrapLangRaw{Jacoco: "../../secret.xml"}}, "must not escape"},
		{"enabled with no source", CrapRaw{Enabled: boolPtr(true)}, "requires at least one language"},
		{"command and reports", CrapRaw{Command: "x", Python: &CrapLangRaw{Coverage: "c.json"}}, "mutually exclusive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCrapRaw(tc.raw)
			if err == nil {
				t.Fatalf("validateCrapRaw() = nil, want error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateCrapRaw() = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateCrapRaw_AcceptsValid(t *testing.T) {
	raw := CrapRaw{
		Enabled:             boolPtr(true),
		Threshold:           floatPtr(30),
		Scope:               "all",
		MaxFindings:         intPtr(5),
		RequireFreshReports: boolPtr(false),
		Languages:           []string{"python", "typescript"},
		Python:              &CrapLangRaw{Collect: "true", Complexity: "r.json", Coverage: "c.json"},
		TypeScript:          &CrapLangRaw{},
	}
	if err := validateCrapRaw(raw); err != nil {
		t.Fatalf("validateCrapRaw() = %v, want nil", err)
	}
}

func TestEffectiveRepoConfig_CrapTrustedOnly(t *testing.T) {
	pushed := &RepoConfig{Crap: CrapRaw{Enabled: boolPtr(true), Threshold: floatPtr(9999)}}
	trusted := &RepoConfig{Crap: CrapRaw{Enabled: boolPtr(true), Threshold: floatPtr(30)}}
	effective := EffectiveRepoConfig(pushed, trusted, false)
	if got := *effective.Crap.Threshold; got != 30 {
		t.Fatalf("pushed threshold leaked through: got %v, want 30", got)
	}

	// With no trusted copy at all the block is dropped entirely: a pushed
	// branch cannot enable the gate.
	effective = EffectiveRepoConfig(pushed, nil, false)
	if effective.Crap.Enabled != nil || effective.Crap.Threshold != nil {
		t.Fatal("pushed crap block must be dropped when no trusted copy exists")
	}
}

func TestEffectiveRepoConfig_CrapTrustedOnlyEvenWithAllowRepoCommands(t *testing.T) {
	pushed := &RepoConfig{Crap: CrapRaw{Enabled: boolPtr(true), Threshold: floatPtr(9999)}}
	trusted := &RepoConfig{Crap: CrapRaw{Enabled: boolPtr(true), Threshold: floatPtr(30)}}
	effective := EffectiveRepoConfig(pushed, trusted, true)
	if got := *effective.Crap.Threshold; got != 30 {
		t.Fatalf("allow_repo_commands must not expose the pushed threshold: got %v", got)
	}
}

func TestCrapGates_DisabledMintsNothing(t *testing.T) {
	gates := CrapGates(CrapRaw{}, []Gate{{Name: "arch", After: "lint", Command: "make arch"}})
	if len(gates) != 1 || gates[0].Name != "arch" {
		t.Fatalf("a disabled crap block must not mint a gate: %+v", gates)
	}
}

func TestCrapGates_EnabledMintsBuiltinBeforeDeclaredGates(t *testing.T) {
	raw := CrapRaw{Enabled: boolPtr(true), Languages: []string{"python"}}
	gates := CrapGates(raw, []Gate{{Name: "arch", After: "lint", Command: "make arch"}})
	if len(gates) != 2 {
		t.Fatalf("gates = %+v, want the builtin plus the declared one", gates)
	}
	builtin := gates[0]
	if builtin.Kind != GateKindCrap || builtin.Crap == nil {
		t.Fatalf("builtin gate = %+v", builtin)
	}
	if builtin.After != types.StepLint {
		t.Fatalf("builtin anchor = %q, want lint", builtin.After)
	}
	if builtin.Name != GateNameCrap {
		t.Fatalf("builtin name = %q", builtin.Name)
	}
}

func TestMarshalParseGates_RoundTripsBuiltinCrapGate(t *testing.T) {
	raw := CrapRaw{Enabled: boolPtr(true), Threshold: floatPtr(25), Languages: []string{"java"}}
	gates := CrapGates(raw, nil)
	payload, err := MarshalGates(gates)
	if err != nil {
		t.Fatalf("MarshalGates: %v", err)
	}
	decoded, err := ParseGates(payload)
	if err != nil {
		t.Fatalf("ParseGates: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Kind != GateKindCrap || decoded[0].Crap == nil {
		t.Fatalf("round-trip lost the builtin gate: %+v", decoded)
	}
	// The pinned payload must carry the parameters the run was created with, so
	// recovery cannot be steered by a since-changed default branch.
	if got := decoded[0].Crap.Threshold; got != 25 {
		t.Fatalf("pinned threshold = %v, want 25", got)
	}
	if got := decoded[0].Crap.ThresholdFor("java"); got != 25 {
		t.Fatalf("pinned java threshold = %v, want 25", got)
	}
}

func TestValidateGates_AcceptsBuiltinCrapAndRejectsShadowing(t *testing.T) {
	builtin := CrapGates(CrapRaw{Enabled: boolPtr(true), Languages: []string{"python"}}, nil)
	if err := validateGates(builtin); err != nil {
		t.Fatalf("validateGates(builtin) = %v, want nil", err)
	}

	shadow := []Gate{{Name: GateNameCrap, After: types.StepLint, Command: "true"}}
	if err := validateGates(shadow); err == nil {
		t.Fatal("a repository-declared gate must not shadow the builtin CRAP step")
	}
}

func TestGateUnmarshal_RejectsUserDeclaredKind(t *testing.T) {
	data := []byte("gates:\n  - name: sneaky\n    after: lint\n    kind: crap\n    command: \"true\"\n")
	if _, err := LoadRepoFromBytes(data); err == nil {
		t.Fatal("a user-declared gate must not set kind")
	}
}

func TestMerge_MintsBuiltinCrapGate(t *testing.T) {
	merged := Merge(DefaultGlobalConfig(), &RepoConfig{
		Crap: CrapRaw{Enabled: boolPtr(true), Languages: []string{"python"}},
	})
	if !merged.Crap.Enabled {
		t.Fatal("merged config must carry the enabled crap block")
	}
	found := false
	for _, gate := range merged.Gates {
		if gate.Kind == GateKindCrap {
			found = true
		}
	}
	if !found {
		t.Fatalf("merge must mint the builtin crap gate: %+v", merged.Gates)
	}
}
