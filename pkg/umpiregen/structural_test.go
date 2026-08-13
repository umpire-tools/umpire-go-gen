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
		"type Workflow struct", "type WorkflowNode struct", "type WorkflowEdge struct", "type WorkflowAction struct",
		"type WorkflowActionValue interface", "type WorkflowActionValueManual struct", "WorkflowActionKindManual WorkflowActionKind",
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
	umpireJSON := []byte(`{"version":1,"fields":{"title":{"isEmpty":"string","required":true},"workflowType":{"isEmpty":"string"}},"rules":[]}`)
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
	for _, want := range []string{"type Doc struct", "type DocWorkflowTypeValue string", "DocWorkflowTypeValuePipe DocWorkflowTypeValue",
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
	manual, ok := w.Nodes[0].Action.Value.(*WorkflowActionValueManual)
	if !ok || manual.Kind != WorkflowActionKindManual { t.Fatalf("union not decoded: %T %+v", w.Nodes[0].Action.Value, w.Nodes[0].Action.Value) }

	// Availability over the richer field types.
	var f WorkflowFields
	if err := json.Unmarshal([]byte(` + "`" + payload + "`" + `), &f); err != nil { t.Fatal(err) }
	// The rc.2 fixture supplies every condition referenced by availability rules;
	// do not rely on the Go bool zero value as an implicit condition default.
	av := Check(f, WorkflowConditions{AllowEdits: false}, WorkflowFields{})
	if !av.Title.Required { t.Fatal("title should be required/available") }
	if !av.Nodes.Satisfied { t.Fatal("nodes should be satisfied (non-empty)") }
	if !av.Edges.Enabled || av.Edges.Reason != nil || len(av.Edges.Reasons) != 0 {
		t.Fatalf("enabled edges must not carry a blocking reason: %+v", av.Edges)
	}
	if av.Status.Enabled || av.Status.Reason == nil || *av.Status.Reason != "Status editing is disabled" {
		t.Fatalf("allowEdits=false should disable status: %+v", av.Status)
	}
}
`
	runMerged(t, source, "smoke", testSrc)
}

func TestGenerateProfileStructural_SafelyQuotesArbitraryWireTags(t *testing.T) {
	profile := []byte(`{
		"$schema":"https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion":1,
		"valueSchema":{
			"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object",
			"properties":{"wire` + "`" + `injected":{"type":"string"}},
			"additionalProperties":false
		},
		"umpire":{
			"version":1,
			"fields":{"wire` + "`" + `injected":{"isEmpty":"string"}},
			"conditions":{"gate` + "`" + `injected":{"type":"boolean"}},
			"rules":[]
		}
	}`)
	source, issues, err := GenerateProfile(profile, Config{PkgName: "safetags", SchemaName: "Doc"})
	if err != nil || len(issues) != 0 {
		t.Fatalf("GenerateProfile() = issues %+v, err %v", issues, err)
	}
	testSrc := `
func TestSafeTags(t *testing.T) {
	_ = json.Valid
	fields, err := DecodeDoc([]byte("{\"wire` + "`" + `injected\":\"ok\"}"))
	if err != nil { t.Fatal(err) }
	if fields.WireInjected == nil || *fields.WireInjected != "ok" { t.Fatalf("DecodeDoc did not preserve wire name: %+v", fields) }
	_ = DocConditions{GateInjected: true}
}
`
	runMerged(t, source, "safetags", testSrc)
}

func TestGenerateProfileStructural_ArrayDefinitionAvailabilityUsesLength(t *testing.T) {
	profile := []byte(`{
		"$schema":"https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion":1,
		"valueSchema":{
			"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object",
			"properties":{"items":{"$ref":"#/$defs/itemsAlias"},"dependent":{"type":"string"}},
			"additionalProperties":false,
			"$defs":{"items":{"type":"array","items":{"type":"integer"}},"itemsAlias":{"$ref":"#/$defs/items"}}
		},
		"umpire":{
			"version":1,
			"fields":{"items":{"isEmpty":"array"},"dependent":{}},
			"rules":[{"type":"requires","field":"dependent","dependency":"items"}]
		}
	}`)
	source, issues, err := GenerateProfile(profile, Config{PkgName: "arrayavailability", SchemaName: "Doc"})
	if err != nil || len(issues) != 0 {
		t.Fatalf("GenerateProfile() = issues %+v, err %v", issues, err)
	}
	testSrc := `
func TestArrayAvailability(t *testing.T) {
	_ = json.Valid
	fields, err := DecodeDoc([]byte("{\"items\":[]}"))
	if err != nil { t.Fatal(err) }
	if fields.Items == nil || *fields.Items == nil { t.Fatal("DecodeDoc lost explicit empty array") }
	availability := Check(fields, DocConditions{}, DocFields{})
	if availability.Items.Satisfied || depSatisfied(fields, "Items") || availability.Dependent.Enabled {
		t.Fatalf("empty pointer-to-array was satisfied: items=%+v dependent=%+v", availability.Items, availability.Dependent)
	}
}
`
	runMerged(t, source, "arrayavailability", testSrc)
}

func TestGenerateProfileStructural_IntegralRequiredAndOptionalShapes(t *testing.T) {
	profile := []byte(`{
		"$schema":"https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion":1,
		"valueSchema":{
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"type":"object",
			"properties":{
				"mode":{"type":"integer","enum":[1.0,2e0]},
				"counts":{"type":"array","items":{"type":"integer"}},
				"matrix":{"type":"array","items":{"type":"array","items":{"type":"integer"}}},
				"optionalCount":{"type":"integer"},
				"optionalCounts":{"type":"array","items":{"type":"integer"}}
			},
			"required":["mode","counts","matrix"],
			"additionalProperties":false
		},
		"umpire":{
			"version":1,
			"fields":{
				"mode":{"isEmpty":"number"},
				"counts":{"isEmpty":"array"},
				"matrix":{"isEmpty":"array"},
				"optionalCount":{"isEmpty":"number"},
				"optionalCounts":{"isEmpty":"array"}
			},
			"rules":[]
		}
	}`)
	source, issues, err := GenerateProfile(profile, Config{PkgName: "integralshapes", SchemaName: "Doc"})
	if err != nil || len(issues) != 0 {
		t.Fatalf("GenerateProfile() = issues %+v, err %v", issues, err)
	}
	testSrc := `
func TestIntegralShapes(t *testing.T) {
	_ = json.Valid
	valid := []byte("{\"mode\":1e0,\"counts\":[1.0,2e0],\"matrix\":[[1e0],[2.0]],\"optionalCount\":1.0,\"optionalCounts\":[2e0]}")
	fields, err := DecodeDoc(valid)
	if err != nil { t.Fatal(err) }
	if fields.Mode == nil || *fields.Mode != DocModeValueValue1 { t.Fatalf("required integer enum: %#v", fields.Mode) }
	if fields.Counts == nil || len(*fields.Counts) != 2 || (*fields.Counts)[0] != 1 || (*fields.Counts)[1] != 2 { t.Fatalf("required integer array: %#v", fields.Counts) }
	if fields.Matrix == nil || len(*fields.Matrix) != 2 || len((*fields.Matrix)[0]) != 1 || (*fields.Matrix)[0][0] != 1 || (*fields.Matrix)[1][0] != 2 { t.Fatalf("required nested integer array: %#v", fields.Matrix) }
	if fields.OptionalCount == nil || *fields.OptionalCount != 1 { t.Fatalf("optional integer: %#v", fields.OptionalCount) }
	if fields.OptionalCounts == nil || len(*fields.OptionalCounts) != 1 || (*fields.OptionalCounts)[0] != 2 { t.Fatalf("optional integer array: %#v", fields.OptionalCounts) }

	minimal, err := DecodeDoc([]byte("{\"mode\":2.0,\"counts\":[],\"matrix\":[]}"))
	if err != nil { t.Fatal(err) }
	if minimal.Mode == nil || *minimal.Mode != DocModeValueValue2 { t.Fatalf("decimal enum: %#v", minimal.Mode) }
	if minimal.Counts == nil || *minimal.Counts == nil || minimal.Matrix == nil || *minimal.Matrix == nil { t.Fatalf("required empty arrays lost presence: %#v", minimal) }
	if minimal.OptionalCount != nil || minimal.OptionalCounts != nil { t.Fatalf("omitted optional pointers became present: %#v", minimal) }

	for _, test := range []struct {
		name string
		input string
		code string
		path string
	}{
		{name:"fractional enum", input:"{\"mode\":1.5,\"counts\":[],\"matrix\":[]}", code:"type", path:"/mode"},
		{name:"unsafe enum", input:"{\"mode\":9007199254740992,\"counts\":[],\"matrix\":[]}", code:"safeInteger", path:"/mode"},
		{name:"fractional array", input:"{\"mode\":1,\"counts\":[1,2.5],\"matrix\":[]}", code:"type", path:"/counts/1"},
		{name:"unsafe nested array", input:"{\"mode\":1,\"counts\":[],\"matrix\":[[9007199254740992]]}", code:"safeInteger", path:"/matrix/0/0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			issues, err := ValidateDocJSON([]byte(test.input))
			if err != nil { t.Fatal(err) }
			found := false
			for _, issue := range issues { found = found || issue.Code == test.code && issue.Path == test.path }
			if !found { t.Fatalf("issues = %+v, want %s at %s", issues, test.code, test.path) }
			if _, err := DecodeDoc([]byte(test.input)); err == nil { t.Fatal("DecodeDoc accepted structurally invalid integer") }
		})
	}
}
`
	runMerged(t, source, "integralshapes", testSrc)
}

func TestGenerateProfileStructural_NamedStringEnumUsesStringEmptiness(t *testing.T) {
	profile := []byte(`{
		"$schema":"https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion":1,
		"valueSchema":{
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"type":"object",
			"properties":{
				"choice":{"type":"string","enum":["ready","later"]},
				"note":{"type":"string"}
			},
			"additionalProperties":false
		},
		"umpire":{
			"version":1,
			"fields":{"choice":{"isEmpty":"string"},"note":{"isEmpty":"string"}},
			"rules":[]
		}
	}`)
	source, issues, err := GenerateProfile(profile, Config{PkgName: "enumempty", SchemaName: "Doc"})
	if err != nil {
		t.Fatalf("GenerateProfile() error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("GenerateProfile() issues: %+v", issues)
	}
	testSrc := `
func TestNamedEnumEmptiness(t *testing.T) {
	empty := DocChoiceValue("")
	note := "present"
	availability := Check(DocFields{Choice: &empty, Note: &note}, DocConditions{}, DocFields{})
	if availability.Choice.Satisfied {
		t.Fatalf("empty named string enum was satisfied: %+v", availability.Choice)
	}
	if depSatisfied(DocFields{Choice: &empty, Note: &note}, "Choice") {
		t.Fatal("depSatisfied accepted an empty named string enum")
	}

	var fields DocFields
	if err := json.Unmarshal([]byte("{\"choice\":\"ready\"}"), &fields); err != nil { t.Fatal(err) }
	availability = Check(fields, DocConditions{}, DocFields{})
	if !availability.Choice.Satisfied || !depSatisfied(fields, "Choice") {
		t.Fatalf("non-empty named string enum was unsatisfied: %+v", availability.Choice)
	}
}
`
	runMerged(t, source, "enumempty", testSrc)
}
