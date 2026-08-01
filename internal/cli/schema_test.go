package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSchema(t *testing.T) {
	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, "test.umpire.json")

	schemaContent := `{
  "fields": [
    {"name": "country", "required": true},
    {"name": "promoCode", "isEmpty": "string"}
  ],
  "conditions": [
    {"name": "userRole", "type": "string"},
    {"name": "isGuest", "type": "boolean"}
  ],
  "rules": [
    {
      "type": "enabledWhen",
      "field": "country",
      "expr": {"op": "present", "field": "country"}
    }
  ]
}`

	if err := os.WriteFile(schemaPath, []byte(schemaContent), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatalf("LoadSchema() error: %v", err)
	}

	if len(s.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(s.Fields))
	}
	if s.Fields[0].Name != "country" {
		t.Errorf("expected first field name 'country', got %q", s.Fields[0].Name)
	}
	if !s.Fields[0].Required {
		t.Error("expected country field to be required")
	}
	if s.Fields[1].Name != "promoCode" {
		t.Errorf("expected second field name 'promoCode', got %q", s.Fields[1].Name)
	}

	if len(s.Conditions) != 2 {
		t.Errorf("expected 2 conditions, got %d", len(s.Conditions))
	}
	if s.Conditions[0].Name != "userRole" {
		t.Errorf("expected first condition name 'userRole', got %q", s.Conditions[0].Name)
	}
	if s.Conditions[0].Type != "string" {
		t.Errorf("expected userRole type 'string', got %q", s.Conditions[0].Type)
	}

	if len(s.Rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(s.Rules))
	}
	if s.Rules[0].Type != "enabledWhen" {
		t.Errorf("expected rule type 'enabledWhen', got %q", s.Rules[0].Type)
	}
	if s.Rules[0].Field != "country" {
		t.Errorf("expected rule field 'country', got %q", s.Rules[0].Field)
	}
}

func TestLoadSchemaNonExistent(t *testing.T) {
	_, err := LoadSchema("/nonexistent/path/schema.umpire.json")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestLoadSchemaInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	badPath := filepath.Join(tmpDir, "bad.umpire.json")
	if err := os.WriteFile(badPath, []byte("not valid json{"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSchema(badPath)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestSchemaExists(t *testing.T) {
	tmpDir := t.TempDir()
	existPath := filepath.Join(tmpDir, "exists.umpire.json")
	if err := os.WriteFile(existPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if !SchemaExists(existPath) {
		t.Error("expected SchemaExists to return true for existing file")
	}
	if SchemaExists("/nonexistent/schema.umpire.json") {
		t.Error("expected SchemaExists to return false for non-existent file")
	}
	if !SchemaExists("-") {
		t.Error("expected SchemaExists to return true for stdin")
	}
}

func TestDefaultOutputPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"full suffix", "checkout.umpire.json", "checkout_gen.go"},
		{"json suffix", "data.json", "data_gen.go"},
		{"umpire suffix", "config.umpire", "config_gen.go"},
		{"with path", "/path/to/schema.umpire.json", "schema_gen.go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultOutputPath(tt.input)
			if got != tt.expected {
				t.Errorf("DefaultOutputPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
