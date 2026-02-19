package policy

import (
	"fmt"
	"os"

	"github.com/mmrzaf/evolver/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	policyTmpl    = "# POLICY\n- Small incremental changes.\n- Keep repo runnable.\n- Update CHANGELOG.\n- Add tests.\n- No secrets.\n"
	roadmapTmpl   = "# ROADMAP\n## Current Objective\n%s\n\n## Now/Next/Later\n- [ ] Initial scaffold\n"
	changelogTmpl = "# CHANGELOG\n"
)

// Bootstrap initializes policy/config/changelog scaffolding files if absent.
func Bootstrap(cfg *config.Config) error {
	if err := os.MkdirAll(".evolver", 0755); err != nil {
		return err
	}

	if _, err := os.Stat(".evolver/config.yml"); os.IsNotExist(err) {
		b, err := yaml.Marshal(cfg)
		if err != nil {
			return err
		}
		if err := os.WriteFile(".evolver/config.yml", b, 0644); err != nil {
			return err
		}
	}

	goal := cfg.RepoGoal
	if goal == "" {
		goal = "Build a useful starting project."
	}

	files := map[string]string{
		"POLICY.md":    policyTmpl,
		"ROADMAP.md":   fmt.Sprintf(roadmapTmpl, goal),
		"CHANGELOG.md": changelogTmpl,
	}

	for p, t := range files {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if err := os.WriteFile(p, []byte(t), 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

// AppendChangelog appends a single entry to CHANGELOG.md.
func AppendChangelog(entry string) (err error) {
	if entry == "" {
		return nil
	}
	f, err := os.OpenFile("CHANGELOG.md", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = f.WriteString("\n" + entry + "\n")
	return err
}

// UpdateRoadmap overwrites ROADMAP.md with the provided content.
func UpdateRoadmap(content string) error {
	return os.WriteFile("ROADMAP.md", []byte(content), 0644)
}
