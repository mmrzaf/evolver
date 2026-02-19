package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mmrzaf/evolver/internal/plan"
)

func TestGeneratePRBodyIncludesCoreSections(t *testing.T) {
	p := &plan.Plan{
		Summary:       "Improve retry logic",
		RoadmapUpdate: "- [x] Added backoff",
	}
	body := generatePRBody(p, 3, 42, 1)

	mustContain := []string{
		"## Summary",
		"Improve retry logic",
		"- Files changed: 3",
		"- Lines changed: 42",
		"- New files: 1",
		"## Roadmap Update",
		"- [x] Added backoff",
	}
	for _, s := range mustContain {
		if !strings.Contains(body, s) {
			t.Fatalf("expected PR body to contain %q", s)
		}
	}
}

func TestSetOutputWritesGithubOutputFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "github_output_*")
	if err != nil {
		t.Fatalf("create temp output file: %v", err)
	}
	path := f.Name()
	_ = f.Close()

	t.Setenv("GITHUB_OUTPUT", path)
	setOutput("summary", "line1\nline2")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	got := string(b)

	if !strings.Contains(got, "summary<<EVOLVER_") {
		t.Fatalf("expected multiline output format, got %q", got)
	}
	if !strings.Contains(got, "line1\nline2\n") {
		t.Fatalf("expected value written as-is, got %q", got)
	}

	re := regexp.MustCompile(`summary<<EVOLVER_\d+\nline1\nline2\nEVOLVER_\d+\n`)
	if !re.MatchString(got) {
		t.Fatalf("unexpected output format: %q", got)
	}
}
