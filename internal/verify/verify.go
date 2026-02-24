package verify

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CommandResult captures a single verification command execution.
type CommandResult struct {
	Index      int           `json:"index"`
	Total      int           `json:"total"`
	Command    string        `json:"command"`
	ExitCode   int           `json:"exit_code"`
	Stdout     string        `json:"stdout,omitempty"`
	Stderr     string        `json:"stderr,omitempty"`
	DurationMS int64         `json:"duration_ms"`
	Passed     bool          `json:"passed"`
	Kind       string        `json:"kind,omitempty"`
	Duration   time.Duration `json:"-"`
}

// Report captures the ordered results for a verification run.
type Report struct {
	Commands []CommandResult `json:"commands"`
}

// FirstFailure returns the first failing command result, if any.
func (r *Report) FirstFailure() *CommandResult {
	if r == nil {
		return nil
	}
	for i := range r.Commands {
		if !r.Commands[i].Passed {
			return &r.Commands[i]
		}
	}
	return nil
}

// CommandFailureError is returned when a verification command fails.
type CommandFailureError struct {
	Result CommandResult
}

func (e *CommandFailureError) Error() string {
	return fmt.Sprintf("command failed: %s (exit=%d, kind=%s)", e.Result.Command, e.Result.ExitCode, e.Result.Kind)
}

// RunCommands preserves the old API for callers/tests that only care about pass/fail.
func RunCommands(commands []string) error {
	_, err := RunCommandsReport(commands)
	return err
}

// RunCommandsReport executes verification commands and returns structured results.
// It stops at the first failure.
func RunCommandsReport(commands []string) (*Report, error) {
	if len(commands) == 0 {
		commands = inferCommands()
	}
	slog.Info("verification commands prepared", "count", len(commands))

	report := &Report{Commands: make([]CommandResult, 0, len(commands))}

	for i, cmdStr := range commands {
		parts := strings.Fields(cmdStr)
		if len(parts) == 0 {
			continue
		}

		startedAt := time.Now()
		slog.Info("verification command started", "index", i+1, "total", len(commands), "command", cmdStr)

		cmd := exec.Command(parts[0], parts[1:]...)

		var stdoutBuf bytes.Buffer
		var stderrBuf bytes.Buffer
		cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

		runErr := cmd.Run()
		dur := time.Since(startedAt)

		res := CommandResult{
			Index:      i + 1,
			Total:      len(commands),
			Command:    cmdStr,
			Stdout:     stdoutBuf.String(),
			Stderr:     stderrBuf.String(),
			DurationMS: dur.Milliseconds(),
			Duration:   dur,
			Passed:     runErr == nil,
		}

		if runErr == nil {
			res.ExitCode = 0
			slog.Info("verification command succeeded",
				"index", i+1, "total", len(commands), "command", cmdStr, "duration_ms", res.DurationMS)
			report.Commands = append(report.Commands, res)
			continue
		}

		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = -1
		}
		res.Kind = ClassifyFailure(res)

		slog.Error("verification command failed",
			"index", i+1,
			"total", len(commands),
			"command", cmdStr,
			"duration_ms", res.DurationMS,
			"exit_code", res.ExitCode,
			"kind", res.Kind,
			"error", runErr,
		)

		report.Commands = append(report.Commands, res)
		return report, &CommandFailureError{Result: res}
	}

	return report, nil
}

// ClassifyFailure performs a lightweight failure classification to avoid bad repair attempts.
func ClassifyFailure(res CommandResult) string {
	text := strings.ToLower(res.Stdout + "\n" + res.Stderr)

	switch {
	case strings.Contains(text, "command not found"),
		strings.Contains(text, "executable file not found"),
		strings.Contains(text, "not recognized as an internal or external command"):
		return "env_command_missing"
	case strings.Contains(text, "no such file or directory"):
		return "env_missing_path"
	case strings.Contains(text, "dial tcp"),
		strings.Contains(text, "tls handshake timeout"),
		strings.Contains(text, "temporary failure in name resolution"),
		strings.Contains(text, "i/o timeout"),
		strings.Contains(text, "connection refused"),
		strings.Contains(text, "network is unreachable"):
		return "env_network"
	case strings.Contains(text, "go: downloading"),
		strings.Contains(text, "unable to access"),
		strings.Contains(text, "authentication required"):
		return "dependency_fetch"
	case strings.Contains(text, "panic:"),
		strings.Contains(text, "--- fail:"),
		strings.Contains(text, "expected") && strings.Contains(text, "got"):
		return "test_failure"
	case strings.Contains(text, "undefined:"),
		strings.Contains(text, "cannot use"),
		strings.Contains(text, "too many arguments in call"),
		strings.Contains(text, "not enough arguments in call"),
		strings.Contains(text, "syntax error"),
		strings.Contains(text, "build failed"),
		strings.Contains(text, "compile"):
		return "compile_failure"
	case strings.Contains(strings.ToLower(res.Command), "vet"),
		strings.Contains(text, "vet:"):
		return "vet_failure"
	default:
		return "unknown_failure"
	}
}

func inferCommands() []string {
	if _, err := os.Stat("go.mod"); err == nil {
		return []string{"go test ./..."}
	}
	if _, err := os.Stat("package.json"); err == nil {
		return []string{"npm test"}
	}
	return []string{}
}
