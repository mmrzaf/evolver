package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mmrzaf/evolver/internal/apply"
	"github.com/mmrzaf/evolver/internal/config"
	"github.com/mmrzaf/evolver/internal/ghapi"
	"github.com/mmrzaf/evolver/internal/gitops"
	"github.com/mmrzaf/evolver/internal/llm/gemini"
	"github.com/mmrzaf/evolver/internal/logging"
	"github.com/mmrzaf/evolver/internal/plan"
	"github.com/mmrzaf/evolver/internal/policy"
	"github.com/mmrzaf/evolver/internal/repoctx"
	"github.com/mmrzaf/evolver/internal/runstate"
	"github.com/mmrzaf/evolver/internal/security"
	"github.com/mmrzaf/evolver/internal/verify"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() (err error) {
	startedAt := time.Now()
	changed := false
	summary := ""

	cfg := config.Load()
	if cfg.Workdir != "" && cfg.Workdir != "." && cfg.Logging.File != "" && !filepath.IsAbs(cfg.Logging.File) {
		cfg.Logging.File = filepath.Join(cfg.Workdir, cfg.Logging.File)
	}
	closeLogger, err := logging.Configure(cfg.Logging)
	if err != nil {
		return fmt.Errorf("configure logger: %w", err)
	}
	defer func() {
		if closeErr := closeLogger(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	slog.Info("evolver run started",
		"provider", cfg.Provider,
		"mode", cfg.Mode,
		"model", cfg.Model,
		"workdir", cfg.Workdir,
	)
	defer func() {
		fields := []any{
			"changed", changed,
			"summary", summary,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		}
		if err != nil {
			fields = append(fields, "error", err)
			slog.Error("evolver run failed", fields...)
			return
		}
		slog.Info("evolver run finished", fields...)
	}()

	if err := logStep("change_workdir", func() error { return os.Chdir(cfg.Workdir) }); err != nil {
		return err
	}
	if err := logStep("policy_bootstrap", func() error { return policy.Bootstrap(cfg) }); err != nil {
		return err
	}

	var unlock func()
	if err := logStep("acquire_lock", func() error {
		lockFn, lockErr := runstate.AcquireLock(cfg.Reliability.LockFile, time.Duration(cfg.Reliability.LockStaleMinutes)*time.Minute)
		if lockErr != nil {
			return lockErr
		}
		unlock = lockFn
		return nil
	}); err != nil {
		return err
	}
	defer unlock()

	var recorder *runstate.Recorder
	if err := logStep("init_runstate_recorder", func() error {
		r, recorderErr := runstate.NewRecorder(cfg.Reliability.StateFile, cfg.Reliability.RunLogFile)
		if recorderErr != nil {
			return recorderErr
		}
		recorder = r
		return nil
	}); err != nil {
		return err
	}
	if err := logStep("record_run_start", func() error { return recorder.Start() }); err != nil {
		return err
	}
	defer func() {
		finishErr := recorder.Finish(changed, summary, err)
		if err == nil && finishErr != nil {
			err = finishErr
		}
	}()

	var repo *repoctx.Context
	if err := logStep("gather_repo_context", func() error {
		repoContext, gatherErr := repoctx.Gather(cfg)
		if gatherErr != nil {
			return gatherErr
		}
		repo = repoContext
		return nil
	}); err != nil {
		return err
	}
	slog.Info("repository context ready", "files", len(repo.Files), "excerpts", len(repo.Excerpts))

	var (
		p      *plan.Plan
		client *gemini.Client
	)

	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "gemini":
		client = gemini.NewClient(os.Getenv("GEMINI_API_KEY"), cfg.Model)
		if err := logStep("generate_plan_gemini", func() error {
			planResult, planErr := client.GeneratePlan(repo, cfg)
			if planErr != nil {
				return planErr
			}
			p = planResult
			return nil
		}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
	slog.Info("plan generated", "files", len(p.Files), "has_changelog", p.ChangelogEntry != "", "has_roadmap_update", p.RoadmapUpdate != "")

	// If the LLM proposes no changes, we still might have bootstrap changes to commit.
	if len(p.Files) == 0 && p.ChangelogEntry == "" && p.RoadmapUpdate == "" {
		slog.Info("plan proposed no direct file changes")
		dirty, derr := gitops.HasChanges()
		if derr == nil && !dirty {
			summary = "No changes proposed"
			slog.Info("run ended with no changes", "summary", summary)
			setOutput("changed", "false")
			setOutput("summary", summary)
			return nil
		}
		if strings.TrimSpace(p.Summary) == "" {
			p.Summary = "Bootstrap evolver scaffolding"
		}
	}

	if cfg.Security.SecretScan {
		if err := logStep("security_scan_plan", func() error { return security.ScanPlan(p) }); err != nil {
			return err
		}
	}
	if err := logStep("validate_paths", func() error { return plan.ValidatePaths(p, cfg) }); err != nil {
		return err
	}

	branchName := fmt.Sprintf("evolve/%s", time.Now().Format("2006-01-02-150405"))
	if cfg.Mode == "pr" {
		if err := logStep("git_checkout_branch", func() error { return gitops.CheckoutNew(branchName) }); err != nil {
			return err
		}
	}

	if err := logStep("apply_plan", func() error { return apply.Execute(p) }); err != nil {
		return err
	}
	if err := logStep("append_changelog", func() error { return policy.AppendChangelog(p.ChangelogEntry) }); err != nil {
		return err
	}
	if p.RoadmapUpdate != "" {
		if err := logStep("update_roadmap", func() error { return policy.UpdateRoadmap(p.RoadmapUpdate) }); err != nil {
			return err
		}
	}

	stats, err := computeAndCheckBudget(cfg)
	if err != nil {
		gitops.ResetHard()
		return err
	}
	if stats.FilesChanged == 0 && stats.LinesChanged == 0 && stats.NewFiles == 0 {
		summary = "No changes produced"
		slog.Info("run ended with no produced changes", "summary", summary)
		setOutput("changed", "false")
		setOutput("summary", summary)
		return nil
	}

	if err := logStep("verify_with_repair", func() error {
		return verifyWithRepair(cfg, repo, client, p)
	}); err != nil {
		gitops.ResetHard()
		return err
	}

	// Recompute final stats after any repair edits/actions.
	stats, err = computeAndCheckBudget(cfg)
	if err != nil {
		gitops.ResetHard()
		return err
	}

	if strings.TrimSpace(p.Summary) == "" {
		p.Summary = "evolver changes"
	}
	if err := logStep("git_commit", func() error { return gitops.Commit(p.Summary) }); err != nil {
		return err
	}

	if cfg.Mode == "pr" {
		if err := logStep("git_push_branch", func() error { return gitops.Push(branchName) }); err != nil {
			return err
		}
		var url string
		if err := logStep("create_pull_request", func() error {
			prURL, prErr := ghapi.CreatePR(branchName, p.Summary, generatePRBody(p, stats.FilesChanged, stats.LinesChanged, stats.NewFiles))
			if prErr != nil {
				return prErr
			}
			url = prURL
			return nil
		}); err != nil {
			return err
		}
		slog.Info("pull request created", "url", url)
		setOutput("pr_url", url)
	} else {
		if err := logStep("git_push_head", func() error { return gitops.Push("HEAD") }); err != nil {
			return err
		}
	}

	setOutput("changed", "true")
	setOutput("summary", p.Summary)
	changed = true
	summary = p.Summary
	return nil
}

type diffStats struct {
	FilesChanged int
	LinesChanged int
	NewFiles     int
}

func computeAndCheckBudget(cfg *config.Config) (diffStats, error) {
	slog.Info("computing diff stats")
	filesChanged, linesChanged, err := gitops.DiffStats()
	if err != nil {
		return diffStats{}, err
	}
	newFiles, err := gitops.NewFilesCount()
	if err != nil {
		return diffStats{}, err
	}
	stats := diffStats{FilesChanged: filesChanged, LinesChanged: linesChanged, NewFiles: newFiles}
	slog.Info("diff stats computed", "files_changed", stats.FilesChanged, "lines_changed", stats.LinesChanged, "new_files", stats.NewFiles)

	if stats.FilesChanged > cfg.Budgets.MaxFilesChanged || stats.LinesChanged > cfg.Budgets.MaxLinesChanged || stats.NewFiles > cfg.Budgets.MaxNewFiles {
		slog.Error("budget exceeded; resetting working tree",
			"files_changed", stats.FilesChanged,
			"lines_changed", stats.LinesChanged,
			"new_files", stats.NewFiles,
			"max_files", cfg.Budgets.MaxFilesChanged,
			"max_lines", cfg.Budgets.MaxLinesChanged,
			"max_new_files", cfg.Budgets.MaxNewFiles,
		)
		return stats, fmt.Errorf("budget exceeded: %d files, %d lines, %d new files", stats.FilesChanged, stats.LinesChanged, stats.NewFiles)
	}
	return stats, nil
}

func verifyWithRepair(cfg *config.Config, repo *repoctx.Context, client *gemini.Client, rootPlan *plan.Plan) error {
	maxAttempts := cfg.Repair.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 2
	}

	for attempt := 0; ; attempt++ {
		report, err := verify.RunCommandsReport(cfg.Commands)
		if err == nil {
			return nil
		}

		var cf *verify.CommandFailureError
		if !errors.As(err, &cf) {
			return err
		}
		failure := cf.Result
		if isTerminalVerifyFailure(failure.Kind) {
			slog.Error("verification failed with terminal kind; not attempting repair",
				"command", failure.Command,
				"exit_code", failure.ExitCode,
				"kind", failure.Kind,
			)
			return err
		}
		if attempt >= maxAttempts {
			slog.Error("verification failed and repair budget exhausted",
				"attempt", attempt,
				"max_attempts", maxAttempts,
				"command", failure.Command,
				"kind", failure.Kind,
			)
			return err
		}
		if client == nil {
			return err
		}

		slog.Warn("verification failed; starting repair attempt",
			"attempt", attempt+1,
			"max_attempts", maxAttempts,
			"command", failure.Command,
			"exit_code", failure.ExitCode,
			"kind", failure.Kind,
		)

		allowedCaps := filterRepairCapabilities(cfg.Repair.Capabilities, failure.Kind)
		repairFailureContext := formatFailureContext(report, failure)

		repairRepo := repo
		if freshRepo, gerr := repoctx.Gather(cfg); gerr == nil {
			repairRepo = freshRepo
		} else {
			slog.Warn("repair context refresh failed; using initial context", "error", gerr)
		}

		repairPlan, rerr := client.GenerateRepairPlan(repairRepo, cfg, rootPlan.Summary, repairFailureContext, allowedCaps)
		if rerr != nil {
			return fmt.Errorf("repair generation failed (attempt %d/%d): %w", attempt+1, maxAttempts, rerr)
		}
		slog.Info("repair plan generated", "attempt", attempt+1, "files", len(repairPlan.Files), "repair_actions", len(repairPlan.RepairActions))

		if cfg.Security.SecretScan {
			if err := security.ScanPlan(repairPlan); err != nil {
				return fmt.Errorf("repair plan secret scan failed: %w", err)
			}
		}
		if err := plan.ValidatePaths(repairPlan, cfg); err != nil {
			return fmt.Errorf("repair plan path validation failed: %w", err)
		}

		if err := apply.Execute(repairPlan); err != nil {
			return fmt.Errorf("repair apply failed: %w", err)
		}
		if strings.TrimSpace(repairPlan.Summary) != "" {
			rootPlan.Summary = repairPlan.Summary
		}

		if err := executeRepairActions(cfg, repairPlan.RepairActions, allowedCaps); err != nil {
			return fmt.Errorf("repair action failed: %w", err)
		}

		if _, err := computeAndCheckBudget(cfg); err != nil {
			return err
		}
	}
}

func isTerminalVerifyFailure(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "security_integrity":
		return true
	default:
		return false
	}
}

func formatFailureContext(report *verify.Report, failure verify.CommandResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Failed command (%d/%d): %s\n", failure.Index, failure.Total, failure.Command)
	fmt.Fprintf(&b, "Exit code: %d\n", failure.ExitCode)
	fmt.Fprintf(&b, "Kind: %s\n", failure.Kind)
	if strings.TrimSpace(failure.Stdout) != "" {
		b.WriteString("\nSTDOUT:\n")
		b.WriteString(trimForPrompt(failure.Stdout, 8000))
		b.WriteByte('\n')
	}
	if strings.TrimSpace(failure.Stderr) != "" {
		b.WriteString("\nSTDERR:\n")
		b.WriteString(trimForPrompt(failure.Stderr, 12000))
		b.WriteByte('\n')
	}
	if report != nil && len(report.Commands) > 0 {
		b.WriteString("\nVerification results so far:\n")
		for _, c := range report.Commands {
			status := "PASS"
			if !c.Passed {
				status = "FAIL"
			}
			fmt.Fprintf(&b, "- [%s] %s (exit=%d kind=%s)\n", status, c.Command, c.ExitCode, c.Kind)
		}
	}
	return b.String()
}

func trimForPrompt(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	keepHead := max * 2 / 3
	keepTail := max - keepHead
	return s[:keepHead] + "\n...<truncated>...\n" + s[len(s)-keepTail:]
}

func filterRepairCapabilities(all []config.RepairCapability, failureKind string) []config.RepairCapability {
	kind := strings.ToLower(strings.TrimSpace(failureKind))
	out := make([]config.RepairCapability, 0, len(all))
	for _, c := range all {
		if strings.TrimSpace(c.ID) == "" || len(c.Argv) == 0 {
			continue
		}
		if len(c.AllowedFailureKinds) == 0 {
			out = append(out, c)
			continue
		}
		for _, k := range c.AllowedFailureKinds {
			if strings.EqualFold(strings.TrimSpace(k), kind) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func executeRepairActions(cfg *config.Config, actionIDs []string, allowed []config.RepairCapability) error {
	if len(actionIDs) == 0 {
		return nil
	}
	maxActions := cfg.Repair.MaxActionsPerAttempt
	if maxActions <= 0 {
		maxActions = 2
	}
	if len(actionIDs) > maxActions {
		return fmt.Errorf("too many repair actions requested: %d > %d", len(actionIDs), maxActions)
	}

	capsByID := make(map[string]config.RepairCapability, len(allowed))
	for _, c := range allowed {
		if c.ID == "" {
			continue
		}
		if _, exists := capsByID[c.ID]; exists {
			return fmt.Errorf("duplicate repair capability id in config: %s", c.ID)
		}
		capsByID[c.ID] = c
	}

	runCounts := make(map[string]int)
	for i, rawID := range actionIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return fmt.Errorf("repair action %d has empty id", i)
		}
		cap, ok := capsByID[id]
		if !ok {
			return fmt.Errorf("repair action %q is not allowed for this failure", id)
		}
		runCounts[id]++
		if cap.MaxRunsPerAttempt > 0 && runCounts[id] > cap.MaxRunsPerAttempt {
			return fmt.Errorf("repair action %q exceeded max_runs_per_attempt (%d)", id, cap.MaxRunsPerAttempt)
		}
		if err := runRepairCapability(cap); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
	}
	return nil
}

func runRepairCapability(cap config.RepairCapability) error {
	if len(cap.Argv) == 0 {
		return fmt.Errorf("empty argv")
	}
	cwd, err := resolveSafeCapabilityCwd(cap.Cwd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cap.TimeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, cap.Argv[0], cap.Argv[1:]...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	display := strings.Join(cap.Argv, " ")
	startedAt := time.Now()
	slog.Info("repair capability command started", "id", cap.ID, "command", display, "cwd", valueOrDot(cwd), "timeout_seconds", cap.TimeoutSeconds)
	runErr := cmd.Run()
	durMS := time.Since(startedAt).Milliseconds()

	if ctx.Err() == context.DeadlineExceeded {
		slog.Error("repair capability command timed out", "id", cap.ID, "command", display, "duration_ms", durMS)
		return fmt.Errorf("timed out after %ds", cap.TimeoutSeconds)
	}
	if runErr != nil {
		res := verify.CommandResult{Command: display, Stdout: stdoutBuf.String(), Stderr: stderrBuf.String()}
		kind := verify.ClassifyFailure(res)
		slog.Error("repair capability command failed", "id", cap.ID, "command", display, "duration_ms", durMS, "kind", kind, "error", runErr)
		if kind == "security_integrity" {
			return fmt.Errorf("security-integrity failure while executing repair capability")
		}
		return runErr
	}
	slog.Info("repair capability command succeeded", "id", cap.ID, "command", display, "duration_ms", durMS)
	return nil
}

func resolveSafeCapabilityCwd(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" || cwd == "." {
		return "", nil
	}
	if filepath.IsAbs(cwd) || strings.HasPrefix(cwd, "..") || strings.Contains(cwd, string(os.PathSeparator)+"..") {
		return "", fmt.Errorf("unsafe repair capability cwd: %q", cwd)
	}
	clean := filepath.Clean(cwd)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe repair capability cwd: %q", cwd)
	}
	return clean, nil
}

func valueOrDot(s string) string {
	if strings.TrimSpace(s) == "" {
		return "."
	}
	return s
}

func logStep(name string, fn func() error) error {
	startedAt := time.Now()
	slog.Info("step started", "step", name)
	if err := fn(); err != nil {
		slog.Error("step failed", "step", name, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err)
		return err
	}
	slog.Info("step succeeded", "step", name, "duration_ms", time.Since(startedAt).Milliseconds())
	return nil
}

func generatePRBody(p *plan.Plan, filesChanged, linesChanged, newFiles int) string {
	return fmt.Sprintf("## Summary\n%s\n\n## Stats\n- Files changed: %d\n- Lines changed: %d\n- New files: %d\n\n## Roadmap Update\n%s\n", p.Summary, filesChanged, linesChanged, newFiles, p.RoadmapUpdate)
}

func setOutput(key, value string) {
	out := os.Getenv("GITHUB_OUTPUT")
	if out == "" {
		return
	}

	f, err := os.OpenFile(out, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open %s: %v\n", out, err)
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "failed to close %s: %v\n", out, cerr)
		}
	}()

	delimiter := fmt.Sprintf("EVOLVER_%d", time.Now().UnixNano())
	for strings.Contains(value, delimiter) {
		delimiter = fmt.Sprintf("EVOLVER_%d", time.Now().UnixNano())
	}

	if _, err := fmt.Fprintf(f, "%s<<%s\n%s\n%s\n", key, delimiter, value, delimiter); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", out, err)
	}
}
