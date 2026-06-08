package cli

import (
	"testing"
)

func TestParseFlags_AllFlags(t *testing.T) {
	cfg, err := ParseFlags([]string{"-i", "testdata/schema.umpire.json", "-o", "./out", "-pkg", "myschema"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.InputPath != "testdata/schema.umpire.json" {
		t.Errorf("InputPath = %q, want %q", cfg.InputPath, "testdata/schema.umpire.json")
	}
	if cfg.OutputDir != "./out" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "./out")
	}
	if cfg.PkgName != "myschema" {
		t.Errorf("PkgName = %q, want %q", cfg.PkgName, "myschema")
	}
}

func TestParseFlags_DefaultOutputDir(t *testing.T) {
	cfg, err := ParseFlags([]string{"-i", "schema.umpire.json", "-pkg", "myschema"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OutputDir != "." {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, ".")
	}
}

func TestParseFlags_DefaultFieldsName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"checkout", "checkout.umpire.json", "CheckoutFields"},
		{"user_profile", "user_profile.umpire.json", "UserProfileFields"},
		{"my-schema", "my-schema.umpire.json", "MySchemaFields"},
		{"A.B_C", "A.B_C.umpire.json", "ABCFields"},
		{"nested/path/pkg.umpire.json", "nested/path/pkg.umpire.json", "PkgFields"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseFlags([]string{"-i", tt.input, "-pkg", "mypkg"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.FieldsName != tt.expect {
				t.Errorf("FieldsName = %q, want %q", cfg.FieldsName, tt.expect)
			}
		})
	}
}

func TestParseFlags_DefaultConditionsName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"checkout", "checkout.umpire.json", "CheckoutConditions"},
		{"user_profile", "user_profile.umpire.json", "UserProfileConditions"},
		{"my-schema", "my-schema.umpire.json", "MySchemaConditions"},
		{"A.B_C", "A.B_C.umpire.json", "ABCConditions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseFlags([]string{"-i", tt.input, "-pkg", "mypkg"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.ConditionsName != tt.expect {
				t.Errorf("ConditionsName = %q, want %q", cfg.ConditionsName, tt.expect)
			}
		})
	}
}

func TestParseFlags_MissingRequiredInput(t *testing.T) {
	_, err := ParseFlags([]string{"-pkg", "myschema"})
	if err == nil {
		t.Fatal("expected error for missing -i flag, got nil")
	}
}

func TestParseFlags_MissingRequiredPkg(t *testing.T) {
	_, err := ParseFlags([]string{"-i", "schema.umpire.json"})
	if err == nil {
		t.Fatal("expected error for missing -pkg flag, got nil")
	}
}

func TestParseFlags_CustomFieldsOverride(t *testing.T) {
	cfg, err := ParseFlags([]string{"-i", "checkout.umpire.json", "-pkg", "pkg", "-fields", "CustomFields"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.FieldsName != "CustomFields" {
		t.Errorf("FieldsName = %q, want %q", cfg.FieldsName, "CustomFields")
	}
}

func TestParseFlags_CustomConditionsOverride(t *testing.T) {
	cfg, err := ParseFlags([]string{"-i", "checkout.umpire.json", "-pkg", "pkg", "-conditions", "CustomConditions"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ConditionsName != "CustomConditions" {
		t.Errorf("ConditionsName = %q, want %q", cfg.ConditionsName, "CustomConditions")
	}
}

func TestParseFlags_UpperCaseExtensionIgnored(t *testing.T) {
	cfg, err := ParseFlags([]string{"-i", "checkout.UMPIRE.JSON", "-pkg", "pkg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.FieldsName != "CheckoutFields" {
		t.Errorf("FieldsName = %q, want %q", cfg.FieldsName, "CheckoutFields")
	}
	if cfg.ConditionsName != "CheckoutConditions" {
		t.Errorf("ConditionsName = %q, want %q", cfg.ConditionsName, "CheckoutConditions")
	}
}

func TestFieldsDefault(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "checkout.umpire.json", "CheckoutFields"},
		{"with path", "/path/to/availability.umpire.json", "AvailabilityFields"},
		{"kebab-case", "my-checkout.umpire.json", "MyCheckoutFields"},
		{"snake_case", "my_checkout.umpire.json", "MyCheckoutFields"},
		{"spaces", "my checkout.umpire.json", "MyCheckoutFields"},
		{"mixed", "my-checkout_schema.umpire.json", "MyCheckoutSchemaFields"},
		{"just json suffix", "data.json", "DataFields"},
		{"just umpire suffix", "data.umpire", "DataFields"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fieldsDefault(tt.input)
			if got != tt.expected {
				t.Errorf("fieldsDefault(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestConditionsDefault(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "checkout.umpire.json", "CheckoutConditions"},
		{"with path", "/path/to/availability.umpire.json", "AvailabilityConditions"},
		{"kebab-case", "my-checkout.umpire.json", "MyCheckoutConditions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := conditionsDefault(tt.input)
			if got != tt.expected {
				t.Errorf("conditionsDefault(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"checkout", "Checkout"},
		{"user_profile", "UserProfile"},
		{"my-schema", "MySchema"},
		{"A.B_C", "ABC"},
		{"simple", "Simple"},
		{"alreadyPascal", "AlreadyPascal"},
		{"__double__underscores__", "DoubleUnderscores"},
		{"---multiple---hyphens---", "MultipleHyphens"},
		{"mixed_case-and.dotted", "MixedCaseAndDotted"},
		{"a", "A"},
		{"123abc", "123abc"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toPascalCase(tt.input)
			if got != tt.expect {
				t.Errorf("toPascalCase(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}
