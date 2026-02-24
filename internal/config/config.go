package config

import (
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config controls runtime behavior for the evolver.
type Config struct {
	Provider    string      `yaml:"provider"`
	Mode        string      `yaml:"mode"`
	Model       string      `yaml:"model"`
	RepoGoal    string      `yaml:"repo_goal,omitempty"`
	Workdir     string      `yaml:"workdir"`
	Budgets     Budgets     `yaml:"budgets"`
	Commands    []string    `yaml:"commands"`
	AllowPaths  []string    `yaml:"allow_paths"`
	DenyPaths   []string    `yaml:"deny_paths"`
	Security    Security    `yaml:"security"`
	Reliability Reliability `yaml:"reliability"`
	Logging     Logging     `yaml:"logging"`
}

// Budgets limits the size of generated changes.
type Budgets struct {
	MaxFilesChanged int `yaml:"max_files_changed"`
	MaxLinesChanged int `yaml:"max_lines_changed"`
	MaxNewFiles     int `yaml:"max_new_files"`
}

// Security configures guardrails for sensitive edits/content.
type Security struct {
	AllowWorkflowEdits bool `yaml:"allow_workflow_edits"`
	SecretScan         bool `yaml:"secret_scan"`
}

// Reliability configures lock and run-state persistence.
type Reliability struct {
	StateFile        string `yaml:"state_file"`
	RunLogFile       string `yaml:"run_log_file"`
	LockFile         string `yaml:"lock_file"`
	LockStaleMinutes int    `yaml:"lock_stale_minutes"`
}

// Logging configures runtime logging behavior.
type Logging struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	File   string `yaml:"file"`
}

// Load builds config from defaults, file values, and environment overrides.
func Load() *Config {
	c := &Config{
		Provider:   "gemini",
		Mode:       "pr",
		Model:      "gemini-2.5-flash-lite",
		Workdir:    ".",
		Budgets:    Budgets{MaxFilesChanged: 10, MaxLinesChanged: 500, MaxNewFiles: 10},
		Commands:   []string{},
		AllowPaths: []string{"."},
		DenyPaths:  []string{".git/", ".github/workflows/", "node_modules/"},
		Security:   Security{AllowWorkflowEdits: false, SecretScan: true},
		Reliability: Reliability{
			StateFile:        ".evolver/state.json",
			RunLogFile:       ".evolver/runs.log",
			LockFile:         ".evolver/run.lock",
			LockStaleMinutes: 180,
		},
		Logging: Logging{
			Level:  "info",
			Format: "text",
			File:   ".evolver/evolver.log",
		},
	}

	// Config file overrides defaults.
	if b, err := os.ReadFile(".evolver/config.yml"); err == nil {
		_ = yaml.Unmarshal(b, c)
	}

	// Environment overrides file and defaults.
	if v := os.Getenv("EVOLVER_PROVIDER"); v != "" {
		c.Provider = v
	}
	if v := os.Getenv("EVOLVER_MODE"); v != "" {
		c.Mode = v
	}
	if v := os.Getenv("EVOLVER_MODEL"); v != "" {
		c.Model = v
	}
	if v := os.Getenv("EVOLVER_REPO_GOAL"); v != "" {
		c.RepoGoal = v
	}
	if v := os.Getenv("EVOLVER_WORKDIR"); v != "" {
		c.Workdir = v
	}
	if v := os.Getenv("EVOLVER_MAX_FILES"); v != "" {
		c.Budgets.MaxFilesChanged, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("EVOLVER_MAX_LINES"); v != "" {
		c.Budgets.MaxLinesChanged, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("EVOLVER_MAX_NEW_FILES"); v != "" {
		c.Budgets.MaxNewFiles, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("EVOLVER_COMMANDS"); v != "" {
		// Newline-separated; ignore blank lines.
		parts := strings.Split(v, "\n")
		c.Commands = c.Commands[:0]
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			c.Commands = append(c.Commands, p)
		}
	}
	if v := os.Getenv("EVOLVER_ALLOW_WORKFLOWS"); v == "true" {
		c.Security.AllowWorkflowEdits = true
	}
	if v := os.Getenv("EVOLVER_STATE_FILE"); v != "" {
		c.Reliability.StateFile = v
	}
	if v := os.Getenv("EVOLVER_RUN_LOG_FILE"); v != "" {
		c.Reliability.RunLogFile = v
	}
	if v := os.Getenv("EVOLVER_LOCK_FILE"); v != "" {
		c.Reliability.LockFile = v
	}
	if v := os.Getenv("EVOLVER_LOCK_STALE_MINUTES"); v != "" {
		c.Reliability.LockStaleMinutes, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("EVOLVER_LOG_LEVEL"); v != "" {
		c.Logging.Level = v
	}
	if v := os.Getenv("EVOLVER_LOG_FORMAT"); v != "" {
		c.Logging.Format = v
	}
	if v := os.Getenv("EVOLVER_LOG_FILE"); v != "" {
		c.Logging.File = v
	}
	return c
}
