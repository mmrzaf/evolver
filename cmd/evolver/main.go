package main

import (
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

	var p *plan.Plan
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "gemini":
		client := gemini.NewClient(os.Getenv("GEMINI_API_KEY"), cfg.Model)
		if err := logStep("generate_plan_gemini", func() error {
			planResult, planErr := client.GeneratePlan(ctx, cfg)
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

	slog.Info("computing diff stats")
	filesChanged, linesChanged, err := gitops.DiffStats()
	if err != nil {
		return err
	}
	newFiles, err := gitops.NewFilesCount()
	if err != nil {
		return err
	}
	slog.Info("diff stats computed", "files_changed", filesChanged, "lines_changed", linesChanged, "new_files", newFiles)

	// No actual changes after applying everything => stop without committing.
	if filesChanged == 0 && linesChanged == 0 && newFiles == 0 {
		summary = "No changes produced"
		slog.Info("run ended with no produced changes", "summary", summary)
		setOutput("changed", "false")
		setOutput("summary", summary)
		return nil
	}

	if filesChanged > cfg.Budgets.MaxFilesChanged || linesChanged > cfg.Budgets.MaxLinesChanged || newFiles > cfg.Budgets.MaxNewFiles {
		slog.Error("budget exceeded; resetting working tree",
			"files_changed", filesChanged,
			"lines_changed", linesChanged,
			"new_files", newFiles,
			"max_files", cfg.Budgets.MaxFilesChanged,
			"max_lines", cfg.Budgets.MaxLinesChanged,
			"max_new_files", cfg.Budgets.MaxNewFiles,
		)
		gitops.ResetHard()
		return fmt.Errorf("budget exceeded: %d files, %d lines, %d new files", filesChanged, linesChanged, newFiles)
	}

	if err := logStep("verify_commands", func() error { return verify.RunCommands(cfg.Commands) }); err != nil {
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
