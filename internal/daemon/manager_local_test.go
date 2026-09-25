package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/db"
	"github.com/kunchenguid/no-mistakes/internal/git"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

func TestEffectiveSkipSteps_LocalRepoForcesDeliverySkip(t *testing.T) {
	local := &db.Repo{UpstreamURL: "", ForkURL: "", DefaultBranch: "main"}
	remote := &db.Repo{UpstreamURL: "https://github.com/test/repo.git", ForkURL: ""}

	// A local-only repo always skips push/pr/ci, even when the caller asked
	// for none of them, and never duplicates an explicitly requested skip.
	got := effectiveSkipSteps(local, nil)
	want := []types.StepName{types.StepPush, types.StepPR, types.StepCI}
	if len(got) != len(want) {
		t.Fatalf("effectiveSkipSteps(local, nil) = %v, want %v", got, want)
	}
	for i, s := range want {
		if got[i] != s {
			t.Fatalf("effectiveSkipSteps(local, nil) = %v, want %v", got, want)
		}
	}

	got = effectiveSkipSteps(local, []types.StepName{types.StepPush})
	want = []types.StepName{types.StepPush, types.StepPR, types.StepCI}
	if len(got) != len(want) {
		t.Fatalf("effectiveSkipSteps(local, [push]) = %v, want %v (no duplicate push)", got, want)
	}
	for i, s := range want {
		if got[i] != s {
			t.Fatalf("effectiveSkipSteps(local, [push]) = %v, want %v", got, want)
		}
	}

	// A repo with an upstream keeps the caller's exact request.
	got = effectiveSkipSteps(remote, []types.StepName{types.StepTest})
	if len(got) != 1 || got[0] != types.StepTest {
		t.Fatalf("effectiveSkipSteps(remote, [test]) = %v, want [test]", got)
	}
	if got := effectiveSkipSteps(nil, nil); got != nil {
		t.Fatalf("effectiveSkipSteps(nil, nil) = %v, want nil", got)
	}
}

func TestFetchRunDefaultBranch_LocalRepoFetchesFromWorkingRepo(t *testing.T) {
	ctx := context.Background()

	// The operator's working repo: local-only, default branch "main" carrying
	// a trusted .no-mistakes.yaml.
	work := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, work, "init", "--initial-branch=main")
	gitCmd(t, work, "config", "user.email", "test@test.com")
	gitCmd(t, work, "config", "user.name", "Test")
	gitCmd(t, work, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".no-mistakes.yaml"),
		[]byte("commands:\n  lint: \"echo local-A\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, work, "add", ".")
	gitCmd(t, work, "commit", "-m", "local default")
	expectedSHA := gitOutput(t, work, "rev-parse", "HEAD")

	// The run worktree carved from the gate: it has no origin remote (a
	// local-only gate keeps none), only the branch that was pushed.
	bare := filepath.Join(t.TempDir(), "gate.git")
	gitCmd(t, "", "init", "--bare", bare)
	gitCmd(t, work, "remote", "add", "no-mistakes", bare)
	gitCmd(t, work, "push", "no-mistakes", "HEAD:refs/heads/main")

	wt := filepath.Join(t.TempDir(), "wt")
	if err := git.WorktreeAdd(ctx, bare, wt, expectedSHA); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	repo := &db.Repo{
		WorkingPath:   work,
		UpstreamURL:   "",
		ForkURL:       "",
		DefaultBranch: "main",
	}
	if !repo.IsLocal() {
		t.Fatal("fixture repo should be local-only")
	}

	if err := fetchRunDefaultBranch(ctx, wt, repo); err != nil {
		t.Fatalf("fetchRunDefaultBranch(local) failed: %v", err)
	}
	resolved, err := git.ResolveRef(ctx, wt, "refs/remotes/origin/main")
	if err != nil {
		t.Fatalf("resolve fetched origin/main: %v", err)
	}
	if resolved != expectedSHA {
		t.Fatalf("fetched origin/main = %s, want working-repo default %s", resolved, expectedSHA)
	}

	// The trusted config at that pinned SHA is the local default branch's own.
	trusted := loadTrustedRepoConfig(ctx, wt, resolved, "test-run")
	if trusted == nil {
		t.Fatal("expected trusted config from the local default branch")
	}
	if trusted.Commands.Lint != "echo local-A" {
		t.Fatalf("trusted lint = %q, want %q", trusted.Commands.Lint, "echo local-A")
	}
}
