package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckoutNewAndCommitAndDiffStats(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	initRepo(t, tmp)
	if err := CheckoutNew("evolve/test"); err != nil {
		t.Fatalf("checkout new branch: %v", err)
	}
	current := runGit(t, "branch", "--show-current")
	if strings.TrimSpace(current) != "evolve/test" {
		t.Fatalf("expected evolve/test branch, got %q", current)
	}

	if err := os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	files, lines, err := DiffStats()
	if err != nil {
		t.Fatalf("diff stats: %v", err)
	}
	if files < 1 || lines < 1 {
		t.Fatalf("expected non-zero diff stats, got files=%d lines=%d", files, lines)
	}

	if err := Commit("test commit"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	msg := strings.TrimSpace(runGit(t, "log", "-1", "--pretty=%s"))
	if msg != "test commit" {
		t.Fatalf("unexpected commit message: %q", msg)
	}
}

func TestResetHardCleansWorkingTree(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	initRepo(t, tmp)
	if err := os.WriteFile("tracked.txt", []byte("changed\n"), 0644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	if err := os.WriteFile("temp.txt", []byte("untracked\n"), 0644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	ResetHard()

	if _, err := os.Stat("temp.txt"); !os.IsNotExist(err) {
		t.Fatalf("expected untracked file to be removed")
	}
	body, err := os.ReadFile("tracked.txt")
	if err != nil {
		t.Fatalf("read tracked file: %v", err)
	}
	if string(body) != "seed\n" {
		t.Fatalf("expected tracked file to be restored, got %q", string(body))
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, "init")
	runGit(t, "config", "user.name", "tester")
	runGit(t, "config", "user.email", "tester@example.com")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("seed\n"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "init")
}

func runGit(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v (%s)", args, err, string(out))
	}
	return string(out)
}
