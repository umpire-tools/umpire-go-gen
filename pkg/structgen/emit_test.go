package structgen

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGenerated builds a Spec from valueSchema, emits source, and runs the
// provided *_test.go body (in the generated package) via `go test`.
func runGenerated(t *testing.T, valueSchema, root, pkg, testSrc string) {
	t.Helper()
	spec, err := Build([]byte(valueSchema), root)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	em, err := Emit(spec, pkg)
	if err != nil {
		t.Fatalf("Emit() error: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generatedsmoke\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gen.go"), []byte(em.Source), 0644); err != nil {
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
		t.Fatalf("generated behavior test failed: %v\n%s\n--- source ---\n%s", err, out, em.Source)
	}
}

// aivenValueSchema loads the avenor-workflow fixture's valueSchema.
func aivenValueSchema(t *testing.T) []byte {
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
		Profile struct {
			ValueSchema json.RawMessage `json:"valueSchema"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &fix); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return fix.Profile.ValueSchema
}

func TestEmitCompilesAvenor(t *testing.T) {
	spec, err := Build(aivenValueSchema(t), "Workflow")
	if err != nil {
		t.Fatalf("Build(): %v", err)
	}
	em, err := Emit(spec, "smoke")
	if err != nil {
		t.Fatalf("Emit(): %v", err)
	}
	if strings.Contains(em.Source, "any") {
		t.Fatalf("emitted source must not expose `any`:\n%s", em.Source)
	}
	for _, want := range []string{"type Workflow struct", "type Node struct", "type Edge struct", "type Action struct",
		"type ActionKind string", "ActionKindManual ActionKind", "func (u *Action) UnmarshalJSON"} {
		if !strings.Contains(em.Source, want) {
			t.Errorf("missing %q in generated source", want)
		}
	}
}

func TestEmitUnionDecodeBehavior(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"action":{
				"oneOf":[
					{"type":"object","properties":{"kind":{"const":"manual"},"instructions":{"type":"string"}},"required":["kind","instructions"]},
					{"type":"object","properties":{"kind":{"const":"run"},"command":{"type":"string"},"timeout":{"type":"integer"}},"required":["kind","command"]}
				]
			}
		},
		"$defs":{}
	}`
	testSrc := `
func TestUnionDecode(t *testing.T) {
	var a Action
	if err := json.Unmarshal([]byte(` + "`{\"kind\":\"run\",\"command\":\"go\",\"timeout\":5}`" + `), &a); err != nil { t.Fatal(err) }
	if a.Kind != ActionKindRun { t.Fatalf("Kind=%v", a.Kind) }
	if a.Command == nil || *a.Command != "go" { t.Fatalf("Command=%v", a.Command) }
	if a.Timeout == nil || *a.Timeout != 5 { t.Fatalf("Timeout=%v", a.Timeout) }
	if err := json.Unmarshal([]byte(` + "`{\"kind\":\"manual\",\"instructions\":\"x\"}`" + `), &a); err != nil { t.Fatal(err) }
	if a.Kind != ActionKindManual || a.Instructions == nil || *a.Instructions != "x" { t.Fatal("manual decode wrong") }
	if err := json.Unmarshal([]byte(` + "`{\"kind\":\"bogus\"}`" + `), &a); err == nil { t.Fatal("want error for unknown discriminator") }
	if err := json.Unmarshal([]byte(` + "`{}`" + `), &a); err == nil { t.Fatal("want error for missing discriminator") }
	if string(ActionKindManual) != "manual" || string(ActionKindRun) != "run" { t.Fatal("wire values wrong") }
}
`
	runGenerated(t, vs, "Job", "smoke", testSrc)
}

func TestEmitObjectEnumTags(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"title":{"type":"string"},
			"workflowType":{"type":"string","enum":["pipeline","fanout"]},
			"profile":{"type":"object","properties":{"nickname":{"type":"string"}},"required":["nickname"]}
		}
	}`
	testSrc := `
func TestDecodeAndTags(t *testing.T) {
	var d Doc
	if err := json.Unmarshal([]byte(` + "`{\"title\":\"t\",\"workflowType\":\"pipeline\",\"profile\":{\"nickname\":\"n\"}}`" + `), &d); err != nil { t.Fatal(err) }
	if d.Title == nil || *d.Title != "t" || d.WorkflowType == nil || *d.WorkflowType != WorkflowTypePipeline { t.Fatalf("decode wrong: %+v", d) }
	if d.Profile == nil || d.Profile.Nickname != "n" { t.Fatalf("profile wrong: %+v", d.Profile) }
	if string(WorkflowTypeFanout) != "fanout" { t.Fatal("fanout wire wrong") }
}
`
	runGenerated(t, vs, "Doc", "smoke", testSrc)
}

func TestEmitValidationAndStrictDecode(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"title":{"type":"string","minLength":3,"maxLength":10},
			"count":{"type":"integer","minimum":1,"maximum":5},
			"tags":{"type":"array","items":{"type":"string"},"maxItems":2},
			"nodes":{"type":"array","items":{"$ref":"#/$defs/node"},"minItems":1}
		},
		"required":["title","nodes"],
		"$defs":{"node":{"type":"object","properties":{"id":{"type":"string"}},"additionalProperties":false}}
	}`
	testSrc := `
func TestStrictAndValidate(t *testing.T) {
	hasIssue := func(t *testing.T, issues []Issue, code, path string) bool {
		for _, i := range issues {
			if i.Code == code && i.Path == path {
				return true
			}
		}
		return false
	}
	// valid
	var d Doc
	if err := json.Unmarshal([]byte(` + "`{\"title\":\"abc\",\"count\":3,\"tags\":[\"a\"],\"nodes\":[{\"id\":\"n\"}]}`" + `), &d); err != nil { t.Fatal(err) }
	if v := d.Validate(); len(v) != 0 { t.Fatalf("want clean, got %+v", v) }
	// rune-based minLength: 3 x-e-acute = 3 code points, valid at minLength 3
	if err := json.Unmarshal([]byte(` + "`{\"title\":\"\\u00e9\\u00e9\\u00e9\",\"nodes\":[{\"id\":\"n\"}]}`" + `), &d); err != nil { t.Fatal(err) }
	if v := d.Validate(); len(v) != 0 { t.Fatalf("rune length: got %+v", v) }
	// title too short
	if err := json.Unmarshal([]byte(` + "`{\"title\":\"ab\",\"nodes\":[{\"id\":\"n\"}]}`" + `), &d); err != nil { t.Fatal(err) }
	if !hasIssue(t, d.Validate(), "minLength", "/title") { t.Fatal("want minLength @ /title") }
	// count out of range (optional)
	if err := json.Unmarshal([]byte(` + "`{\"title\":\"abc\",\"count\":9,\"nodes\":[{\"id\":\"n\"}]}`" + `), &d); err != nil { t.Fatal(err) }
	if !hasIssue(t, d.Validate(), "maximum", "/count") { t.Fatal("want maximum @ /count") }
	// nodes empty -> minItems
	if err := json.Unmarshal([]byte(` + "`{\"title\":\"abc\",\"nodes\":[]}`" + `), &d); err != nil { t.Fatal(err) }
	if !hasIssue(t, d.Validate(), "minItems", "/nodes") { t.Fatal("want minItems @ /nodes") }
	// unknown property -> strict decode error
	if err := json.Unmarshal([]byte(` + "`{\"title\":\"abc\",\"nodes\":[{\"id\":\"n\"}],\"bogus\":1}`" + `), &d); err == nil { t.Fatal("want unknown-field error") }
	// missing required nodes
	if err := json.Unmarshal([]byte(` + "`{\"title\":\"abc\"}`" + `), &d); err == nil { t.Fatal("want missing-required error") }
	// null required nodes
	if err := json.Unmarshal([]byte(` + "`{\"title\":\"abc\",\"nodes\":null}`" + `), &d); err == nil { t.Fatal("want null-required error") }
}
`
	runGenerated(t, vs, "Doc", "smoke", testSrc)
}
