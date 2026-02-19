package repoctx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mmrzaf/evolver/internal/config"
)

func TestGatherCollectsExpectedContextAndRespectsDenyPaths(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if err := os.MkdirAll(".github/workflows", 0755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := os.WriteFile("main.go", []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	if err := os.WriteFile(filepath.Join(".github/workflows", "ci.yml"), []byte("name: ci\n"), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	if err := os.WriteFile("POLICY.md", []byte("# POLICY\n"), 0644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	if err := os.WriteFile("ROADMAP.md", []byte("# ROADMAP\n"), 0644); err != nil {
		t.Fatalf("write roadmap: %v", err)
	}
	large := strings.Repeat("x", 2100)
	if err := os.WriteFile("CHANGELOG.md", []byte(large), 0644); err != nil {
		t.Fatalf("write changelog: %v", err)
	}

	cfg := &config.Config{DenyPaths: []string{".github/workflows/"}}
	ctx, err := Gather(cfg)
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, f := range ctx.Files {
		if strings.HasPrefix(f, ".github/workflows") {
			t.Fatalf("expected denied workflow file to be excluded, found %s", f)
		}
	}
	if _, ok := ctx.Excerpts["main.go"]; !ok {
		t.Fatalf("expected excerpt for main.go")
	}
	if len(ctx.Changelog) != 2000 {
		t.Fatalf("expected changelog tail of 2000 chars, got %d", len(ctx.Changelog))
	}
}
