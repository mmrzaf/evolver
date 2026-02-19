package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mmrzaf/evolver/internal/config"
)

func TestBootstrapCreatesFilesAndIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg := &config.Config{RepoGoal: "Harden reliability"}
	if err := Bootstrap(cfg); err != nil {
		t.Fatalf("bootstrap first run: %v", err)
	}
	if err := Bootstrap(cfg); err != nil {
		t.Fatalf("bootstrap second run should also succeed: %v", err)
	}

	mustExist := []string{".evolver/config.yml", "POLICY.md", "ROADMAP.md", "CHANGELOG.md"}
	for _, p := range mustExist {
		if _, err := os.Stat(filepath.Join(tmp, p)); err != nil {
			t.Fatalf("expected %s to exist: %v", p, err)
		}
	}

	roadmap, err := os.ReadFile("ROADMAP.md")
	if err != nil {
		t.Fatalf("read roadmap: %v", err)
	}
	if !strings.Contains(string(roadmap), "Harden reliability") {
		t.Fatalf("expected roadmap to include repo goal, got %q", string(roadmap))
	}
}

func TestAppendChangelogAndUpdateRoadmap(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if err := os.WriteFile("CHANGELOG.md", []byte("# CHANGELOG\n"), 0644); err != nil {
		t.Fatalf("seed changelog: %v", err)
	}
	if err := AppendChangelog("- add retries"); err != nil {
		t.Fatalf("append changelog: %v", err)
	}
	body, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatalf("read changelog: %v", err)
	}
	if !strings.Contains(string(body), "- add retries") {
		t.Fatalf("expected appended entry in changelog")
	}

	if err := UpdateRoadmap("# ROADMAP\n- [x] done\n"); err != nil {
		t.Fatalf("update roadmap: %v", err)
	}
	roadmap, err := os.ReadFile("ROADMAP.md")
	if err != nil {
		t.Fatalf("read roadmap: %v", err)
	}
	if string(roadmap) != "# ROADMAP\n- [x] done\n" {
		t.Fatalf("unexpected roadmap content: %q", string(roadmap))
	}
}
