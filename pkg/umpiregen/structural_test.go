package umpiregen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umpire-tools/umpire-go-gen/pkg/internal/testutil"
)

// loadAvenorProfile returns the avenor-workflow profile document.
func loadAvenorProfile(t *testing.T) []byte {
	t.Helper()
	paths := []string{
		"../../spec/profiles/conformance/fixtures/avenor-workflow.json",
		"../spec/profiles/conformance/fixtures/avenor-workflow.json",
	}
	var data []byte
	for _, p := range paths {
		if b, err := os.ReadFile(filepath.FromSlash(p)); err == nil {
			data = b
			break
		}
	}
	if data == nil {
		t.Skip("avenor-workflow fixture not found")
	}
	var fix struct {
		Profile json.RawMessage `json:"profile"`
	}
	if err := json.Unmarshal(data, &fix); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return fix.Profile
}

// TestGenerateProfileStructural_Avenor verifies the profile path emits a single
// merged file with structural types + availability Check/Challenge, compiles,
// and never exposes any.
func TestGenerateProfileStructural_Avenor(t *testing.T) {
	profile := loadAvenorProfile(t)

	source, issues, err := GenerateProfile(profile, Config{PkgName: "smoke", SchemaName: "Workflow"})
	if err != nil {
		t.Fatalf("GenerateProfile() error: %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("expected no definition issues, got: %v", issues)
	}

	for _, want := range []string{
		"type Workflow struct", "type Node struct", "type Edge struct", "type Action struct",
		"type ActionKind string", "ActionKindManual ActionKind",
		"type WorkflowFields struct", "func Check(", "func Challenge(", "func (v Workflow) Validate()",
		"type Issue struct",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("merged output missing %q", want)
		}
	}
	if strings.Contains(source, "map[string]any") {
		t.Errorf("merged output must not expose any (map[string]any)")
	}
	testutil.AssertGeneratedPackageCompiles(t, source)
}

// TestGenerateComposedStructural composes an umpire vessel with a valueSchema and
// asserts structural + availability output compiles.
func TestGenerateComposedStructural(t *testing.T) {
	umpireJSON := []byte(`{"version":1,"fields":{"title":{"isEmpty":"string","required":true}},"rules":[]}`)
	valueSchemaJSON := []byte(`{
		"$schema":"https://json-schema.org/draft/2020-12/schema",
		"type":"object",
		"properties":{"title":{"type":"string","minLength":1},"workflowType":{"type":"string","enum":["pipe","fan"]}},
		"required":["title"],
		"additionalProperties":false
	}`)

	source, issues, err := GenerateComposed(umpireJSON, valueSchemaJSON, Config{PkgName: "smoke", SchemaName: "Doc"})
	if err != nil {
		t.Fatalf("GenerateComposed() error: %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("expected no issues, got: %v", issues)
	}
	for _, want := range []string{"type Doc struct", "type WorkflowType string", "WorkflowTypePipe WorkflowType",
		"type DocFields struct", "func Check(", "func Challenge("} {
		if !strings.Contains(source, want) {
			t.Errorf("merged output missing %q", want)
		}
	}
	testutil.AssertGeneratedPackageCompiles(t, source)
}
