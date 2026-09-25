package steps

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/crap"
	"github.com/kunchenguid/no-mistakes/internal/git"
	"github.com/kunchenguid/no-mistakes/internal/pipeline"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// maxCrapReportBytes bounds a single report so a runaway or hostile file
// cannot exhaust daemon memory before parsing.
const maxCrapReportBytes = 64 << 20

// CrapStep is the built-in CRAP (Change Risk Anti-Patterns) scoring check. It
// is minted as a gate anchored after lint, so it rides the run's pinned extra
// step list: the thresholds and report paths a run was created with are what
// recovery executes, never a since-changed default branch.
//
// The step is a deterministic gate. It reads report files (optionally
// generating them with per-language collect commands), computes each in-scope
// function's CRAP score, and parks with one ask-user finding per function over
// the effective threshold. It never repairs on its own initiative; answering
// the park with fix authorizes one agent repair round, exactly as a
// repository-declared gate does.
type CrapStep struct {
	Gate   config.Gate
	Config config.Crap
}

func (s *CrapStep) Name() types.StepName { return s.Gate.StepName() }

func (s *CrapStep) Execute(sctx *pipeline.StepContext) (*pipeline.StepOutcome, error) {
	if err := assertPipelineHeadContinuity(sctx, s.Name()); err != nil {
		return nil, err
	}
	fixSummary, err := s.runFixTurn(sctx)
	if err != nil {
		return nil, err
	}

	// assess re-runs the collectors, so a fix round scores the repaired
	// worktree's regenerated reports rather than the stale ones that parked it.
	result, err := s.assess(sctx)
	if err != nil {
		return nil, err
	}

	report := buildCrapReport(s.Config, result)
	summary := crapSummaryLine(s.Config, result)
	sctx.Log(summary)
	findingsJSON, err := json.Marshal(Findings{
		Items:         crapFindings(result, s.Config.MaxFindings),
		Summary:       summary,
		Tested:        sortedKeys(result.Thresholds),
		TestedHeadSHA: sctx.Run.HeadSHA,
		Crap:          report,
	})
	if err != nil {
		return nil, fmt.Errorf("encode crap findings: %w", err)
	}
	if len(result.Over) == 0 {
		return &pipeline.StepOutcome{Findings: string(findingsJSON), FixSummary: fixSummary}, nil
	}
	return &pipeline.StepOutcome{
		NeedsApproval: true,
		// A CRAP threshold states a repository rule. Whether the change should
		// be altered to satisfy it (add tests, split the function) is the
		// author's call, so the pipeline parks instead of repairing; answering
		// the park with fix is that authorization.
		AutoFixable: false,
		Findings:    string(findingsJSON),
		FixSummary:  fixSummary,
	}, nil
}

// assess runs the collectors, loads the reports, and scores the in-scope
// functions. Every failure path is fail-closed: an unreadable, missing, stale,
// or unparsable report must never read as "nothing over threshold".
func (s *CrapStep) assess(sctx *pipeline.StepContext) (crap.Result, error) {
	if err := s.runCollectors(sctx); err != nil {
		return crap.Result{}, err
	}

	baseSHA := resolveBranchBaseSHA(sctx.Ctx, sctx.WorkDir, sctx.Run.BaseSHA, sctx.Repo.DefaultBranch)
	headSHA := sctx.Run.HeadSHA
	changed, changedPaths, err := s.changedScope(sctx, baseSHA, headSHA)
	if err != nil {
		return crap.Result{}, err
	}

	// Only allowed files decide which languages are needed and which reports
	// must carry data: a change to nothing but test files must not demand a
	// report for the language they happen to be written in.
	allowedChanged := make([]string, 0, len(changedPaths))
	for _, file := range changedPaths {
		if s.fileAllowed(sctx, file) {
			allowedChanged = append(allowedChanged, file)
		}
	}

	fns, err := s.loadFunctions(sctx, changed, allowedChanged)
	if err != nil {
		return crap.Result{}, err
	}

	scoped := make([]crap.Function, 0, len(fns))
	for _, fn := range fns {
		if s.Config.Scope != "all" && !changed.InScope(fn) {
			continue
		}
		if !s.fileAllowed(sctx, fn.File) {
			continue
		}
		scoped = append(scoped, fn)
	}
	if err := s.assertReportsCovered(scoped, allowedChanged); err != nil {
		return crap.Result{}, err
	}

	return crap.Evaluate(scoped, s.Config.ThresholdsFor(languagesOf(scoped)), config.DefaultCrapThresholdFallback), nil
}

// fileAllowed applies the repository's ignore patterns and, unless the config
// opts in, drops test files: scoring test code with CRAP punishes the suite
// that is doing the covering.
func (s *CrapStep) fileAllowed(sctx *pipeline.StepContext, file string) bool {
	if matchesAnyIgnorePattern(file, sctx.Config.IgnorePatterns) {
		return false
	}
	if !s.Config.IncludeTests && isTestFile(file) {
		return false
	}
	return true
}

// runCollectors executes each language's optional collect command after the
// run-scoped dependency preparation, matching the configured-command path Test
// and Lint use. A non-zero exit, timeout, or cancellation fails the step.
func (s *CrapStep) runCollectors(sctx *pipeline.StepContext) error {
	if s.Config.Command != "" {
		return nil // the command is the report source; loadFunctions runs it
	}
	for _, language := range s.Config.Languages {
		lang := s.Config.Lang[language]
		command := strings.TrimSpace(lang.Collect)
		if command == "" {
			continue
		}
		if err := ensurePrepared(sctx, s.Name()); err != nil {
			return fmt.Errorf("prepare crap dependencies: %w", err)
		}
		sctx.Log(fmt.Sprintf("collecting %s CRAP reports: %s", language, command))
		output, exitCode, err := runStepShellCommand(sctx, command)
		if err != nil {
			return fmt.Errorf("run crap collect for %s: %w", language, err)
		}
		logConfiguredCommandOutput(sctx, output, s.Name())
		if exitCode != 0 {
			return fmt.Errorf("crap collect for %s exited %d; its reports were not produced", language, exitCode)
		}
	}
	return nil
}

// changedScope returns the added/modified line ranges and the changed file
// list for the run head against its base. Under scope: all the line ranges are
// empty and the caller does not consult them; the file list still names the
// files a configured language must have data for.
func (s *CrapStep) changedScope(sctx *pipeline.StepContext, baseSHA, headSHA string) (crap.ChangedLines, []string, error) {
	rangeSpec := baseSHA + ".." + headSHA
	raw, err := git.RunRaw(sctx.Ctx, sctx.WorkDir, "diff", "--name-only", "-z", rangeSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("list changed files for crap: %w", err)
	}
	paths := changedPathList(string(raw))

	if s.Config.Scope == "all" {
		return crap.ChangedLines{}, paths, nil
	}
	diffOut, err := git.Run(sctx.Ctx, sctx.WorkDir, "diff", "-U0", rangeSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("diff changed lines for crap: %w", err)
	}
	changed, err := crap.ParseDiffHunks(diffOut)
	if err != nil {
		return nil, nil, fmt.Errorf("parse changed lines for crap: %w", err)
	}
	return changed, paths, nil
}

// loadFunctions builds the normalized function set from the configured
// providers. Only languages with something in scope are loaded, so enabling
// python and java does not demand a JaCoCo report on a python-only change.
func (s *CrapStep) loadFunctions(sctx *pipeline.StepContext, changed crap.ChangedLines, allowedChanged []string) ([]crap.Function, error) {
	if s.Config.Command != "" {
		return s.commandFunctions(sctx)
	}

	need := func(language string) bool {
		if !languageConfigured(s.Config.Languages, language) {
			return false
		}
		if s.Config.Scope == "all" {
			return true
		}
		return pathsHaveLanguage(allowedChanged, language)
	}

	var fns []crap.Function
	if need(crap.LanguagePython) {
		lang := s.Config.Lang[crap.LanguagePython]
		radon, err := s.readReport(sctx, lang.Complexity)
		if err != nil {
			return nil, fmt.Errorf("read python complexity report: %w", err)
		}
		coverage, err := s.readReport(sctx, lang.Coverage)
		if err != nil {
			return nil, fmt.Errorf("read python coverage report: %w", err)
		}
		parsed, err := crap.ParsePython(radon, coverage)
		if err != nil {
			return nil, err
		}
		fns = append(fns, parsed...)
	}
	if need(crap.LanguageJava) {
		lang := s.Config.Lang[crap.LanguageJava]
		report, err := s.readReport(sctx, lang.Jacoco)
		if err != nil {
			return nil, fmt.Errorf("read java jacoco report: %w", err)
		}
		parsed, err := crap.ParseJacoco(report)
		if err != nil {
			return nil, err
		}
		fns = append(fns, parsed...)
	}
	if need(crap.LanguageJavaScript) || need(crap.LanguageTypeScript) {
		// Istanbul and ESLint both cover JavaScript and TypeScript, so the pair
		// is loaded once and split by file extension.
		lang := s.Config.Lang[crap.LanguageTypeScript]
		if !need(crap.LanguageTypeScript) {
			lang = s.Config.Lang[crap.LanguageJavaScript]
		}
		coverage, err := s.readReport(sctx, lang.Coverage)
		if err != nil {
			return nil, fmt.Errorf("read istanbul coverage report: %w", err)
		}
		complexity, err := s.readReport(sctx, lang.Complexity)
		if err != nil {
			return nil, fmt.Errorf("read eslint complexity report: %w", err)
		}
		parsed, err := crap.ParseIstanbul(coverage, sctx.WorkDir)
		if err != nil {
			return nil, err
		}
		report, err := crap.ParseESLintComplexity(complexity, sctx.WorkDir)
		if err != nil {
			return nil, err
		}
		merged, err := crap.ApplyComplexity(parsed, report)
		if err != nil {
			return nil, err
		}
		for i := range merged {
			merged[i].Language = crap.LanguageFromFile(merged[i].File)
		}
		fns = append(fns, merged...)
	}
	return fns, nil
}

// commandFunctions runs crap.command and parses its normalized JSON, assigning
// each entry's language from its file extension so per-language thresholds
// still apply; an unknown extension uses the command language and the fallback
// threshold.
func (s *CrapStep) commandFunctions(sctx *pipeline.StepContext) ([]crap.Function, error) {
	if err := ensurePrepared(sctx, s.Name()); err != nil {
		return nil, fmt.Errorf("prepare crap dependencies: %w", err)
	}
	sctx.Log(fmt.Sprintf("running crap command: %s", s.Config.Command))
	output, exitCode, err := runStepShellCommand(sctx, s.Config.Command)
	if err != nil {
		return nil, fmt.Errorf("run crap command: %w", err)
	}
	logConfiguredCommandOutput(sctx, output, s.Name())
	if exitCode != 0 {
		return nil, fmt.Errorf("crap command exited %d; the normalized report was not produced", exitCode)
	}
	fns, err := crap.ParseCommandJSON([]byte(output))
	if err != nil {
		return nil, err
	}
	for i := range fns {
		if language := crap.LanguageFromFile(fns[i].File); language != "" {
			fns[i].Language = language
		}
	}
	return fns, nil
}

// readReport reads a report from inside the run worktree. Config validation
// keeps the path repository-relative; this re-checks containment so a report
// path can never escape the worktree.
func (s *CrapStep) readReport(sctx *pipeline.StepContext, rel string) ([]byte, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return nil, fmt.Errorf("no report path configured")
	}
	if filepath.IsAbs(rel) {
		return nil, fmt.Errorf("report path %q must stay inside the repository", rel)
	}
	full := filepath.Join(sctx.WorkDir, filepath.FromSlash(rel))
	clean, err := filepath.Abs(full)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", rel, err)
	}
	root, err := filepath.Abs(sctx.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve worktree: %w", err)
	}
	if clean != root && !strings.HasPrefix(clean, root+string(filepath.Separator)) {
		return nil, fmt.Errorf("report path %q escapes the worktree", rel)
	}
	info, err := os.Stat(clean)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	if info.Size() > maxCrapReportBytes {
		return nil, fmt.Errorf("%s is %d bytes, over the %d byte report limit", rel, info.Size(), maxCrapReportBytes)
	}
	if err := s.assertFresh(sctx, rel, clean); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return data, nil
}

// assertFresh enforces require_fresh_reports: a report older than this run's
// test step describes pre-change code, so gating on it would certify the wrong
// tree. The test step is the report producer of record; when it did not run,
// there is nothing to compare against and the check is skipped.
func (s *CrapStep) assertFresh(sctx *pipeline.StepContext, rel, full string) error {
	if !s.Config.RequireFreshReports || sctx.DB == nil {
		return nil
	}
	steps, err := sctx.DB.GetStepsByRun(sctx.Run.ID)
	if err != nil {
		return fmt.Errorf("read step history for report freshness: %w", err)
	}
	var testStartedAt int64
	for _, sr := range steps {
		if sr.StepName == types.StepTest && sr.StartedAt != nil {
			testStartedAt = *sr.StartedAt
		}
	}
	if testStartedAt == 0 {
		return nil
	}
	info, err := os.Stat(full)
	if err != nil {
		return fmt.Errorf("stat %s: %w", rel, err)
	}
	if info.ModTime().Unix() < testStartedAt {
		return fmt.Errorf("%s is older than this run's test step; regenerate the report or set crap.require_fresh_reports: false", rel)
	}
	return nil
}

// assertReportsCovered refuses a configured language that has in-scope changed
// files but produced no parsable functions at all: that is a wrong path, wrong
// globs, or a report for another tree, never a clean result. A file that
// legitimately declares no functions is tolerated; total silence for the
// language is not.
func (s *CrapStep) assertReportsCovered(scoped []crap.Function, allowedChanged []string) error {
	for _, language := range s.Config.Languages {
		if languageActive(language, scoped) {
			continue
		}
		if s.Config.Scope == "all" {
			return fmt.Errorf("crap: %s is enabled but its reports contained no functions", language)
		}
		if pathsHaveLanguage(allowedChanged, language) {
			return fmt.Errorf("crap: %s has changed files in scope but its reports contained no functions for them; check the report paths", language)
		}
	}
	return nil
}

// runFixTurn services a `fix` answer to the CRAP park with one agent repair
// round, returning its commit summary.
func (s *CrapStep) runFixTurn(sctx *pipeline.StepContext) (string, error) {
	if !sctx.Fixing {
		return "", nil
	}
	requirement := "Every function in scope must score at or below its language's CRAP threshold: " +
		formatThresholds(s.Config)
	prompt := fmt.Sprintf(
		`Fix the functions this repository's CRAP (Change Risk Anti-Patterns) gate flagged.

%s

Rules:
- Raise coverage for under-tested functions, or simplify over-complex ones - whichever is the honest root-cause fix.
- Do not weaken, disable, or narrow the gate itself, its thresholds, or its report sources.
- Do not delete working behavior to lower a score; removing a path is correct only when it is genuinely unnecessary.
- Prefer real tests that exercise observable behavior over assertions on implementation text.
- Re-run the checks that produce the coverage and complexity reports before finishing.
- Return JSON with a single "summary" field when you are done.
- The summary must be one concise sentence fragment suitable for a git commit subject.
- Keep the summary under 10 words.%s`,
		requirement,
		executionContextPromptSection(sctx.WorkDir)+roundHistoryPromptSection(sctx)+userIntentPromptSection(sctx),
	)
	if sctx.PreviousFindings != "" {
		prompt += "\n\nPrevious CRAP findings to address:\n" + sanitizedPreviousFindingsForPrompt(sctx.PreviousFindings)
	}
	return executeFixMode(sctx, s.Name(), fixExecutionOptions{
		LogMessage:      "asking agent to fix CRAP findings...",
		Prompt:          prompt,
		ErrorPrefix:     "agent fix crap",
		FallbackSummary: "reduce CRAP scores",
	})
}

// ---- summary and findings ----

func crapSummaryLine(cfg config.Crap, result crap.Result) string {
	line := fmt.Sprintf("CRAP (%s scope): %d functions evaluated, %d over threshold, worst %.1f",
		cfg.Scope, result.Evaluated, len(result.Over), result.Worst)
	if result.Unmeasured > 0 {
		line += fmt.Sprintf(", %d unmeasured", result.Unmeasured)
	}
	return line
}

func crapFindings(result crap.Result, max int) []Finding {
	items := make([]Finding, 0, min(max, len(result.Over)))
	for i, scored := range result.Over {
		if i >= max {
			break
		}
		coverage, _ := scored.Function.Coverage()
		name := scored.Function.Name
		if name == "" {
			name = filepath.Base(scored.Function.File)
		}
		items = append(items, Finding{
			ID:       fmt.Sprintf("crap-%d", i+1),
			Severity: types.FindingSeverityError,
			Category: types.FindingCategoryCrap,
			File:     scored.Function.File,
			Line:     scored.Function.StartLine,
			Description: fmt.Sprintf("%s() CRAP %.1f > %g (complexity %d, %s coverage %.0f%%)",
				name, scored.Score, result.Thresholds[scored.Function.Language],
				scored.Function.Complexity, scored.Function.Measure, coverage*100),
			Action: types.ActionAskUser,
		})
	}
	return items
}

func buildCrapReport(cfg config.Crap, result crap.Result) *types.CrapReport {
	entries := make([]types.CrapEntry, 0, min(cfg.MaxFindings, len(result.Over)))
	for _, scored := range result.Over {
		if len(entries) >= cfg.MaxFindings {
			break
		}
		coverage, _ := scored.Function.Coverage()
		entries = append(entries, types.CrapEntry{
			File:       scored.Function.File,
			Name:       scored.Function.Name,
			Line:       scored.Function.StartLine,
			Language:   scored.Function.Language,
			Complexity: scored.Function.Complexity,
			Coverage:   coverage,
			Score:      scored.Score,
			Over:       true,
		})
	}
	return &types.CrapReport{
		Thresholds: result.Thresholds,
		Scope:      cfg.Scope,
		Evaluated:  result.Evaluated,
		Unmeasured: result.Unmeasured,
		Worst:      result.Worst,
		Languages:  sortedKeys(result.Thresholds),
		Entries:    entries,
	}
}

// ---- small helpers ----

func languageConfigured(languages []string, want string) bool {
	for _, language := range languages {
		if language == want {
			return true
		}
	}
	return false
}

func languageActive(language string, fns []crap.Function) bool {
	for _, fn := range fns {
		if fn.Language == language {
			return true
		}
	}
	return false
}

// languagesOf returns the sorted, unique languages present in the function set,
// which is what the effective threshold map is keyed by.
func languagesOf(fns []crap.Function) []string {
	seen := make(map[string]bool, 4)
	for _, fn := range fns {
		seen[fn.Language] = true
	}
	out := make([]string, 0, len(seen))
	for language := range seen {
		out = append(out, language)
	}
	sort.Strings(out)
	return out
}

// languageHasScope reports whether the changed-line set touches a file of the
// language's extensions.
func languageHasScope(language string, changed crap.ChangedLines) bool {
	for file := range changed {
		if languageOfFile(file) == language {
			return true
		}
	}
	return false
}

// pathsHaveLanguage reports whether any of the paths belongs to the language.
func pathsHaveLanguage(paths []string, language string) bool {
	for _, file := range paths {
		if languageOfFile(file) == language {
			return true
		}
	}
	return false
}

// languageOfFile maps a file path to a configured CRAP language, or "" when the
// extension is not one the built-in parsers understand.
func languageOfFile(file string) string {
	switch {
	case strings.HasSuffix(file, ".py"):
		return crap.LanguagePython
	case strings.HasSuffix(file, ".java"):
		return crap.LanguageJava
	case strings.HasSuffix(file, ".ts"), strings.HasSuffix(file, ".tsx"), strings.HasSuffix(file, ".mts"), strings.HasSuffix(file, ".cts"):
		return crap.LanguageTypeScript
	case strings.HasSuffix(file, ".js"), strings.HasSuffix(file, ".jsx"), strings.HasSuffix(file, ".mjs"), strings.HasSuffix(file, ".cjs"):
		return crap.LanguageJavaScript
	default:
		return ""
	}
}

func matchesAnyIgnorePattern(file string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchIgnorePattern(file, pattern) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func formatThresholds(cfg config.Crap) string {
	parts := make([]string, 0, len(cfg.Languages))
	for _, language := range cfg.Languages {
		parts = append(parts, fmt.Sprintf("%s <= %g", language, cfg.ThresholdFor(language)))
	}
	return strings.Join(parts, ", ")
}
