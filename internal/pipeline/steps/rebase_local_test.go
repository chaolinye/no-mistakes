package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/db"
	"github.com/kunchenguid/no-mistakes/internal/git"
	"github.com/kunchenguid/no-mistakes/internal/pipeline"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// TestRebaseStep_LocalOnlyRepoIntegratesWorkingDefaultBranch verifies the
// rebase step on a local-only repository. There is no origin remote anywhere:
// the operator's working repo IS the upstream, and the step must fetch the
// working default branch into the run worktree's origin/<default> tracking ref
// and rebase the feature branch onto it, exactly as it would against a network
// remote.
func TestRebaseStep_LocalOnlyRepoIntegratesWorkingDefaultBranch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Operator's working repo: local-only, default branch main.
	work := t.TempDir()
	gitCmd(t, work, "init", "--initial-branch=main")
	gitCmd(t, work, "config", "user.name", "test")
	gitCmd(t, work, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(work, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, work, "add", "-A")
	gitCmd(t, work, "commit", "-m", "base")

	// Feature branch diverges from main.
	gitCmd(t, work, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, work, "add", "-A")
	gitCmd(t, work, "commit", "-m", "feature")
	featureHead := gitCmd(t, work, "rev-parse", "HEAD")
	baseSHA := gitCmd(t, work, "rev-parse", "HEAD~1")

	// Main advances with a commit the feature branch does not have.
	gitCmd(t, work, "checkout", "main")
	if err := os.WriteFile(filepath.Join(work, "main.txt"), []byte("main advancement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, work, "add", "-A")
	gitCmd(t, work, "commit", "-m", "main advancement")
	mainTip := gitCmd(t, work, "rev-parse", "HEAD")

	// The run worktree: carved from the gate (no origin remote), checked out
	// detached at the submitted feature head. The user pushes the feature
	// branch to the no-mistakes gate remote, so the gate carries the commit.
	bare := filepath.Join(t.TempDir(), "gate.git")
	gitCmd(t, "", "init", "--bare", bare)
	gitCmd(t, work, "remote", "add", "no-mistakes", bare)
	gitCmd(t, work, "push", "no-mistakes", "refs/heads/feature:refs/heads/feature")
	wt := filepath.Join(t.TempDir(), "wt")
	if err := git.WorktreeAdd(ctx, bare, wt, featureHead); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	sctx := &pipeline.StepContext{
		Ctx:         context.Background(),
		Run:         &db.Run{ID: "run-1", RepoID: "repo-1", Branch: "refs/heads/feature", HeadSHA: featureHead, BaseSHA: baseSHA},
		Repo:        &db.Repo{ID: "repo-1", WorkingPath: work, UpstreamURL: "", ForkURL: "", DefaultBranch: "main"},
		EvidenceDir: filepath.Join(t.TempDir(), "evidence", "run-1"),
		WorkDir:     wt,
		Agent:       &mockAgent{name: "test"},
		Config:      &config.Config{Agent: types.AgentClaude, Commands: config.Commands{}},
		DB:          database,
		Log:         func(s string) { t.Log(s) },
		LogChunk:    func(s string) {},
		LogFile:     func(s string) {},
	}

	outcome, err := (&RebaseStep{}).Execute(sctx)
	if err != nil {
		t.Fatalf("rebase on local-only repo failed: %v", err)
	}
	if outcome != nil && outcome.NeedsApproval {
		t.Fatalf("unexpected approval: %s", outcome.Findings)
	}

	// The feature branch must now carry the local main advancement: the rebase
	// integrated the working repo's default branch, not the empty gate.
	// merge-base --is-ancestor exits 0 when mainTip is an ancestor of HEAD.
	if _, err := git.Run(ctx, wt, "merge-base", "--is-ancestor", mainTip, "HEAD"); err != nil {
		t.Fatalf("local main tip %s is not an ancestor of the rebased feature head: %v", mainTip, err)
	}
	// The feature change survives the rebase.
	if _, err := os.Stat(filepath.Join(wt, "feature.txt")); err != nil {
		t.Fatalf("feature file missing after rebase: %v", err)
	}
}
