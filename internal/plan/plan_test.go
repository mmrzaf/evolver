package plan

import (
	"testing"

	"github.com/mmrzaf/evolver/internal/config"
)

func TestValidatePathsRejectsDeniedPath(t *testing.T) {
	cfg := &config.Config{
		AllowPaths: []string{"."},
		DenyPaths:  []string{".github/workflows/", ".git/"},
		Security:   config.Security{AllowWorkflowEdits: false},
	}
	p := &Plan{
		Files: []File{{Path: ".github/workflows/ci.yml", Mode: "write", Content: "name: CI"}},
	}
	if err := ValidatePaths(p, cfg); err == nil {
		t.Fatalf("expected denied workflow path to fail validation")
	}
}

func TestValidatePathsAllowsWorkflowsWhenExplicitlyEnabled(t *testing.T) {
	cfg := &config.Config{
		AllowPaths: []string{"."},
		DenyPaths:  []string{".github/workflows/", ".git/"},
		Security:   config.Security{AllowWorkflowEdits: true},
	}
	p := &Plan{
		Files: []File{{Path: ".github/workflows/ci.yml", Mode: "write", Content: "name: CI"}},
	}
	if err := ValidatePaths(p, cfg); err != nil {
		t.Fatalf("expected workflow path to pass when explicitly enabled: %v", err)
	}
}

func TestValidatePathsRejectsTraversal(t *testing.T) {
	cfg := &config.Config{
		AllowPaths: []string{"."},
		DenyPaths:  []string{".git/"},
		Security:   config.Security{AllowWorkflowEdits: false},
	}
	p := &Plan{
		Files: []File{{Path: "../../pwned.txt", Mode: "write", Content: "x"}},
	}
	if err := ValidatePaths(p, cfg); err == nil {
		t.Fatalf("expected traversal path to fail validation")
	}
}

func TestValidatePathsRejectsAbsolute(t *testing.T) {
	cfg := &config.Config{
		AllowPaths: []string{"."},
		DenyPaths:  []string{},
		Security:   config.Security{AllowWorkflowEdits: false},
	}
	p := &Plan{
		Files: []File{{Path: "/etc/passwd", Mode: "write", Content: "x"}},
	}
	if err := ValidatePaths(p, cfg); err == nil {
		t.Fatalf("expected absolute path to fail validation")
	}
}

func TestValidatePathsEnforcesAllowPaths(t *testing.T) {
	cfg := &config.Config{
		AllowPaths: []string{"docs"},
		DenyPaths:  []string{},
		Security:   config.Security{AllowWorkflowEdits: true},
	}
	p := &Plan{
		Files: []File{{Path: "README.md", Mode: "write", Content: "# no"}},
	}
	if err := ValidatePaths(p, cfg); err == nil {
		t.Fatalf("expected path outside allow_paths to fail validation")
	}
}

func TestValidatePathsAllowsSafeFiles(t *testing.T) {
	cfg := &config.Config{
		AllowPaths: []string{"."},
		DenyPaths:  []string{".github/workflows/", ".git/"},
		Security:   config.Security{AllowWorkflowEdits: false},
	}
	p := &Plan{
		Files: []File{
			{Path: "internal/app/main.go", Mode: "write", Content: "package main"},
			{Path: "README.md", Mode: "write", Content: "# hi"},
		},
	}
	if err := ValidatePaths(p, cfg); err != nil {
		t.Fatalf("expected safe paths to pass validation: %v", err)
	}
}
