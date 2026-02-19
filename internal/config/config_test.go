package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	c := Load()
	if c.Provider != "gemini" {
		t.Fatalf("unexpected provider: %s", c.Provider)
	}
	if c.Mode != "pr" {
		t.Fatalf("unexpected mode: %s", c.Mode)
	}
	if c.Model != "gemini-2.5-flash-lite" {
		t.Fatalf("unexpected model: %s", c.Model)
	}
	if c.Budgets.MaxFilesChanged != 10 || c.Budgets.MaxLinesChanged != 500 || c.Budgets.MaxNewFiles != 10 {
		t.Fatalf("unexpected budgets: %+v", c.Budgets)
	}
	if !c.Security.SecretScan {
		t.Fatalf("expected secret scan enabled by default")
	}
	if c.Reliability.LockStaleMinutes != 180 {
		t.Fatalf("unexpected reliability defaults: %+v", c.Reliability)
	}
}

func TestLoadFromFileAndEnvOverrides(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if err := os.MkdirAll(".evolver", 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfgYAML := []byte("provider: gemini\nmode: push\nmodel: test-model\nworkdir: /tmp/project\nbudgets:\n  max_files_changed: 3\n  max_lines_changed: 25\n  max_new_files: 2\n")
	if err := os.WriteFile(filepath.Join(".evolver", "config.yml"), cfgYAML, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("EVOLVER_PROVIDER", "gemini")
	t.Setenv("EVOLVER_MODE", "pr")
	t.Setenv("EVOLVER_MODEL", "override-model")
	t.Setenv("EVOLVER_REPO_GOAL", "Ship safely")
	t.Setenv("EVOLVER_WORKDIR", "/work/dir")
	t.Setenv("EVOLVER_MAX_FILES", "7")
	t.Setenv("EVOLVER_MAX_LINES", "99")
	t.Setenv("EVOLVER_MAX_NEW_FILES", "5")
	t.Setenv("EVOLVER_COMMANDS", "go test ./...\ngo vet ./...")
	t.Setenv("EVOLVER_ALLOW_WORKFLOWS", "true")
	t.Setenv("EVOLVER_STATE_FILE", ".evolver/custom_state.json")
	t.Setenv("EVOLVER_RUN_LOG_FILE", ".evolver/custom_runs.log")
	t.Setenv("EVOLVER_LOCK_FILE", ".evolver/custom.lock")
	t.Setenv("EVOLVER_LOCK_STALE_MINUTES", "45")

	c := Load()
	if c.Provider != "gemini" {
		t.Fatalf("expected env provider override, got %s", c.Provider)
	}
	if c.Mode != "pr" {
		t.Fatalf("expected env mode override, got %s", c.Mode)
	}
	if c.Model != "override-model" {
		t.Fatalf("expected env model override, got %s", c.Model)
	}
	if c.RepoGoal != "Ship safely" {
		t.Fatalf("expected repo goal override, got %q", c.RepoGoal)
	}
	if c.Workdir != "/work/dir" {
		t.Fatalf("expected workdir override, got %s", c.Workdir)
	}
	if c.Budgets.MaxFilesChanged != 7 || c.Budgets.MaxLinesChanged != 99 || c.Budgets.MaxNewFiles != 5 {
		t.Fatalf("expected budget overrides, got %+v", c.Budgets)
	}
	if len(c.Commands) != 2 || c.Commands[0] != "go test ./..." || c.Commands[1] != "go vet ./..." {
		t.Fatalf("unexpected commands: %#v", c.Commands)
	}
	if !c.Security.AllowWorkflowEdits {
		t.Fatalf("expected workflow edits enabled by env")
	}
	if c.Reliability.StateFile != ".evolver/custom_state.json" || c.Reliability.RunLogFile != ".evolver/custom_runs.log" || c.Reliability.LockFile != ".evolver/custom.lock" {
		t.Fatalf("unexpected reliability path overrides: %+v", c.Reliability)
	}
	if c.Reliability.LockStaleMinutes != 45 {
		t.Fatalf("unexpected reliability numeric overrides: %+v", c.Reliability)
	}
}
