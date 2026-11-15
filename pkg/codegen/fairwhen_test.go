package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
)

// TestGeneratedCodeKeepsEveryFairWhenRule covers a field the schema fouls for
// two reasons: the generated check holds both rules, and the reason it reports
// is the rule that failed. The runtimes evaluate every fairWhen on a field, so
// a field's generated check has to as well.
func TestGeneratedCodeKeepsEveryFairWhenRule(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "score", IsEmpty: "number"},
		},
		Rules: []schema.Rule{
			{
				Type:     "fairWhen",
				Field:    "score",
				FairWhen: &schema.Expr{Op: "gte", Field: "score", Value: float64(1)},
				Reason:   "score must be at least 1",
			},
			{
				Type:     "fairWhen",
				Field:    "score",
				FairWhen: &schema.Expr{Op: "lte", Field: "score", Value: float64(10)},
				Reason:   "score must be at most 10",
			},
		},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	gen := NewGenerator("Score", "scorepkg", "ScoreFields", "ScoreConditions", inferred)
	gen.WithFields(s.Fields)
	gen.WithRules(s.Rules)

	result, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "scorepkg"), 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "scorepkg", "score_gen.go"), []byte(result.Source), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module scorepkg\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	const testSource = `package scorepkg

import "testing"

func value(v float64) *float64 { return &v }

func TestEveryFairWhenRuleHolds(t *testing.T) {
	tests := []struct {
		name   string
		score  *float64
		fair   bool
		reason string
	}{
		{"below the first rule", value(0), false, "score must be at least 1"},
		{"above the second rule", value(50), false, "score must be at most 10"},
		{"inside both", value(5), true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avail := Check(ScoreFields{Score: tt.score}, ScoreConditions{}, ScoreFields{})
			if avail.Score.Fair != tt.fair {
				t.Fatalf("fair is %v, want %v", avail.Score.Fair, tt.fair)
			}
			if tt.reason == "" {
				return
			}
			if avail.Score.Reason == nil {
				t.Fatalf("the field reports no reason, want %q", tt.reason)
			}
			if *avail.Score.Reason != tt.reason {
				t.Errorf("the reason is %q, want %q", *avail.Score.Reason, tt.reason)
			}
		})
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "scorepkg", "score_gen_test.go"), []byte(testSource), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(),
		"GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(tmpDir, ".gocache"),
		"GOMODCACHE="+filepath.Join(tmpDir, ".gomodcache"),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated check failed its tests: %v\n%s\n--- source ---\n%s", err, output, result.Source)
	}
}

// TestGeneratedCodeHoldsACheckAndAFairWhen covers a field with both a check
// rule and a fairWhen rule: both decide fairness, and each reports its own
// reason. Before, the check ruled alone and the fairWhen reason described a
// condition nothing evaluated.
func TestGeneratedCodeHoldsACheckAndAFairWhen(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "score", IsEmpty: "number"},
		},
		Rules: []schema.Rule{
			{
				Type:   "check",
				Field:  "score",
				Check:  &schema.Expr{Op: "max", Field: "score", Value: float64(100)},
				Reason: "score must be at most 100",
			},
			{
				Type:     "fairWhen",
				Field:    "score",
				FairWhen: &schema.Expr{Op: "gte", Field: "score", Value: float64(1)},
				Reason:   "score must be at least 1",
			},
		},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	gen := NewGenerator("Score", "scorepkg", "ScoreFields", "ScoreConditions", inferred)
	gen.WithFields(s.Fields)
	gen.WithRules(s.Rules)

	result, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "scorepkg"), 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "scorepkg", "score_gen.go"), []byte(result.Source), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module scorepkg\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	const testSource = `package scorepkg

import "testing"

func value(v float64) *float64 { return &v }

func TestCheckAndFairWhenBothHold(t *testing.T) {
	tests := []struct {
		name   string
		score  *float64
		fair   bool
		reason string
	}{
		{"the fairWhen alone fails", value(0.5), false, "score must be at least 1"},
		{"the check alone fails", value(200), false, "score must be at most 100"},
		{"inside both", value(50), true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avail := Check(ScoreFields{Score: tt.score}, ScoreConditions{}, ScoreFields{})
			if avail.Score.Fair != tt.fair {
				t.Fatalf("fair is %v, want %v", avail.Score.Fair, tt.fair)
			}
			if tt.reason == "" {
				return
			}
			if avail.Score.Reason == nil {
				t.Fatalf("the field reports no reason, want %q", tt.reason)
			}
			if *avail.Score.Reason != tt.reason {
				t.Errorf("the reason is %q, want %q", *avail.Score.Reason, tt.reason)
			}
		})
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "scorepkg", "score_gen_test.go"), []byte(testSource), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(),
		"GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(tmpDir, ".gocache"),
		"GOMODCACHE="+filepath.Join(tmpDir, ".gomodcache"),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated check failed its tests: %v\n%s\n--- source ---\n%s", err, output, result.Source)
	}
}
