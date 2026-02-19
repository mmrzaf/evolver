package plan

import (
	"testing"

	"github.com/mmrzaf/evolver/internal/config"
)

func TestValidatePathsRejectsDeniedPath(t *testing.T) {
	cfg := &config.Config{
		DenyPaths: []string{".github/workflows/", ".git/"},
		Security:  config.Security{AllowWorkflowEdits: false},
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
		DenyPaths: []string{".github/workflows/", ".git/"},
		Security:  config.Security{AllowWorkflowEdits: true},
	}
	p := &Plan{
		Files: []File{{Path: ".github/workflows/ci.yml", Mode: "write", Content: "name: CI"}},
	}
	if err := ValidatePaths(p, cfg); err != nil {
		t.Fatalf("expected workflow path to pass when explicitly enabled: %v", err)
	}
}

func TestValidatePathsAllowsSafeFiles(t *testing.T) {
	cfg := &config.Config{
		DenyPaths: []string{".github/workflows/", ".git/"},
		Security:  config.Security{AllowWorkflowEdits: false},
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
