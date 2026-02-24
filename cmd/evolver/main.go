package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
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

const maxRepairAttempts = 2

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

	var ctx *repoctx.Context
	if err := logStep("gather_repo_context", func() error {
		repoContext, gatherErr := repoctx.Gather(cfg)
		if gatherErr != nil {
			return gatherErr
		}
		ctx = repoContext
		return nil
	}); err != nil {
		return err
	}
	slog.Info("repository context ready", "files", len(ctx.Files), "excerpts", len(ctx.Excerpts))

	var client *gemini.Client
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "gemini":
		client = gemini.NewClient(os.Getenv("GEMINI_API_KEY"), cfg.Model)
	default:
		return fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}

	var p *plan.Plan
	if err := logStep("generate_plan", func() error {
		planResult, planErr := client.GeneratePlan(ctx, cfg)
		if planErr != nil {
			return planErr
		}
		p = planResult
		return nil
	}); err != nil {
		return err
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

	filesChanged, linesChanged, newFiles, err := computeAndValidateDiffBudget(cfg)
	if err != nil {
		gitops.ResetHard()
		return err
	}

	if err := logStep("verify_with_repair", func() error {
		return verifyWithRepair(client, ctx, cfg, p, &filesChanged, &linesChanged, &newFiles)
	}); err != nil {
		gitops.ResetHard()
		return err
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
			prURL, prErr := ghapi.CreatePR(branchName, p.Summary, generatePRBody(p, filesChanged, linesChanged, newFiles))
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

func verifyWithRepair(client *gemini.Client, ctx *repoctx.Context, cfg *config.Config, p *plan.Plan, filesChanged, linesChanged, newFiles *int) error {
	report, err := verify.RunCommandsReport(cfg.Commands)
	if err == nil {
		return nil
	}

	var failErr *verify.CommandFailureError
	if !errors.As(err, &failErr) {
		return err
	}

	if !isRepairableFailure(failErr.Result.Kind) {
		return fmt.Errorf("verification failed (%s) and is not repairable automatically: %w", failErr.Result.Kind, err)
	}

	lastFailure := failErr.Result
	for attempt := 1; attempt <= maxRepairAttempts; attempt++ {
		slog.Warn("verification failed; starting repair attempt",
			"attempt", attempt,
			"max_attempts", maxRepairAttempts,
			"command", lastFailure.Command,
			"exit_code", lastFailure.ExitCode,
			"kind", lastFailure.Kind,
		)

		failureContext := buildFailureContext(report, lastFailure, attempt)
		var repair *plan.Plan
		if genErr := func() error {
			var err error
			repair, err = client.GenerateRepairPlan(ctx, cfg, p.Summary, failureContext)
			return err
		}(); genErr != nil {
			return fmt.Errorf("generate repair plan attempt %d: %w", attempt, genErr)
		}

		// Force repair mode behavior: keep metadata stable, patch code only.
		repair.ChangelogEntry = ""
		repair.RoadmapUpdate = ""
		if strings.TrimSpace(repair.Summary) == "" {
			repair.Summary = p.Summary
		}

		slog.Info("repair plan generated", "attempt", attempt, "files", len(repair.Files))

		if cfg.Security.SecretScan {
			if err := security.ScanPlan(repair); err != nil {
				return fmt.Errorf("repair security scan failed: %w", err)
			}
		}
		if err := plan.ValidatePaths(repair, cfg); err != nil {
			return fmt.Errorf("repair path validation failed: %w", err)
		}
		if err := apply.Execute(repair); err != nil {
			return fmt.Errorf("apply repair attempt %d: %w", attempt, err)
		}

		fc, lc, nf, err := computeAndValidateDiffBudget(cfg)
		if err != nil {
			return fmt.Errorf("repair attempt %d exceeded budget: %w", attempt, err)
		}
		*filesChanged, *linesChanged, *newFiles = fc, lc, nf

		report, err = verify.RunCommandsReport(cfg.Commands)
		if err == nil {
			slog.Info("repair succeeded", "attempt", attempt)
			return nil
		}

		if !errors.As(err, &failErr) {
			return err
		}
		lastFailure = failErr.Result

		if !isRepairableFailure(lastFailure.Kind) {
			return fmt.Errorf("verification still failing (%s) after repair attempt %d: %w", lastFailure.Kind, attempt, err)
		}
	}

	return fmt.Errorf("verification failed after %d repair attempts: %s (kind=%s exit=%d)",
		maxRepairAttempts, lastFailure.Command, lastFailure.Kind, lastFailure.ExitCode)
}

func computeAndValidateDiffBudget(cfg *config.Config) (filesChanged, linesChanged, newFiles int, err error) {
	slog.Info("computing diff stats")
	filesChanged, linesChanged, err = gitops.DiffStats()
	if err != nil {
		return 0, 0, 0, err
	}
	newFiles, err = gitops.NewFilesCount()
	if err != nil {
		return 0, 0, 0, err
	}
	slog.Info("diff stats computed", "files_changed", filesChanged, "lines_changed", linesChanged, "new_files", newFiles)

	// No actual changes after applying everything => caller handles as needed.
	if filesChanged == 0 && linesChanged == 0 && newFiles == 0 {
		return filesChanged, linesChanged, newFiles, nil
	}

	if filesChanged > cfg.Budgets.MaxFilesChanged || linesChanged > cfg.Budgets.MaxLinesChanged || newFiles > cfg.Budgets.MaxNewFiles {
		slog.Error("budget exceeded",
			"files_changed", filesChanged,
			"lines_changed", linesChanged,
			"new_files", newFiles,
			"max_files", cfg.Budgets.MaxFilesChanged,
			"max_lines", cfg.Budgets.MaxLinesChanged,
			"max_new_files", cfg.Budgets.MaxNewFiles,
		)
		return filesChanged, linesChanged, newFiles, fmt.Errorf("budget exceeded: %d files, %d lines, %d new files", filesChanged, linesChanged, newFiles)
	}

	return filesChanged, linesChanged, newFiles, nil
}

func isRepairableFailure(kind string) bool {
	switch kind {
	case "compile_failure", "test_failure", "vet_failure", "unknown_failure":
		return true
	default:
		return false
	}
}

func buildFailureContext(report *verify.Report, failed verify.CommandResult, repairAttempt int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repair attempt: %d\n", repairAttempt)
	fmt.Fprintf(&b, "Failed command: %s\n", failed.Command)
	fmt.Fprintf(&b, "Exit code: %d\n", failed.ExitCode)
	fmt.Fprintf(&b, "Failure kind: %s\n", failed.Kind)
	fmt.Fprintf(&b, "Duration ms: %d\n", failed.DurationMS)

	if s := truncateForLLM(strings.TrimSpace(failed.Stdout), 6000); s != "" {
		b.WriteString("\nSTDOUT:\n")
		b.WriteString(s)
		b.WriteString("\n")
	}
	if s := truncateForLLM(strings.TrimSpace(failed.Stderr), 10000); s != "" {
		b.WriteString("\nSTDERR:\n")
		b.WriteString(s)
		b.WriteString("\n")
	}

	// Include already-passed commands to prevent unnecessary command changes.
	if report != nil && len(report.Commands) > 0 {
		b.WriteString("\nVerification results so far:\n")
		for _, r := range report.Commands {
			state := "pass"
			if !r.Passed {
				state = "fail"
			}
			fmt.Fprintf(&b, "- [%s] (%d/%d) %s (exit=%d kind=%s)\n", state, r.Index, r.Total, r.Command, r.ExitCode, r.Kind)
		}
	}

	return b.String()
}

func truncateForLLM(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	const marker = "\n...[truncated]...\n"
	if max <= len(marker)+32 {
		return s[:max]
	}
	head := (max - len(marker)) / 2
	tail := max - len(marker) - head
	return s[:head] + marker + s[len(s)-tail:]
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
