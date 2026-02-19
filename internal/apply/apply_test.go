package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mmrzaf/evolver/internal/plan"
)

func TestExecuteWritesFilesAndIsRepeatable(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	p := &plan.Plan{
		Files: []plan.File{
			{Path: "a/b/c.txt", Mode: "write", Content: "first"},
			{Path: "skip.txt", Mode: "delete", Content: "ignored"},
		},
	}
	if err := Execute(p); err != nil {
		t.Fatalf("execute first run: %v", err)
	}
	if err := Execute(p); err != nil {
		t.Fatalf("execute second run should also succeed: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(tmp, "a/b/c.txt"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(b) != "first" {
		t.Fatalf("unexpected file content: %q", string(b))
	}
	if _, err := os.Stat(filepath.Join(tmp, "skip.txt")); !os.IsNotExist(err) {
		t.Fatalf("non-write mode should not create file")
	}
}
