package verify

import (
	"errors"
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

func TestRunCommandsReportFailureIncludesStructuredResult(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	cmd := os.Args[0] + " -test.run=TestVerifyHelperProcess -- fail"

	report, err := RunCommandsReport([]string{cmd})
	if err == nil {
		t.Fatalf("expected failure")
	}
	if report == nil || len(report.Commands) != 1 {
		t.Fatalf("expected report with one command, got %#v", report)
	}

	var cf *CommandFailureError
	if !errors.As(err, &cf) {
		t.Fatalf("expected CommandFailureError, got %T: %v", err, err)
	}
	if cf.Result.Command != cmd {
		t.Fatalf("unexpected command in failure result: %q", cf.Result.Command)
	}
	if cf.Result.Passed {
		t.Fatalf("expected failed command result")
	}
	if cf.Result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code")
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

func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name string
		in   CommandResult
		want string
	}{
		{
			name: "compile",
			in:   CommandResult{Command: "go test ./...", Stderr: "undefined: Foo"},
			want: "compile_failure",
		},
		{
			name: "vet",
			in:   CommandResult{Command: "go vet ./...", Stderr: "vet: unreachable code"},
			want: "vet_failure",
		},
		{
			name: "env missing command",
			in:   CommandResult{Command: "foo", Stderr: "executable file not found in $PATH"},
			want: "env_command_missing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyFailure(tc.in); got != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, got)
			}
		})
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
