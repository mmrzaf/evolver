package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunCommandsSuccess(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	cmd := os.Args[0] + " -test.run=TestVerifyHelperProcess -- ok"
	if err := RunCommands([]string{cmd}); err != nil {
		t.Fatalf("expected command to succeed: %v", err)
	}
}

func TestRunCommandsFailure(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	cmd := os.Args[0] + " -test.run=TestVerifyHelperProcess -- fail"
	if err := RunCommands([]string{cmd}); err == nil {
		t.Fatalf("expected command failure to be returned")
	}
}

func TestInferCommandsByProjectType(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if got := inferCommands(); len(got) != 0 {
		t.Fatalf("expected no inferred commands in empty dir, got %#v", got)
	}

	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module x\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if got := inferCommands(); len(got) != 1 || got[0] != "go test ./..." {
		t.Fatalf("expected go test inferred command, got %#v", got)
	}
}

func TestVerifyHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for i := range args {
		if args[i] == "--" && i+1 < len(args) {
			switch args[i+1] {
			case "ok":
				os.Exit(0)
			case "fail":
				os.Exit(1)
			}
		}
	}
	os.Exit(2)
}
