package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mmrzaf/evolver/internal/config"
)

// Plan is the structured output describing repository updates.
type Plan struct {
	Summary        string `json:"summary"`
	Files          []File `json:"files"`
	ChangelogEntry string `json:"changelog_entry"`
	RoadmapUpdate  string `json:"roadmap_update"`
}

// File describes a single file operation from a plan.
type File struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
}

// ValidatePaths enforces path safety, allow-paths, and deny-path rules against planned file edits.
func ValidatePaths(p *Plan, cfg *config.Config) error {
	for _, f := range p.Files {
		cleanPath, err := normalizeRelPath(f.Path)
		if err != nil {
			return fmt.Errorf("invalid path %q: %w", f.Path, err)
		}

		// Workflows are always gated by the explicit flag, even if a user edits deny_paths.
		if isWorkflowPath(cleanPath) && !cfg.Security.AllowWorkflowEdits {
			return fmt.Errorf("path %s is denied: workflow edits are not enabled", cleanPath)
		}

		if !isAllowed(cleanPath, cfg.AllowPaths) {
			return fmt.Errorf("path %s is not within allow_paths", cleanPath)
		}

		// Apply deny rules (except workflows, handled above).
		for _, deny := range cfg.DenyPaths {
			denyClean, derr := normalizeRelPath(deny)
			if derr != nil {
				// ignore malformed deny entry rather than disabling all validation
				continue
			}
			if denyClean == "." {
				continue
			}
			if isWorkflowPath(denyClean) {
				continue
			}
			if cleanPath == denyClean || strings.HasPrefix(cleanPath, denyClean+string(os.PathSeparator)) {
				return fmt.Errorf("path %s is denied by rule %s", cleanPath, deny)
			}
		}
	}
	return nil
}

func isAllowed(path string, allow []string) bool {
	// Default allow: everything under repo root.
	if len(allow) == 0 {
		return true
	}
	for _, a := range allow {
		if strings.TrimSpace(a) == "." {
			return true
		}
		ac, err := normalizeRelPath(a)
		if err != nil {
			continue
		}
		if path == ac || strings.HasPrefix(path, ac+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func isWorkflowPath(cleanRelPath string) bool {
	// `.github/workflows` subtree.
	wf := filepath.Clean(".github/workflows")
	if cleanRelPath == wf {
		return true
	}
	return strings.HasPrefix(cleanRelPath, wf+string(os.PathSeparator))
}

func normalizeRelPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.ContainsRune(p, '\x00') {
		return "", fmt.Errorf("nul byte")
	}
	// Guard against absolute paths and Windows drive prefixes in LLM output.
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") || (len(p) > 1 && p[1] == ':') {
		return "", fmt.Errorf("absolute path")
	}
	clean := filepath.Clean(p)

	// filepath.Clean can yield "."; treat as invalid file path.
	if clean == "." {
		return "", fmt.Errorf("invalid path")
	}

	// Disallow escaping repo root.
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes repository root")
	}
	return clean, nil
}
