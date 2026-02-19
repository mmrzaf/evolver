package apply

import (
	"os"
	"path/filepath"

	"github.com/mmrzaf/evolver/internal/plan"
)

// Execute applies write operations from a generated plan.
func Execute(p *plan.Plan) error {
	for _, f := range p.Files {
		if f.Mode != "write" {
			continue
		}
		dir := filepath.Dir(f.Path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(f.Path, []byte(f.Content), 0644); err != nil {
			return err
		}
	}
	return nil
}
