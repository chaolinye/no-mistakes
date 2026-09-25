package steps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/crap"
	"github.com/kunchenguid/no-mistakes/internal/pipeline"
	"github.com/kunchenguid/no-mistakes/internal/pipeline/steps/internal/stepstest"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// crapTestEnv builds a repo with one committed python source file and its
// reports, returning a ready step context.
type crapTestEnv struct {
	sctx   *pipeline.StepContext
	step   *CrapStep
	dir    string
	head   string
	source string
}

const crapRadon = `{
  "src/pricing.py": [
    {"type": "function", "name": "pricing_for", "lineno": 1, "col_offset": 0, "endline": 12, "complexity": 12, "rank": "C"}
  ]
}`

// Executed 1..5 of the function's 12 lines: coverage 5/12, CRAP ~40.6 at CC 12.
const crapCoverage = `{
  "files": {
    "src/pricing.py": {
      "executed_lines": [1, 2, 3, 4, 5],
      "missing_lines": [6, 7, 8, 9, 10, 11, 12]
    }
  }
}`

func newCrapTestEnv(t *testing.T, cfg config.Crap) crapTestEnv {
	t.Helper()
	dir, base, _ := stepstest.SetupGitRepo(t)

	source := "def pricing_for(x):\n"
	for i := 2; i <= 12; i++ {
		source += "    x += 1\n"
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "pricing.py"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	stepstest.GitCmd(t, dir, "add", "-A")
	stepstest.GitCmd(t, dir, "commit", "-m", "add pricing")
	head := stepstest.GitCmd(t, dir, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(dir, "radon.json"), []byte(crapRadon), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "coverage.json"), []byte(crapCoverage), 0o644); err != nil {
		t.Fatal(err)
	}

	sctx := stepstest.NewTestContext(t, nil, dir, base, head, config.Commands{})
	step := &CrapStep{
		Gate: config.Gate{
			Name:  config.GateNameCrap,
			After: types.StepLint,
			Kind:  config.GateKindCrap,
			Crap:  &cfg,
		},
		Config: cfg,
	}
	return crapTestEnv{sctx: sctx, step: step, dir: dir, head: head, source: source}
}

func pythonCrapConfig(threshold float64) config.Crap {
	return config.Crap{
		Enabled:             true,
		Scope:               "changed",
		MaxFindings:         20,
		RequireFreshReports: false,
		Languages:           []string{crap.LanguagePython},
		Lang: map[string]config.CrapLang{
			crap.LanguagePython: {
				Threshold:  threshold,
				Complexity: "radon.json",
				Coverage:   "coverage.json",
			},
		},
	}
}

func TestCrapStep_ParksWhenOverThreshold(t *testing.T) {
	env := newCrapTestEnv(t, pythonCrapConfig(30))

	outcome, err := env.step.Execute(env.sctx)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !outcome.NeedsApproval {
		t.Fatal("a function over threshold must park for approval")
	}
	if outcome.AutoFixable {
		t.Fatal("a CRAP threshold must never auto-repair on the pipeline's own initiative")
	}
	findings, err := types.ParseFindingsJSON(outcome.Findings)
	if err != nil {
		t.Fatalf("parse findings: %v", err)
	}
	if len(findings.Items) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings.Items))
	}
	item := findings.Items[0]
	if item.Category != types.FindingCategoryCrap || item.Action != types.ActionAskUser ||
		item.Severity != types.FindingSeverityError {
		t.Fatalf("finding routing = %+v", item)
	}
	if item.File != "src/pricing.py" || item.Line != 1 {
		t.Fatalf("finding location = %s:%d", item.File, item.Line)
	}
	// The description must carry both numbers an operator needs to decide
	// between adding tests and splitting the function.
	if !strings.Contains(item.Description, "complexity 12") || !strings.Contains(item.Description, "coverage 42%") {
		t.Fatalf("description missing CC/coverage: %q", item.Description)
	}
	if findings.Crap == nil {
		t.Fatal("findings must carry the machine-readable CRAP report")
	}
	if findings.Crap.Evaluated != 1 || findings.Crap.Unmeasured != 0 || len(findings.Crap.Entries) != 1 {
		t.Fatalf("crap report = %+v", findings.Crap)
	}
	if findings.TestedHeadSHA != env.head {
		t.Fatalf("tested head = %q, want %q", findings.TestedHeadSHA, env.head)
	}
}

func TestCrapStep_PassesUnderThreshold(t *testing.T) {
	env := newCrapTestEnv(t, pythonCrapConfig(100))

	outcome, err := env.step.Execute(env.sctx)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.NeedsApproval {
		t.Fatal("a pass must not park")
	}
	findings, err := types.ParseFindingsJSON(outcome.Findings)
	if err != nil {
		t.Fatalf("parse findings: %v", err)
	}
	if len(findings.Items) != 0 {
		t.Fatalf("a pass must carry no findings, got %+v", findings.Items)
	}
	if findings.Crap == nil || findings.Crap.Evaluated != 1 {
		t.Fatalf("a pass must still record what it measured: %+v", findings.Crap)
	}
}

func TestCrapStep_MissingReportFailsClosed(t *testing.T) {
	env := newCrapTestEnv(t, pythonCrapConfig(30))
	if err := os.Remove(filepath.Join(env.dir, "radon.json")); err != nil {
		t.Fatal(err)
	}

	if _, err := env.step.Execute(env.sctx); err == nil {
		t.Fatal("a missing complexity report must fail the step, never pass it")
	}
}

func TestCrapStep_MalformedReportFailsClosed(t *testing.T) {
	env := newCrapTestEnv(t, pythonCrapConfig(30))
	if err := os.WriteFile(filepath.Join(env.dir, "radon.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := env.step.Execute(env.sctx); err == nil {
		t.Fatal("a malformed report must fail the step, never pass it")
	}
}

func TestCrapStep_OutOfScopeFunctionIsNotScored(t *testing.T) {
	env := newCrapTestEnv(t, pythonCrapConfig(30))

	// Move the default branch to the pricing commit, then change only an
	// unrelated file: the branch base is now the pricing commit, so the python
	// function is not in the changed line scope and nothing is scored.
	env.advanceDefaultBranchToHead(t)
	other := filepath.Join(env.dir, "docs.txt")
	if err := os.WriteFile(other, []byte("docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stepstest.GitCmd(t, env.dir, "add", "-A")
	stepstest.GitCmd(t, env.dir, "commit", "-m", "docs only")
	env.sctx.Run.HeadSHA = stepstest.GitCmd(t, env.dir, "rev-parse", "HEAD")

	outcome, err := env.step.Execute(env.sctx)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.NeedsApproval {
		t.Fatal("an unchanged function must not be scored")
	}
	findings, _ := types.ParseFindingsJSON(outcome.Findings)
	if findings.Crap == nil || findings.Crap.Evaluated != 0 {
		t.Fatalf("evaluated = %+v, want 0 in-scope functions", findings.Crap)
	}
}

func TestCrapStep_TestFilesExcludedByDefault(t *testing.T) {
	env := newCrapTestEnv(t, pythonCrapConfig(30))
	env.advanceDefaultBranchToHead(t)

	testFile := "src/test_pricing.py"
	if err := os.WriteFile(filepath.Join(env.dir, "src", "test_pricing.py"), []byte("def test_x():\n    pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stepstest.GitCmd(t, env.dir, "add", "-A")
	stepstest.GitCmd(t, env.dir, "commit", "-m", "add test file")
	env.sctx.Run.HeadSHA = stepstest.GitCmd(t, env.dir, "rev-parse", "HEAD")

	radon := `{"` + testFile + `": [{"type":"function","name":"test_x","lineno":1,"col_offset":0,"endline":2,"complexity":9,"rank":"C"}]}`
	cov := `{"files": {"` + testFile + `": {"executed_lines": [], "missing_lines": [1,2]}}}`
	if err := os.WriteFile(filepath.Join(env.dir, "radon.json"), []byte(radon), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.dir, "coverage.json"), []byte(cov), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := env.step.Execute(env.sctx)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.NeedsApproval {
		t.Fatal("test files must be excluded from scoring by default")
	}
}

func TestCrapStep_ThresholdIsPerLanguage(t *testing.T) {
	// A java threshold that the same function would trip if it were java; the
	// python default must not be affected by it.
	cfg := pythonCrapConfig(30)
	cfg.Languages = []string{crap.LanguagePython, crap.LanguageJava}
	cfg.Lang[crap.LanguageJava] = config.CrapLang{Threshold: 8, Jacoco: "target/site/jacoco/jacoco.xml"}
	env := newCrapTestEnv(t, cfg)

	outcome, err := env.step.Execute(env.sctx)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	findings, _ := types.ParseFindingsJSON(outcome.Findings)
	if findings.Crap.Thresholds[crap.LanguagePython] != 30 {
		t.Fatalf("python threshold = %v, want 30", findings.Crap.Thresholds[crap.LanguagePython])
	}
	if _, ok := findings.Crap.Thresholds[crap.LanguageJava]; ok {
		t.Fatal("an unscored language must not appear in the applied thresholds")
	}
}

// advanceDefaultBranchToHead moves the repository's default branch to the
// current HEAD so the branch base the step resolves is this commit, leaving
// only the test's own next commit in the changed-line scope.
func (e crapTestEnv) advanceDefaultBranchToHead(t *testing.T) {
	t.Helper()
	stepstest.GitCmd(t, e.dir, "branch", "-f", "main", "HEAD")
}
