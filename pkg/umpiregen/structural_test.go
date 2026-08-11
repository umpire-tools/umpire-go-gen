package umpiregen

import (
	"encoding/json"
	"os"
	"os/exec"
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
	if strings.Contains(source, "map[string]any") || strings.Contains(source, "]any") || strings.Contains(source, "*any") {
		t.Errorf("merged output must not expose any")
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

// runMerged compiles the merged output and runs the given *_test.go body via
// `go test`, giving end-to-end runtime coverage of structural + availability.
func runMerged(t *testing.T, source, pkg, testSrc string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module merged\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gen.go"), []byte(source), 0644); err != nil {
		t.Fatalf("gen.go: %v", err)
	}
	header := "package " + pkg + "\n\nimport (\n\t\"encoding/json\"\n\t\"testing\"\n)\n\n"
	if err := os.WriteFile(filepath.Join(dir, "gen_test.go"), []byte(header+testSrc), 0644); err != nil {
		t.Fatalf("gen_test.go: %v", err)
	}
	cmd := exec.Command("go", "test", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(dir, ".gocache"),
		"GOMODCACHE="+filepath.Join(dir, ".gomodcache"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("merged runtime test failed: %v\n%s\n--- source ---\n%s", err, out, source)
	}
}

// TestGenerateProfileStructural_Runtime decodes into both the structural root type
// (Validate) and the availability Fields type (Check) from one merged file.
func TestGenerateProfileStructural_Runtime(t *testing.T) {
	profile := loadAvenorProfile(t)
	source, issues, err := GenerateProfile(profile, Config{PkgName: "smoke", SchemaName: "Workflow"})
	if err != nil {
		t.Fatalf("GenerateProfile() error: %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}

	const payload = `{"nodes":[{"id":"n1","action":{"kind":"manual","instructions":"go here"}}],"title":"my workflow"}`
	testSrc := `
func TestMergedRuntime(t *testing.T) {
	// Structural decode + validation.
	var w Workflow
	if err := json.Unmarshal([]byte(` + "`" + payload + "`" + `), &w); err != nil { t.Fatal(err) }
	if v := w.Validate(); len(v) != 0 { t.Fatalf("validate: %+v", v) }
	if w.Nodes[0].Action.Kind != ActionKindManual { t.Fatalf("union not decoded: %+v", w.Nodes[0].Action) }

	// Availability over the richer field types.
	var f WorkflowFields
	if err := json.Unmarshal([]byte(` + "`" + payload + "`" + `), &f); err != nil { t.Fatal(err) }
	av := Check(f, WorkflowConditions{}, WorkflowFields{})
	if !av.Title.Required { t.Fatal("title should be required/available") }
	if !av.Nodes.Satisfied { t.Fatal("nodes should be satisfied (non-empty)") }
}
`
	runMerged(t, source, "smoke", testSrc)
}
