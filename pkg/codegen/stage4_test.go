package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
)

func TestGenerateWithRules(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "country", Required: true, IsEmpty: "string"},
			{Name: "promoCode"},
			{Name: "itemsCount", TypeHint: "number"},
		},
		Conditions: []schema.ConditionDef{
			{Name: "userRole", Type: "string"},
			{Name: "isGuest", Type: "boolean"},
		},
		Rules: []schema.Rule{
			{
				Type:   "enabledWhen",
				Field:  "country",
				Expr:   &schema.Expr{Op: "present", Field: "country"},
				Reason: "Country must be set",
			},
			{
				Type:     "requires",
				Field:    "promoCode",
				Requires: []string{"country"},
				Reason:   "Requires country",
			},
			{
				Type:     "fairWhen",
				Field:    "itemsCount",
				FairWhen: &schema.Expr{Op: "gt", Field: "itemsCount", Value: float64(5)},
				Reason:   "Fair for large orders",
			},
		},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	gen := NewGenerator("Checkout", "testpkg", "CheckoutFields", "CheckoutConditions", inferred)
	gen.WithFields(s.Fields)
	gen.WithFields(s.Fields)
		gen.WithRules(s.Rules)

	result, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	if result.Source == "" {
		t.Fatal("generated source is empty")
	}

	// Verify Check function is present
	if !strings.Contains(result.Source, "func Check(f CheckoutFields, c CheckoutConditions, prev CheckoutFields) CheckoutAvailability") {
		t.Error("generated source missing Check function")
	}

	// Verify depSatisfied helper is present
	if !strings.Contains(result.Source, "func depSatisfied(f CheckoutFields, name string) bool") {
		t.Error("generated source missing depSatisfied helper")
	}

	// Verify Challenge function is present
	if !strings.Contains(result.Source, "func Challenge(fieldName string, f CheckoutFields, c CheckoutConditions, prev CheckoutFields) ChallengeResult") {
		t.Error("generated source missing Challenge function")
	}

	// Verify RuleMetaEntry is present
	if !strings.Contains(result.Source, "type RuleMetaEntry struct") {
		t.Error("generated source missing RuleMetaEntry")
	}

	// Verify ruleMeta variable is present
	if !strings.Contains(result.Source, "var ruleMeta = []RuleMetaEntry") {
		t.Error("generated source missing ruleMeta")
	}

	// Verify Country has Required: true
	if !strings.Contains(result.Source, "Required: true") {
		t.Error("generated source missing Required: true for country")
	}
}

func TestGeneratedCodeCompilesAndRuns(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "country", Required: true, TypeHint: "string"},
			{Name: "promoCode", TypeHint: "string"},
		},
		Conditions: []schema.ConditionDef{
			{Name: "isAdmin", Type: "boolean"},
		},
		Rules: []schema.Rule{
			{
				Type:   "enabledWhen",
				Field:  "country",
				Expr:   &schema.Expr{Op: "present", Field: "country"},
				Reason: "Country must be set",
			},
			{
				Type:     "requires",
				Field:    "promoCode",
				Requires: []string{"country"},
				Reason:   "Requires country to use promo code",
			},
		},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	gen := NewGenerator("Test", "testpkg", "TestFields", "TestConditions", inferred)
	gen.WithFields(s.Fields)
		gen.WithRules(s.Rules)

	result, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// Write to temp directory as a Go package
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "testpkg"), 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}

	// Write the generated Go source
	if err := os.WriteFile(filepath.Join(tmpDir, "testpkg", "test_gen.go"), []byte(result.Source), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	// Write a test file that exercises the generated code
	testMain := `package testpkg

import "testing"

func TestCheckWithEmptyFields(t *testing.T) {
	var f TestFields
	var c TestConditions
	var prev TestFields

	avail := Check(f, c, prev)

	// country is a non-pointer string. The present expression compiles to
	// "f.Country != \"\"" so the field is disabled until it holds a value.
	if avail.Country.Enabled {
		t.Errorf("country should NOT be enabled when empty (present checks non-empty for strings)")
	}

	if avail.Country.Satisfied {
		t.Errorf("country should NOT be satisfied when empty")
	}

	if avail.Country.Required != true {
		t.Errorf("country should be required, got %v", avail.Country.Required)
	}

	// promoCode requires country — country is not satisfied, so promoCode should be disabled
	if avail.PromoCode.Enabled {
		t.Errorf("promoCode should NOT be enabled when required country is not satisfied")
	}
}

func TestCheckWithSetFields(t *testing.T) {
	var f TestFields
	f.Country = "US"
	var c TestConditions
	var prev TestFields

	avail := Check(f, c, prev)

	if !avail.Country.Enabled {
		t.Errorf("country should be enabled when set")
	}

	if !avail.Country.Satisfied {
		t.Errorf("country should be satisfied when set to non-empty")
	}

	if !avail.Country.Required {
		t.Errorf("country should be required")
	}

	// promoCode should be enabled since country is satisfied
	if !avail.PromoCode.Enabled {
		t.Errorf("promoCode should be enabled when country is satisfied")
	}

	// Test Challenge
	ch := Challenge("country", f, c, prev)
	if ch.FieldName != "Country" && ch.FieldName != "country" {
		t.Errorf("Challenge returned wrong field name: %s", ch.FieldName)
	}
	if len(ch.Explanations) == 0 {
		t.Errorf("Challenge should have explanations, got %d", len(ch.Explanations))
	}
}

`
	if err := os.WriteFile(filepath.Join(tmpDir, "testpkg", "test_gen_test.go"), []byte(testMain), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	// Create go.mod
	goMod := "module testpkg\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	// Run go test
	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(string(output), "PASS") {
		t.Errorf("expected PASS in test output, got:\n%s", output)
	}
}

func TestGeneratedCodeWithDisabledField(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "country", Required: false, TypeHint: "string"},
			{Name: "vipDiscount", TypeHint: "string"},
		},
		Conditions: []schema.ConditionDef{},
		Rules: []schema.Rule{
			{
				Type:   "enabledWhen",
				Field:  "country",
				Expr:   &schema.Expr{Op: "present", Field: "country"},
				Reason: "Country required",
			},
			{
				Type:   "disables",
				Field:  "vipDiscount",
				Expr:   &schema.Expr{Op: "not", Exprs: []schema.Expr{{Op: "eq", Field: "country", Value: "US"}}},
				Reason: "VIP discount only for US",
			},
		},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	gen := NewGenerator("Test", "testpkg", "TestFields", "TestConditions", inferred)
	gen.WithFields(s.Fields)
		gen.WithRules(s.Rules)

	result, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	if result.Source == "" {
		t.Fatal("generated source is empty")
	}

	// Verify the disables rule compiled to the vipDiscount field
	if !strings.Contains(result.Source, "VipDiscount: FieldStatus") {
		t.Error("generated source missing VipDiscount FieldStatus")
	}

	// Write to temp directory and test
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "testpkg"), 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "testpkg", "test_gen.go"), []byte(result.Source), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	testMain := `package testpkg

import "testing"

func TestDisabledField(t *testing.T) {
	var f TestFields
	f.Country = "US"
	var prev TestFields

	avail := Check(f, TestConditions{}, prev)

	// country should be enabled
	if !avail.Country.Enabled {
		t.Errorf("country should be enabled")
	}

	// vipDiscount: disables when country != "US". Since country IS "US", the disables expr is false, so vipDiscount should be enabled
	if !avail.VipDiscount.Enabled {
		t.Errorf("vipDiscount should be enabled when country == US (disables expr is false)")
	}
}

func TestDisabledWhenNotUS(t *testing.T) {
	var f TestFields
	f.Country = "CA"
	var prev TestFields

	avail := Check(f, TestConditions{}, prev)

	// country should be enabled
	if !avail.Country.Enabled {
		t.Errorf("country should be enabled")
	}

	// vipDiscount: disables when country != "US". Since country is CA, the disables expr is true, so vipDiscount should be disabled
	if avail.VipDiscount.Enabled {
		t.Errorf("vipDiscount should NOT be enabled when country != US")
	}
}

`
	if err := os.WriteFile(filepath.Join(tmpDir, "testpkg", "test_gen_test.go"), []byte(testMain), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	goMod := "module testpkg\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(string(output), "PASS") {
		t.Errorf("expected PASS in test output, got:\n%s", output)
	}
}
