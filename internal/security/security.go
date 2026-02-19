package security

import (
	"fmt"
	"regexp"

	"github.com/mmrzaf/evolver/internal/plan"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN (RSA|OPENSSH|PRIVATE) KEY-----`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`ghp_[0-9a-zA-Z]{36}`),
}

// ScanPlan rejects plans that appear to include sensitive secrets.
func ScanPlan(p *plan.Plan) error {
	for _, f := range p.Files {
		for _, re := range secretPatterns {
			if re.MatchString(f.Content) {
				return fmt.Errorf("security violation: sensitive data detected in %s", f.Path)
			}
		}
	}
	return nil
}
