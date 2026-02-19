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

// ValidatePaths enforces deny-path rules against planned file edits.
func ValidatePaths(p *Plan, cfg *config.Config) error {
	for _, f := range p.Files {
		cleanPath := filepath.Clean(f.Path)
		for _, deny := range cfg.DenyPaths {
			cleanDeny := filepath.Clean(deny)
			if cleanPath == cleanDeny || strings.HasPrefix(cleanPath, cleanDeny+string(os.PathSeparator)) {
				if deny == ".github/workflows/" && cfg.Security.AllowWorkflowEdits {
					continue
				}
				return fmt.Errorf("path %s is denied by rule %s", cleanPath, deny)
			}
		}
	}
	return nil
}
