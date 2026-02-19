package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mmrzaf/evolver/internal/apply"
	"github.com/mmrzaf/evolver/internal/config"
	"github.com/mmrzaf/evolver/internal/ghapi"
	"github.com/mmrzaf/evolver/internal/gitops"
	"github.com/mmrzaf/evolver/internal/llm/gemini"
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
	changed := false
	summary := ""

	cfg := config.Load()
	if err := os.Chdir(cfg.Workdir); err != nil {
		return err
	}

	if err := policy.Bootstrap(cfg); err != nil {
		return err
	}

	unlock, err := runstate.AcquireLock(cfg.Reliability.LockFile, time.Duration(cfg.Reliability.LockStaleMinutes)*time.Minute)
	if err != nil {
		return err
	}
	defer unlock()

	recorder, err := runstate.NewRecorder(cfg.Reliability.StateFile, cfg.Reliability.RunLogFile)
	if err != nil {
		return err
	}
	if err := recorder.Start(); err != nil {
		return err
	}
	defer func() {
		finishErr := recorder.Finish(changed, summary, err)
		if err == nil && finishErr != nil {
			err = finishErr
		}
	}()

	ctx, err := repoctx.Gather(cfg)
	if err != nil {
		return err
	}

	var p *plan.Plan
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "gemini":
		client := gemini.NewClient(os.Getenv("GEMINI_API_KEY"), cfg.Model)
		p, err = client.GeneratePlan(ctx, cfg)
	default:
		return fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
	if err != nil {
		return err
	}

	// If the LLM proposes no changes, we still might have bootstrap changes to commit.
	if len(p.Files) == 0 && p.ChangelogEntry == "" && p.RoadmapUpdate == "" {
		dirty, derr := gitops.HasChanges()
		if derr == nil && !dirty {
			summary = "No changes proposed"
			setOutput("changed", "false")
			setOutput("summary", summary)
			return nil
		}
		if strings.TrimSpace(p.Summary) == "" {
			p.Summary = "Bootstrap evolver scaffolding"
		}
	}

	if cfg.Security.SecretScan {
		if err := security.ScanPlan(p); err != nil {
			return err
		}
	}
	if err := plan.ValidatePaths(p, cfg); err != nil {
		return err
	}

	branchName := fmt.Sprintf("evolve/%s", time.Now().Format("2006-01-02-150405"))
	if cfg.Mode == "pr" {
		if err := gitops.CheckoutNew(branchName); err != nil {
			return err
		}
	}

	if err := apply.Execute(p); err != nil {
		return err
	}

	if err := policy.AppendChangelog(p.ChangelogEntry); err != nil {
		return err
	}
	if p.RoadmapUpdate != "" {
		if err := policy.UpdateRoadmap(p.RoadmapUpdate); err != nil {
			return err
		}
	}

	filesChanged, linesChanged, err := gitops.DiffStats()
	if err != nil {
		return err
	}
	newFiles, err := gitops.NewFilesCount()
	if err != nil {
		return err
	}

	// No actual changes after applying everything => stop without committing.
	if filesChanged == 0 && linesChanged == 0 && newFiles == 0 {
		summary = "No changes produced"
		setOutput("changed", "false")
		setOutput("summary", summary)
		return nil
	}

	if filesChanged > cfg.Budgets.MaxFilesChanged || linesChanged > cfg.Budgets.MaxLinesChanged || newFiles > cfg.Budgets.MaxNewFiles {
		gitops.ResetHard()
		return fmt.Errorf("budget exceeded: %d files, %d lines, %d new files", filesChanged, linesChanged, newFiles)
	}

	if err := verify.RunCommands(cfg.Commands); err != nil {
		gitops.ResetHard()
		return err
	}

	if err := gitops.Commit(p.Summary); err != nil {
		return err
	}

	if cfg.Mode == "pr" {
		if err := gitops.Push(branchName); err != nil {
			return err
		}
		url, err := ghapi.CreatePR(branchName, p.Summary, generatePRBody(p, filesChanged, linesChanged, newFiles))
		if err != nil {
			return err
		}
		setOutput("pr_url", url)
	} else {
		if err := gitops.Push("HEAD"); err != nil {
			return err
		}
	}

	setOutput("changed", "true")
	setOutput("summary", p.Summary)
	changed = true
	summary = p.Summary
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
