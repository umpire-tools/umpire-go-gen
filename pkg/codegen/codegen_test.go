package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umpire-tools/umpire-gen/pkg/schema"
)

func TestGenerateSampleSchema(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "country", Required: true, IsEmpty: "string"},
			{Name: "promoCode"},
			{Name: "itemsCount", TypeHint: "number"},
			{Name: "paymentCreditCard"},
			{Name: "paymentBankTransfer"},
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
				Reason: "Country is required",
			},
			{
				Type:     "requires",
				Field:    "promoCode",
				Requires: []string{"country"},
			},
			{
				Type:        "excluded",
				Description: "Legacy shipping rule",
			},
		},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	gen := NewGenerator("Checkout", "availability", "CheckoutFields", "CheckoutConditions", inferred)
	result, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	if result.Source == "" {
		t.Fatal("generated source is empty")
	}

	// Verify expected struct names are present
	expected := []string{
		"package availability",
		"type CheckoutFields struct",
		"type CheckoutConditions struct",
		"type CheckoutAvailability struct",
		"type FieldStatus struct",
		"Country FieldStatus",
		"PromoCode FieldStatus",
		"ItemsCount FieldStatus",
		"ActivePaymentBranch PaymentBranch",
		"type PaymentBranch int",
	}

	for _, exp := range expected {
		if !strings.Contains(result.Source, exp) {
			t.Errorf("generated source missing %q:\n%s", exp, result.Source)
		}
	}
}

func TestGenerateWritesToFile(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "country", Required: true, IsEmpty: "string"},
		},
		Conditions: []schema.ConditionDef{
			{Name: "isAdmin", Type: "boolean"},
		},
		Rules: []schema.Rule{},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	gen := NewGenerator("Test", "testpkg", "TestFields", "TestConditions", inferred)
	result, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// Write to temp file and verify it parses
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_umpire.go")
	if err := os.WriteFile(tmpFile, []byte(result.Source), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	// Verify the file can be parsed (basic check)
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(data) != result.Source {
		t.Error("file content does not match generated source")
	}

	// Verify expected content is in the file
	if !strings.Contains(string(data), "package testpkg") {
		t.Error("file missing package declaration")
	}
	if !strings.Contains(string(data), "type TestFields struct") {
		t.Error("file missing TestFields struct")
	}
}
