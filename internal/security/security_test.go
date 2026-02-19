package security

import (
	"testing"

	"github.com/mmrzaf/evolver/internal/plan"
)

func TestScanPlanDetectsSecrets(t *testing.T) {
	p := &plan.Plan{
		Files: []plan.File{
			{Path: "keys.txt", Content: "AKIAABCDEFGHIJKLMNOP"},
		},
	}
	if err := ScanPlan(p); err == nil {
		t.Fatalf("expected secret scan to fail on AWS key pattern")
	}
}

func TestScanPlanPassesSafeContent(t *testing.T) {
	p := &plan.Plan{
		Files: []plan.File{
			{Path: "README.md", Content: "safe docs only"},
		},
	}
	if err := ScanPlan(p); err != nil {
		t.Fatalf("expected safe content to pass scan: %v", err)
	}
}
