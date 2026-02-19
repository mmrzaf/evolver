package repoctx

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mmrzaf/evolver/internal/config"
)

// Context contains repository metadata and excerpts used in prompting.
type Context struct {
	Files     []string
	Excerpts  map[string]string
	Policy    string
	Roadmap   string
	Changelog string
}

// Gather collects repository file data while respecting deny rules.
func Gather(cfg *config.Config) (*Context, error) {
	ctx := &Context{Excerpts: make(map[string]string)}

	if err := filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		for _, deny := range cfg.DenyPaths {
			if strings.HasPrefix(path, filepath.Clean(deny)) {
				return nil
			}
		}
		ctx.Files = append(ctx.Files, path)
		if info.Size() < 5000 {
			b, _ := os.ReadFile(path)
			ctx.Excerpts[path] = string(b)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	p, _ := os.ReadFile("POLICY.md")
	ctx.Policy = string(p)
	r, _ := os.ReadFile("ROADMAP.md")
	ctx.Roadmap = string(r)
	c, _ := os.ReadFile("CHANGELOG.md")
	if len(c) > 2000 {
		ctx.Changelog = string(c[len(c)-2000:])
	} else {
		ctx.Changelog = string(c)
	}

	return ctx, nil
}
