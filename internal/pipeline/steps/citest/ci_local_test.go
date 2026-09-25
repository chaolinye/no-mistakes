package citest

import (
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/pipeline/steps"
	"github.com/kunchenguid/no-mistakes/internal/pipeline/steps/internal/stepstest"
)

func TestCIStep_LocalOnlyRepoSkips(t *testing.T) {
	t.Parallel()
	// A local-only repository has no PR head for CI to watch, so the CI step
	// must skip with a clear reason before constructing any forge host.
	dir, baseSHA, headSHA := stepstest.SetupGitRepo(t)
	ag := &stepstest.MockAgent{AgentName: "test"}
	sctx := stepstest.NewTestContextWithDBRecords(t, ag, dir, baseSHA, headSHA, config.Commands{})
	sctx.Repo.UpstreamURL = ""
	sctx.Repo.ForkURL = ""
	sctx.Run.PRURL = nil

	outcome, err := (&steps.CIStep{}).Execute(sctx)
	if err != nil {
		t.Fatalf("expected skip on local-only repo, got: %v", err)
	}
	if outcome == nil || !outcome.Skipped {
		t.Fatalf("expected skipped outcome, got %+v", outcome)
	}
	if !strings.Contains(outcome.SkipReason, "local-only") {
		t.Errorf("expected local-only skip reason, got %q", outcome.SkipReason)
	}
}
