package apply

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mmrzaf/evolver/internal/plan"
)

// Execute applies write operations from a generated plan.
func Execute(p *plan.Plan) error {
	writes := 0
	for _, f := range p.Files {
		if f.Mode != "write" {
			continue
		}
		cleanPath, err := safeRelPath(f.Path)
		if err != nil {
			return fmt.Errorf("refusing to write unsafe path %q: %w", f.Path, err)
		}

		dir := filepath.Dir(cleanPath)
		if dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
		}
		if err := os.WriteFile(cleanPath, []byte(f.Content), 0644); err != nil {
			return err
		}
		writes++
		slog.Debug("applied file write", "path", cleanPath, "bytes", len(f.Content))
	}
	slog.Info("plan applied", "files_written", writes)
	return nil
}

func safeRelPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.ContainsRune(p, '\x00') {
		return "", fmt.Errorf("nul byte")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") || (len(p) > 1 && p[1] == ':') {
		return "", fmt.Errorf("absolute path")
	}
	clean := filepath.Clean(p)
	if clean == "." {
		return "", fmt.Errorf("invalid path")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes repository root")
	}
	return clean, nil
}
