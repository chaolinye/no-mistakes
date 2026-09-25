package config

import (
	"fmt"
	"path"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/crap"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// Default per-language CRAP thresholds. They come from each ecosystem's own
// tooling so a repository that enables CRAP without tuning it gets a
// defensible gate rather than an arbitrary one:
//
//   - python: pytest-crap's default.
//   - java: crap4java's CLI default 8. (The original crap4j used 30; a
//     repository that finds 8 too strict sets crap.java.threshold.)
//   - javascript/typescript: the community convention used by js-crap-score
//     and the crap4ts documentation.
const (
	DefaultCrapThresholdPython     = 30.0
	DefaultCrapThresholdJava       = 8.0
	DefaultCrapThresholdJavaScript = 30.0
	DefaultCrapThresholdTypeScript = 30.0
	// DefaultCrapThresholdFallback applies to crap.command entries whose file
	// extension maps to no known language.
	DefaultCrapThresholdFallback = 30.0
	// DefaultCrapMaxFindings bounds how many over-threshold functions become
	// findings in one round; the rest are summarized as counts.
	DefaultCrapMaxFindings = 20
)

// Default report paths per language, used when a language block omits them.
const (
	defaultCrapRadonPath        = "radon.json"
	defaultCrapCoveragePyPath   = "coverage.json"
	defaultCrapJacocoReportPath = "target/site/jacoco/jacoco.xml"
	defaultCrapESLintPath       = ".crap/eslint.json"
	defaultCrapIstanbulPath     = "coverage/coverage-final.json"
	defaultCrapThresholdMinimum = 1.0
)

// CrapRaw is the YAML representation of the CRAP step's configuration. Every
// field that executes code or steers the gate is trusted-only: the daemon
// reads this block from the trusted default-branch copy of .no-mistakes.yaml
// only, exactly like gates and commands.*.
type CrapRaw struct {
	Enabled *bool `yaml:"enabled"`
	// Threshold is the repo-wide override. Unset (nil) means "use each
	// language's default"; a language block's own threshold overrides it.
	Threshold *float64 `yaml:"threshold"`
	// Scope is "changed" (default) or "all".
	Scope string `yaml:"scope"`
	// MaxFindings bounds findings per round. Unset uses DefaultCrapMaxFindings.
	MaxFindings *int `yaml:"max_findings"`
	// IncludeTests includes test files in scoring. Default false.
	IncludeTests *bool `yaml:"include_tests"`
	// RequireFreshReports fails the step when a report predates this run's
	// test step. Default true.
	RequireFreshReports *bool `yaml:"require_fresh_reports"`
	// Languages is the explicit enable list. A language block alone also
	// enables its language.
	Languages []string `yaml:"languages"`
	// Command is the escape hatch: a shell command whose stdout is the
	// normalized crap report. Mutually exclusive with per-language report
	// paths and collect commands.
	Command    string       `yaml:"command"`
	Python     *CrapLangRaw `yaml:"python"`
	Java       *CrapLangRaw `yaml:"java"`
	JavaScript *CrapLangRaw `yaml:"javascript"`
	TypeScript *CrapLangRaw `yaml:"typescript"`
}

// CrapLangRaw is one language's report sources and optional threshold.
type CrapLangRaw struct {
	// Threshold overrides both the repo-wide threshold and the language
	// default for this language only.
	Threshold *float64 `yaml:"threshold"`
	// Collect is an optional shell command run before the reports are read.
	Collect string `yaml:"collect"`
	// Complexity and Coverage are report paths for python and javascript/
	// typescript.
	Complexity string `yaml:"complexity"`
	Coverage   string `yaml:"coverage"`
	// Jacoco is the JaCoCo XML report path for java.
	Jacoco string `yaml:"jacoco"`
}

// Crap is the resolved CRAP configuration.
type Crap struct {
	Enabled             bool
	Threshold           float64
	Scope               string
	MaxFindings         int
	IncludeTests        bool
	RequireFreshReports bool
	Languages           []string
	Command             string
	Lang                map[string]CrapLang
}

// CrapLang is one resolved language's report sources and threshold override.
type CrapLang struct {
	Threshold  float64
	Collect    string
	Complexity string
	Coverage   string
	Jacoco     string
}

// KnownCrapLanguages returns the supported language identifiers in a stable
// order, for error messages and documentation.
func KnownCrapLanguages() []string {
	return []string{crap.LanguagePython, crap.LanguageJava, crap.LanguageJavaScript, crap.LanguageTypeScript}
}

// DefaultCrapThreshold returns the built-in threshold for a language.
func DefaultCrapThreshold(language string) float64 {
	switch language {
	case crap.LanguagePython:
		return DefaultCrapThresholdPython
	case crap.LanguageJava:
		return DefaultCrapThresholdJava
	case crap.LanguageJavaScript:
		return DefaultCrapThresholdJavaScript
	case crap.LanguageTypeScript:
		return DefaultCrapThresholdTypeScript
	default:
		return DefaultCrapThresholdFallback
	}
}

// ThresholdFor resolves the effective threshold for a language: the language
// block's own value, else the repo-wide value, else the language default.
func (c Crap) ThresholdFor(language string) float64 {
	if lang, ok := c.Lang[language]; ok && lang.Threshold > 0 {
		return lang.Threshold
	}
	if c.Threshold > 0 {
		return c.Threshold
	}
	return DefaultCrapThreshold(language)
}

// ThresholdsFor returns the effective thresholds for the given languages.
func (c Crap) ThresholdsFor(languages []string) map[string]float64 {
	out := make(map[string]float64, len(languages))
	for _, language := range languages {
		out[language] = c.ThresholdFor(language)
	}
	return out
}

// rawForString returns the raw language block for a language identifier.
func (raw CrapRaw) rawForString(language string) *CrapLangRaw {
	switch language {
	case crap.LanguagePython:
		return raw.Python
	case crap.LanguageJava:
		return raw.Java
	case crap.LanguageJavaScript:
		return raw.JavaScript
	case crap.LanguageTypeScript:
		return raw.TypeScript
	default:
		return nil
	}
}

// ResolveCrap turns the raw block into its resolved form, applying defaults.
// It assumes validateCrapRaw already accepted the block.
func ResolveCrap(raw CrapRaw) Crap {
	resolved := Crap{
		Enabled:             raw.Enabled != nil && *raw.Enabled,
		Scope:               "changed",
		MaxFindings:         DefaultCrapMaxFindings,
		RequireFreshReports: true,
		Lang:                make(map[string]CrapLang),
		Command:             strings.TrimSpace(raw.Command),
	}
	if raw.Threshold != nil {
		resolved.Threshold = *raw.Threshold
	}
	if strings.TrimSpace(raw.Scope) != "" {
		resolved.Scope = strings.TrimSpace(raw.Scope)
	}
	if raw.MaxFindings != nil {
		resolved.MaxFindings = *raw.MaxFindings
	}
	if raw.IncludeTests != nil {
		resolved.IncludeTests = *raw.IncludeTests
	}
	if raw.RequireFreshReports != nil {
		resolved.RequireFreshReports = *raw.RequireFreshReports
	}

	// Explicit language list first, then any language block that was declared.
	seen := make(map[string]bool)
	add := func(language string) {
		if seen[language] {
			return
		}
		seen[language] = true
		resolved.Languages = append(resolved.Languages, language)
	}
	for _, language := range raw.Languages {
		add(strings.TrimSpace(language))
	}
	for _, language := range KnownCrapLanguages() {
		if raw.rawForString(language) != nil {
			add(language)
		}
	}

	for _, language := range KnownCrapLanguages() {
		rawLang := raw.rawForString(language)
		lang := CrapLang{
			Complexity: defaultCrapComplexityPath(language),
			Coverage:   defaultCrapCoveragePath(language),
			Jacoco:     defaultCrapJacocoPath(language),
		}
		if rawLang != nil {
			if rawLang.Threshold != nil {
				lang.Threshold = *rawLang.Threshold
			}
			lang.Collect = strings.TrimSpace(rawLang.Collect)
			if rawLang.Complexity != "" {
				lang.Complexity = rawLang.Complexity
			}
			if rawLang.Coverage != "" {
				lang.Coverage = rawLang.Coverage
			}
			if rawLang.Jacoco != "" {
				lang.Jacoco = rawLang.Jacoco
			}
		}
		resolved.Lang[language] = lang
	}
	return resolved
}

func defaultCrapComplexityPath(language string) string {
	switch language {
	case crap.LanguagePython:
		return defaultCrapRadonPath
	case crap.LanguageJavaScript, crap.LanguageTypeScript:
		return defaultCrapESLintPath
	default:
		return ""
	}
}

func defaultCrapCoveragePath(language string) string {
	switch language {
	case crap.LanguagePython:
		return defaultCrapCoveragePyPath
	case crap.LanguageJavaScript, crap.LanguageTypeScript:
		return defaultCrapIstanbulPath
	default:
		return ""
	}
}

func defaultCrapJacocoPath(language string) string {
	if language == crap.LanguageJava {
		return defaultCrapJacocoReportPath
	}
	return ""
}

// validateCrapRaw fails a config closed on values the step could not honor
// deterministically. Like validateGates it also runs on the pushed copy even
// though EffectiveRepoConfig discards a pushed crap block: the trusted-copy
// read aborts every run whose default-branch .no-mistakes.yaml fails these
// checks, so a branch carrying an invalid block fails before it merges.
func validateCrapRaw(raw CrapRaw) error {
	if raw.Threshold != nil && *raw.Threshold <= defaultCrapThresholdMinimum {
		return fmt.Errorf("crap.threshold must be > %g, got %g", defaultCrapThresholdMinimum, *raw.Threshold)
	}
	switch strings.TrimSpace(raw.Scope) {
	case "", "changed", "all":
	default:
		return fmt.Errorf("crap.scope %q must be changed or all", raw.Scope)
	}
	if raw.MaxFindings != nil && *raw.MaxFindings < 1 {
		return fmt.Errorf("crap.max_findings must be >= 1, got %d", *raw.MaxFindings)
	}

	seen := make(map[string]int, len(raw.Languages))
	for i, language := range raw.Languages {
		language = strings.TrimSpace(language)
		if !isKnownCrapLanguage(language) {
			return fmt.Errorf("crap.languages[%d] %q is not supported; valid: %s", i, language, strings.Join(KnownCrapLanguages(), ", "))
		}
		if first, dup := seen[language]; dup {
			return fmt.Errorf("crap.languages[%d] %q duplicates crap.languages[%d]", i, language, first)
		}
		seen[language] = i
	}

	commandSet := strings.TrimSpace(raw.Command) != ""
	reportSet := false
	for _, language := range KnownCrapLanguages() {
		rawLang := raw.rawForString(language)
		if rawLang == nil {
			continue
		}
		label := "crap." + language
		if rawLang.Threshold != nil && *rawLang.Threshold <= defaultCrapThresholdMinimum {
			return fmt.Errorf("%s.threshold must be > %g, got %g", label, defaultCrapThresholdMinimum, *rawLang.Threshold)
		}
		for field, value := range map[string]string{
			"complexity": rawLang.Complexity,
			"coverage":   rawLang.Coverage,
			"jacoco":     rawLang.Jacoco,
		} {
			if strings.TrimSpace(value) == "" {
				continue
			}
			reportSet = true
			if err := validateCrapReportPath(label+"."+field, value); err != nil {
				return err
			}
		}
		if strings.TrimSpace(rawLang.Collect) != "" {
			reportSet = true
		}
	}
	if commandSet && reportSet {
		return fmt.Errorf("crap.command and per-language report paths/collect are mutually exclusive; use one source")
	}

	if raw.Enabled != nil && *raw.Enabled {
		if len(seen) == 0 && !anyCrapLangBlock(raw) && !commandSet {
			return fmt.Errorf("crap.enabled requires at least one language in crap.languages, a language block, or crap.command")
		}
	}
	return nil
}

func anyCrapLangBlock(raw CrapRaw) bool {
	for _, language := range KnownCrapLanguages() {
		if raw.rawForString(language) != nil {
			return true
		}
	}
	return false
}

func isKnownCrapLanguage(language string) bool {
	for _, known := range KnownCrapLanguages() {
		if language == known {
			return true
		}
	}
	return false
}

// validateCrapReportPath keeps report paths repository-relative: an absolute
// path or a ".." segment would let a config read a file outside the worktree.
func validateCrapReportPath(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\`) {
		return fmt.Errorf("%s must be repository-relative, got absolute path %q", field, value)
	}
	if strings.Contains(value, `\`) {
		return fmt.Errorf("%s must use forward slashes, got %q", field, value)
	}
	if path.Clean(value) != value || value == "." {
		return fmt.Errorf("%s %q must be a clean repository-relative path", field, value)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return fmt.Errorf("%s %q must not escape the repository root", field, value)
		}
	}
	return nil
}

// crapGate mints the built-in CRAP step as a gate anchored immediately after
// lint, the position the plan settled on: lint/formatter have already fixed
// the file shape and line numbers the CRAP reports refer to, and push has not
// yet published anything. It returns nil when the block is disabled, so a
// repository that never opted in gets exactly today's step sequence. The gate
// carries the resolved parameters so the run pins them and recovery cannot be
// steered by a since-changed default branch (see pinnedRunGates).
func crapGate(resolved Crap) *Gate {
	if !resolved.Enabled {
		return nil
	}
	payload := resolved
	return &Gate{
		Name:  GateNameCrap,
		After: types.StepLint,
		Kind:  GateKindCrap,
		Crap:  &payload,
	}
}

// CrapGates returns the repository's extra checks with the built-in CRAP gate
// prepended when the crap block is enabled. The built-in check runs before a
// repository's own lint-anchored gates so a declared gate sees a worktree CRAP
// has already reported on, and so the order stays deterministic.
func CrapGates(raw CrapRaw, gates []Gate) []Gate {
	builtin := crapGate(ResolveCrap(raw))
	if builtin == nil {
		return copyGates(gates)
	}
	out := make([]Gate, 0, len(gates)+1)
	out = append(out, *builtin)
	out = append(out, gates...)
	return out
}
