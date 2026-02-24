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

// ClassifyFailure performs failure classification with strong Go coverage.
// Goal: avoid "unknown_failure" for common Go-project verification failures.
func ClassifyFailure(res CommandResult) string {
	cmdLower := strings.ToLower(strings.TrimSpace(res.Command))
	textRaw := res.Stdout + "\n" + res.Stderr
	text := strings.ToLower(textRaw)

	// --- Universal / infra first (highest priority) ---

	// Security/integrity should stop repair early.
	if hasAny(text,
		"checksum mismatch",
		"security error",
		"sum.golang.org",
		"verifying module:",
		"go: verification failed",
	) {
		return "security_integrity"
	}

	// Timeout-like signals (tool-level text; command context timeout may be handled elsewhere too).
	if hasAny(text,
		"test timed out after",
		"context deadline exceeded",
		"deadline exceeded",
		"timed out",
	) {
		// Keep this conservative; generic timeout is useful for policy decisions.
		return "timeout_failure"
	}

	if hasAny(text,
		"command not found",
		"executable file not found",
		"not recognized as an internal or external command",
	) {
		return "env_command_missing"
	}

	// "no such file or directory" is broad; classify before generic compile/test paths.
	if hasAny(text,
		"no such file or directory",
		"cannot find the path specified",
	) {
		return "env_missing_path"
	}

	if hasAny(text,
		"dial tcp",
		"tls handshake timeout",
		"temporary failure in name resolution",
		"i/o timeout",
		"connection refused",
		"network is unreachable",
		"connection reset by peer",
		"no route to host",
	) {
		return "env_network"
	}

	// --- Go-specific dependency / module resolution / manifest issues ---

	// go.mod / go.sum sync or readonly drift.
	if hasAny(text,
		"missing go.sum entry",
		"updates to go.mod needed",
		"updates to go.sum needed",
		"go: updates to go.mod needed; to update it:",
		"go: updates to go.sum needed",
	) {
		return "dependency_manifest_missing"
	}

	// Missing required dependency for an import/package.
	if hasAny(text,
		"no required module provides package",
		"cannot find module providing package",
		"cannot find package", // older GOPATH/tooling style, still useful
	) {
		return "dependency_resolution"
	}

	// go.mod syntax/parse errors (often LLM broke go.mod formatting/content).
	if hasAny(text,
		"go: errors parsing go.mod",
		"go.mod:", // paired with parser messages below
	) && hasAny(text,
		"usage: require module/path v",
		"usage: replace",
		"usage: exclude",
		"unknown directive",
		"repeated go statement",
		"invalid go version",
		"malformed module path",
		"expected",
	) {
		return "dependency_manifest_invalid"
	}

	// Fetch/auth/download/proxy issues.
	if hasAny(text,
		"go: downloading",
		"authentication required",
		"unable to access",
		"repository not found",
		"403 forbidden",
		"401 unauthorized",
		"proxyconnect tcp",
		"module lookup disabled by goproxy=off",
		"unrecognized import path",
	) {
		return "dependency_fetch"
	}

	// go command usage errors often indicate malformed verify command or invalid flags.
	if strings.HasPrefix(strings.TrimSpace(text), "usage: go ") ||
		hasAny(text, "go help test", "unknown command", "flag provided but not defined: -") && strings.Contains(cmdLower, "go ") {
		return "verify_command_invalid"
	}

	// --- Static analysis / vet / lint-ish ---

	if strings.Contains(cmdLower, " vet") || strings.HasPrefix(cmdLower, "go vet") || hasAny(text, "vet:") {
		return "vet_failure"
	}

	// --- Go test/runtime failures ---

	// Panics and runtime crashes during tests.
	if hasAny(text,
		"panic:",
		"fatal error:",
		"runtime error:",
	) {
		return "test_failure"
	}

	// Canonical go test failure markers.
	if hasAny(text,
		"--- fail:",
		"fail\t", // go test package fail line
		"fail ",  // fallback
		"--- panic:",
	) && (strings.Contains(cmdLower, "go test") || strings.Contains(text, "testing.t")) {
		return "test_failure"
	}

	// Common assertion-style outputs from Go test frameworks.
	if hasAny(text,
		"expected:",
		"actual  :",
		"not equal:",
		"received unexpected error",
		"assertion failed",
		"want ",
		"got ",
	) && strings.Contains(cmdLower, "go test") {
		return "test_failure"
	}

	// --- Compile/build/typecheck failures (Go-centric patterns) ---

	if hasAny(text,
		"undefined:",
		"cannot use ",
		"too many arguments in call",
		"not enough arguments in call",
		"missing argument in conversion to",
		"invalid operation:",
		"cannot assign to",
		"declared and not used:",
		"imported and not used:",
		"redeclared in this block",
		"method has no receiver",
		"cannot range over",
		"does not implement",
		"type mismatch",
		"syntax error:",
		"unexpected newline",
		"unexpected eof",
		"build failed",
		"[build failed]",
		"failed to build",
	) {
		return "compile_failure"
	}

	// Generic "FAIL" in Go build/test output but not matched above.
	if strings.Contains(cmdLower, "go ") && hasAny(text, "go test", "go build", "build failed", "[build failed]", "fail\t") {
		// Prefer compile if build markers exist; otherwise test.
		if hasAny(text, "build failed", "[build failed]") {
			return "compile_failure"
		}
		return "test_failure"
	}

	// --- Fallbacks with command-aware heuristics ---

	// If command is go vet and we got here, still classify as vet_failure.
	if strings.HasPrefix(cmdLower, "go vet") {
		return "vet_failure"
	}

	// If command is go test and we got here, classify by strongest remaining hints.
	if strings.HasPrefix(cmdLower, "go test") {
		// A non-zero go test without recognizable output is still more useful as test_failure than unknown.
		return "test_failure"
	}

	// If command is go build/list/mod and failed, classify as compile/dependency depending on subcommand.
	if strings.HasPrefix(cmdLower, "go mod ") {
		return "dependency_manifest_invalid"
	}
	if strings.HasPrefix(cmdLower, "go list") {
		return "dependency_resolution"
	}
	if strings.HasPrefix(cmdLower, "go build") {
		return "compile_failure"
	}

	return "unknown_failure"
}

func hasAny(s string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(s, n) {
			return true
		}
	}
	return false
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
