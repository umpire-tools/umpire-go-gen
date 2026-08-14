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
	em, err := Emit(spec, EmitOptions{PkgName: pkg})
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

func TestEmitPrimitiveEnumsAndExclusiveBounds(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"flag":{"type":"boolean","enum":[true,false]},
			"label":{"type":"string","enum":["x","okay","oversized"],"minLength":2,"maxLength":4},
			"limits":{"type":"integer","enum":[-1,2,4],"minimum":0,"maximum":3},
			"count":{"type":"integer","enum":[1,2,4],"exclusiveMinimum":1,"exclusiveMaximum":4},
			"ratio":{"type":"number","enum":[0.5,1,2],"exclusiveMinimum":0.5,"exclusiveMaximum":2}
		}
	}`
	testSrc := `
func hasStructural(issues []DocStructuralIssue, code, path string) bool {
	for _, issue := range issues { if issue.Code == code && issue.Path == path { return true } }
	return false
}
func hasTyped(issues []Issue, code, path string) bool {
	for _, issue := range issues { if issue.Code == code && issue.Path == path { return true } }
	return false
}
func TestPrimitiveEnumsAndBounds(t *testing.T) {
	var d Doc
	if err := json.Unmarshal([]byte("{\"flag\":true,\"label\":\"okay\",\"limits\":2,\"count\":2,\"ratio\":1}"), &d); err != nil { t.Fatal(err) }
	if d.Flag == nil || *d.Flag != DocFlagValueTrue || d.Count == nil || *d.Count != DocCountValueValue2 || d.Ratio == nil || *d.Ratio != DocRatioValueValue2 { t.Fatalf("decoded enums: %+v", d) }
	if issues := d.Validate(); len(issues) != 0 { t.Fatalf("typed issues: %+v", issues) }

	// Every value below remains a valid enum member but violates an attached
	// lower/string constraint. Typed Validate must enforce both layers.
	if err := json.Unmarshal([]byte("{\"label\":\"x\",\"limits\":-1,\"count\":1,\"ratio\":0.5}"), &d); err != nil { t.Fatal(err) }
	typed := d.Validate()
	if !hasTyped(typed, "minLength", "/label") || !hasTyped(typed, "minimum", "/limits") || !hasTyped(typed, "exclusiveMinimum", "/count") || !hasTyped(typed, "exclusiveMinimum", "/ratio") { t.Fatalf("typed lower issues: %+v", typed) }

	// Exercise exclusiveMaximum through typed Validate for both integer and number.
	if err := json.Unmarshal([]byte("{\"label\":\"oversized\",\"limits\":4,\"count\":4,\"ratio\":2}"), &d); err != nil { t.Fatal(err) }
	typed = d.Validate()
	if !hasTyped(typed, "maxLength", "/label") || !hasTyped(typed, "maximum", "/limits") || !hasTyped(typed, "exclusiveMaximum", "/count") || !hasTyped(typed, "exclusiveMaximum", "/ratio") { t.Fatalf("typed upper issues: %+v", typed) }

	issues, err := ValidateDocJSON([]byte("{\"label\":\"x\",\"limits\":-1,\"count\":1,\"ratio\":0.5}"))
	if err != nil { t.Fatal(err) }
	if !hasStructural(issues, "minLength", "/label") || !hasStructural(issues, "minimum", "/limits") || !hasStructural(issues, "exclusiveMinimum", "/count") || !hasStructural(issues, "exclusiveMinimum", "/ratio") { t.Fatalf("raw lower issues: %+v", issues) }
	issues, err = ValidateDocJSON([]byte("{\"label\":\"oversized\",\"limits\":4,\"count\":4,\"ratio\":2}"))
	if err != nil { t.Fatal(err) }
	if !hasStructural(issues, "maxLength", "/label") || !hasStructural(issues, "maximum", "/limits") || !hasStructural(issues, "exclusiveMaximum", "/count") || !hasStructural(issues, "exclusiveMaximum", "/ratio") { t.Fatalf("raw upper issues: %+v", issues) }
	issues, err = ValidateDocJSON([]byte("{\"flag\":null,\"label\":\"no\",\"limits\":3,\"count\":3,\"ratio\":1.5}"))
	if err != nil { t.Fatal(err) }
	if !hasStructural(issues, "type", "/flag") || !hasStructural(issues, "enum", "/label") || !hasStructural(issues, "enum", "/limits") || !hasStructural(issues, "enum", "/count") || !hasStructural(issues, "enum", "/ratio") { t.Fatalf("raw membership issues: %+v", issues) }
}
`
	runGenerated(t, vs, "Doc", "primitiveenums", testSrc)
}

func TestEmitCompilesAvenor(t *testing.T) {
	spec, err := Build(aivenValueSchema(t), "Workflow")
	if err != nil {
		t.Fatalf("Build(): %v", err)
	}
	em, err := Emit(spec, EmitOptions{PkgName: "smoke"})
	if err != nil {
		t.Fatalf("Emit(): %v", err)
	}
	if strings.Contains(em.Source, "map[string]any") || strings.Contains(em.Source, "]any") || strings.Contains(em.Source, "*any") {
		t.Fatalf("emitted source must not expose `any`:\n%s", em.Source)
	}
	for _, want := range []string{"type Workflow struct", "type WorkflowNode struct", "type WorkflowEdge struct", "type WorkflowAction struct",
		"type WorkflowActionValue interface", "type WorkflowActionValueManual struct", "WorkflowActionKindManual WorkflowActionKind", "func (u *WorkflowAction) UnmarshalJSON"} {
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
	var a JobAction
	if err := json.Unmarshal([]byte(` + "`{\"kind\":\"run\",\"command\":\"go\",\"timeout\":5}`" + `), &a); err != nil { t.Fatal(err) }
	run, ok := a.Value.(*JobActionValueRun)
	if !ok || run.Kind != JobActionKindRun || run.Command != "go" || run.Timeout == nil || *run.Timeout != 5 { t.Fatalf("run branch=%T %+v", a.Value, a.Value) }
	if err := json.Unmarshal([]byte(` + "`{\"kind\":\"manual\",\"instructions\":\"x\"}`" + `), &a); err != nil { t.Fatal(err) }
	manual, ok := a.Value.(*JobActionValueManual)
	if !ok || manual.Kind != JobActionKindManual || manual.Instructions != "x" { t.Fatal("manual decode wrong") }
	if err := json.Unmarshal([]byte(` + "`{\"kind\":\"manual\",\"instructions\":\"x\",\"command\":\"go\"}`" + `), &a); err == nil { t.Fatal("want error for property from another union branch") }
	if err := json.Unmarshal([]byte(` + "`{\"kind\":\"bogus\"}`" + `), &a); err == nil { t.Fatal("want error for unknown discriminator") }
	if err := json.Unmarshal([]byte(` + "`{}`" + `), &a); err == nil { t.Fatal("want error for missing discriminator") }
	if string(JobActionKindManual) != "manual" || string(JobActionKindRun) != "run" { t.Fatal("wire values wrong") }
}
`
	runGenerated(t, vs, "Job", "smoke", testSrc)
}

func TestEmitUnionMarshalRoundTrip(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"action":{
				"oneOf":[
					{"type":"object","properties":{"kind":{"const":"MANUAL"},"XMLParser":{"type":"string"}},"required":["kind","XMLParser"],"additionalProperties":false},
					{"type":"object","properties":{"kind":{"const":"RUN"},"snake_case":{"type":"array","items":{"type":"integer"}}},"required":["kind","snake_case"],"additionalProperties":false},
					{"type":"object","properties":{"kind":{"const":"STOP"},"reason":{"type":"string"}},"required":["kind","reason"],"additionalProperties":false}
				]
			}
		},
		"additionalProperties":false
	}`
	testSrc := `
func TestUnionMarshalRoundTrip(t *testing.T) {
	for _, test := range []struct {
		name string
		input string
		want string
	}{
		{name:"manual", input:"{\"kind\":\"MANUAL\",\"XMLParser\":\"exact-wire\"}", want:"{\"XMLParser\":\"exact-wire\",\"kind\":\"MANUAL\"}"},
		{name:"run", input:"{\"kind\":\"RUN\",\"snake_case\":[1.0,1e0]}", want:"{\"kind\":\"RUN\",\"snake_case\":[1,1]}"},
		{name:"stop", input:"{\"kind\":\"STOP\",\"reason\":\"done\"}", want:"{\"kind\":\"STOP\",\"reason\":\"done\"}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var action DocAction
			if err := json.Unmarshal([]byte(test.input), &action); err != nil { t.Fatal(err) }
			encoded, err := json.Marshal(action)
			if err != nil { t.Fatal(err) }
			if string(encoded) != test.want { t.Fatalf("wire JSON = %s, want %s", encoded, test.want) }
			var roundTrip DocAction
			if err := json.Unmarshal(encoded, &roundTrip); err != nil { t.Fatal(err) }
			again, err := json.Marshal(roundTrip)
			if err != nil { t.Fatal(err) }
			if string(again) != test.want { t.Fatalf("round-trip JSON = %s, want %s", again, test.want) }
		})
	}
}

func TestUnionMarshalRejectsNoSelectedBranch(t *testing.T) {
	if _, err := json.Marshal(DocAction{}); err == nil { t.Fatal("zero union must not marshal as a fabricated variant") }
	var branch *DocActionValueMANUAL
	if _, err := json.Marshal(DocAction{Value: branch}); err == nil { t.Fatal("typed nil branch must not marshal") }
}
`
	runGenerated(t, vs, "Doc", "unionmarshal", testSrc)
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
	if d.Title == nil || *d.Title != "t" || d.WorkflowType == nil || *d.WorkflowType != DocWorkflowTypeValuePipeline { t.Fatalf("decode wrong: %+v", d) }
	if d.Profile == nil || d.Profile.Nickname != "n" { t.Fatalf("profile wrong: %+v", d.Profile) }
	if string(DocWorkflowTypeValueFanout) != "fanout" { t.Fatal("fanout wire wrong") }
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

// TestEmitConstraintsAndRecursion exercises const, maxLength, maxItems, number
// bounds, and indexed reference recursion through arrays (RFC 6901 paths).
func TestEmitConstraintsAndRecursion(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"title":{"type":"string","minLength":3,"maxLength":5},
			"count":{"type":"integer","minimum":10,"maximum":20},
			"ratio":{"type":"number","minimum":0.5,"maximum":1.5},
			"status":{"type":"string","const":"active"},
			"tags":{"type":"array","items":{"type":"string"},"maxItems":2},
			"nodes":{"type":"array","items":{"$ref":"#/$defs/node"},"minItems":1}
		},
		"required":["title","nodes"],
		"$defs":{"node":{"type":"object","properties":{"code":{"type":"string","minLength":2}}}}
	}`
	testSrc := `
func TestConstraintsAndRecursion(t *testing.T) {
	has := func(issues []Issue, code, path string) bool {
		for _, i := range issues { if i.Code == code && i.Path == path { return true } }
		return false
	}
	var d Doc
	mk := func(s string) { if err := json.Unmarshal([]byte(s), &d); err != nil { t.Fatal(err) } }
	// valid
	mk(` + "`{\"title\":\"abc\",\"count\":15,\"ratio\":1.2,\"status\":\"active\",\"tags\":[\"a\",\"b\"],\"nodes\":[{\"code\":\"xx\"}]}`" + `)
	if v := d.Validate(); len(v) != 0 { t.Fatalf("want clean, got %+v", v) }
	// maxLength
	mk(` + "`{\"title\":\"toolong\",\"nodes\":[{\"code\":\"xx\"}]}`" + `)
	if !has(d.Validate(), "maxLength", "/title") { t.Fatal("want maxLength @ /title") }
	// minimum/maximum (int) and maximum (number)
	mk(` + "`{\"title\":\"abc\",\"count\":5,\"nodes\":[{\"code\":\"xx\"}]}`" + `)
	if !has(d.Validate(), "minimum", "/count") { t.Fatal("want minimum @ /count") }
	mk(` + "`{\"title\":\"abc\",\"count\":25,\"nodes\":[{\"code\":\"xx\"}]}`" + `)
	if !has(d.Validate(), "maximum", "/count") { t.Fatal("want maximum @ /count (int)") }
	mk(` + "`{\"title\":\"abc\",\"ratio\":2.0,\"nodes\":[{\"code\":\"xx\"}]}`" + `)
	if !has(d.Validate(), "maximum", "/ratio") { t.Fatal("want maximum @ /ratio (number)") }
	// const
	mk(` + "`{\"title\":\"abc\",\"status\":\"inactive\",\"nodes\":[{\"code\":\"xx\"}]}`" + `)
	if !has(d.Validate(), "const", "/status") { t.Fatal("want const @ /status") }
	// maxItems
	mk(` + "`{\"title\":\"abc\",\"tags\":[\"a\",\"b\",\"c\"],\"nodes\":[{\"code\":\"xx\"}]}`" + `)
	if !has(d.Validate(), "maxItems", "/tags") { t.Fatal("want maxItems @ /tags") }
	// ref recursion into array element yields indexed path
	mk(` + "`{\"title\":\"abc\",\"nodes\":[{\"code\":\"x\"}]}`" + `)
	if !has(d.Validate(), "minLength", "/nodes/0/code") { t.Fatalf("want minLength @ /nodes/0/code, got %+v", d.Validate()) }
}
`
	runGenerated(t, vs, "Doc", "smoke", testSrc)
}

// TestEmitOptionalEmptyObjectAndNested checks optional-scalar nil on omission,
// empty-schema -> named struct, and unknown-property rejection on a nested object.
func TestEmitOptionalEmptyObjectAndNested(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"nickname":{"type":"string"},
			"meta":{"type":"object","properties":{"env":{"type":"string"}},"required":["env"]},
			"extra":{}
		},
		"required":[]
	}`
	spec := mustBuild(t, vs, "Doc")
	em, err := Emit(spec, EmitOptions{PkgName: "smoke", SchemaName: "Doc"})
	if err != nil {
		t.Fatalf("Emit(): %v", err)
	}
	if !strings.Contains(em.Source, "type DocExtra struct") {
		t.Fatalf("empty schema must emit a named Extra struct:\n%s", em.Source)
	}
	testSrc := `
func TestOptionalNilAndNested(t *testing.T) {
	var d Doc
	if err := json.Unmarshal([]byte(` + "`{}`" + `), &d); err != nil { t.Fatal(err) }
	if d.Nickname != nil { t.Fatalf("missing optional string should be nil, got %v", d.Nickname) }
	if err := json.Unmarshal([]byte(` + "`{\"meta\":{\"env\":\"prod\",\"bogus\":1}}`" + `), &d); err == nil { t.Fatal("want nested unknown-field error") }
}
`
	runGenerated(t, vs, "Doc", "smoke", testSrc)
}

// TestEmitEnumNonAlphaWire ensures enum wire values with non-identifier
// characters still produce compilable constants with the exact wire value.
func TestEmitEnumNonAlphaWire(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{"mode":{"type":"string","enum":["my-value","snake_case","UPPER"]}}
	}`
	testSrc := `
func TestEnumNonAlpha(t *testing.T) {
	if b, _ := json.Marshal(DocModeValueMyValue); string(b) != "\"my-value\"" { t.Fatalf("hyphen wire lost: %s", b) }
	if string(DocModeValueSnakeCase) != "snake_case" { t.Fatal("underscore wire lost") }
	if string(DocModeValueUPPER) != "UPPER" { t.Fatal("upper wire lost") }
}
`
	runGenerated(t, vs, "Doc", "smoke", testSrc)
}

// TestEmitEscapingNullEnumBranchRequired exercises the fixes from openai review:
// RFC 6901 escaping, null-on-optional rejection, enum membership validation,
// array-of-enum, and per-branch required enforcement.
func TestEmitEscapingNullEnumBranchRequired(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"a/b":{"type":"string","minLength":2},
			"x~y":{"type":"string","maxLength":1},
			"opt":{"type":"string"},
			"mode":{"type":"string","enum":["on","off"]},
			"tags":{"type":"array","items":{"type":"string","enum":["red","blue"]}},
			"state":{"oneOf":[
				{"type":"object","properties":{"kind":{"const":"manual"},"instructions":{"type":"string"}},"required":["kind","instructions"]},
				{"type":"object","properties":{"kind":{"const":"auto"}},"required":["kind"]}
			]}
		},
		"$defs":{}
	}`
	testSrc := `
func TestEscapingNullEnumBranch(t *testing.T) {
	has := func(issues []Issue, code, path string) bool {
		for _, i := range issues { if i.Code == code && i.Path == path { return true } }
		return false
	}
	var d Doc
	mk := func(s string) { d = Doc{}; if err := json.Unmarshal([]byte(s), &d); err != nil { t.Fatal("decode:", err) } }
	// RFC 6901 escaping
	mk(` + "`{\"a/b\":\"a\"}`" + `)
	if !has(d.Validate(), "minLength", "/a~1b") { t.Fatalf("want /a~1b, got %+v", d.Validate()) }
	mk(` + "`{\"x~y\":\"zz\"}`" + `)
	if !has(d.Validate(), "maxLength", "/x~0y") { t.Fatalf("want /x~0y, got %+v", d.Validate()) }
	// null on optional
	if err := json.Unmarshal([]byte(` + "`{\"opt\":null}`" + `), &d); err == nil { t.Fatal("want null-on-optional error") }
	// enum membership via Validate
	mk(` + "`{\"mode\":\"nope\"}`" + `)
	if !has(d.Validate(), "enum", "/mode") { t.Fatalf("want enum @ /mode, got %+v", d.Validate()) }
	// array-of-enum per-index
	mk(` + "`{\"tags\":[\"red\",\"purple\"]}`" + `)
	if !has(d.Validate(), "enum", "/tags/1") { t.Fatalf("want enum @ /tags/1, got %+v", d.Validate()) }
	// branch-required missing
	if err := json.Unmarshal([]byte(` + "`{\"state\":{\"kind\":\"manual\"}}`" + `), &d); err == nil { t.Fatal("want missing branch-required error") }
	// branch-required present decodes and validates clean
	mk(` + "`{\"state\":{\"kind\":\"manual\",\"instructions\":\"x\"}}`" + `)
	if v := d.Validate(); len(v) != 0 { t.Fatalf("want clean manual branch, got %+v", v) }
	// auto branch requires only kind
	mk(` + "`{\"state\":{\"kind\":\"auto\"}}`" + `)
	if v := d.Validate(); len(v) != 0 { t.Fatalf("want clean auto branch, got %+v", v) }
}
`
	runGenerated(t, vs, "Doc", "smoke", testSrc)
}

func TestEmitScalarArrayDefinitionsRecursiveConstraintsAndIntegralNumbers(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"code":{"$ref":"#/$defs/code"},
			"counts":{"$ref":"#/$defs/counts"},
			"matrix":{"type":"array","items":{"type":"array","minItems":1,"items":{"type":"string","minLength":2}}}
		},
		"additionalProperties":false,
		"$defs":{
			"code":{"type":"string","minLength":2},
			"counts":{"type":"array","items":{"type":"integer","minimum":1}}
		}
	}`
	testSrc := `
func TestDefinitionAndRecursiveItems(t *testing.T) {
	_ = json.Valid
	has := func(issues []DocStructuralIssue, code, path string) bool {
		for _, issue := range issues { if issue.Code == code && issue.Path == path { return true } }
		return false
	}
	issues, err := ValidateDocJSON([]byte("{\"code\":\"x\",\"counts\":[0],\"matrix\":[[],[\"x\"]]}"))
	if err != nil { t.Fatal(err) }
	if !has(issues, "minLength", "/code") || !has(issues, "minimum", "/counts/0") || !has(issues, "minItems", "/matrix/0") || !has(issues, "minLength", "/matrix/1/0") { t.Fatalf("recursive issues: %+v", issues) }

	input := []byte("{\"code\":\"ok\",\"counts\":[1.0,1e0],\"matrix\":[[\"ok\"]]}")
	before := append([]byte(nil), input...)
	decoded, err := DecodeDoc(input)
	if err != nil { t.Fatal(err) }
	if string(input) != string(before) { t.Fatal("DecodeDoc mutated input") }
	if decoded.Counts == nil || len(*decoded.Counts) != 2 || (*decoded.Counts)[0] != 1 || (*decoded.Counts)[1] != 1 { t.Fatalf("integral decode: %+v", decoded.Counts) }
	if _, ok := any(*decoded.Counts).(DocCounts); !ok { t.Fatal("array definition did not retain named Go type") }
}
`
	runGenerated(t, vs, "Doc", "smoke", testSrc)
}

func TestEmitBranchSpecificIncompatibleWireTypes(t *testing.T) {
	vs := `{"type":"object","properties":{"choice":{"oneOf":[
		{"type":"object","properties":{"kind":{"const":"text"},"payload":{"type":"string"}},"required":["kind","payload"],"additionalProperties":false},
		{"type":"object","properties":{"kind":{"const":"count"},"payload":{"type":"integer"}},"required":["kind","payload"],"additionalProperties":false}
	]}},"additionalProperties":false}`
	testSrc := `
func TestBranchStorage(t *testing.T) {
	var doc Doc
	if err := json.Unmarshal([]byte("{\"choice\":{\"kind\":\"text\",\"payload\":\"value\"}}"), &doc); err != nil { t.Fatal(err) }
	if doc.Choice == nil { t.Fatal("missing choice") }
	text, ok := doc.Choice.Value.(*DocChoiceValueText)
	if !ok || text.Payload != "value" { t.Fatalf("text branch: %T %+v", doc.Choice.Value, doc.Choice.Value) }
	if err := json.Unmarshal([]byte("{\"choice\":{\"kind\":\"count\",\"payload\":2.0}}"), &doc); err != nil { t.Fatal(err) }
	count, ok := doc.Choice.Value.(*DocChoiceValueCount)
	if !ok || count.Payload != 2 { t.Fatalf("count branch: %T %+v", doc.Choice.Value, doc.Choice.Value) }
}
`
	runGenerated(t, vs, "Doc", "smoke", testSrc)
}

func TestEmitStrictObjectNullAndReceiverReset(t *testing.T) {
	vs := `{"type":"object","properties":{"name":{"type":"string"}},"additionalProperties":false}`
	testSrc := `
func TestStrictRoot(t *testing.T) {
	value := "stale"
	doc := Doc{Name: &value}
	if err := json.Unmarshal([]byte("null"), &doc); err == nil { t.Fatal("explicit null must fail") }
	if doc.Name == nil || *doc.Name != "stale" { t.Fatal("failed decode mutated receiver") }
	if err := json.Unmarshal([]byte("{}"), &doc); err != nil { t.Fatal(err) }
	if doc.Name != nil { t.Fatalf("omitted field retained stale state: %+v", doc) }
}
`
	runGenerated(t, vs, "Doc", "smoke", testSrc)
}

func TestEmitRefOnlyDefinitionsPreserveShapesAndEmptyIntegralArrays(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"code":{"$ref":"#/$defs/codeAlias"},
			"counts":{"$ref":"#/$defs/countsAlias"},
			"matrix":{"$ref":"#/$defs/matrixAlias"},
			"meta":{"$ref":"#/$defs/metaAlias"},
			"mode":{"$ref":"#/$defs/modeAlias"},
			"choice":{"$ref":"#/$defs/choiceAlias"}
		},
		"additionalProperties":false,
		"$defs":{
			"code":{"type":"string","minLength":2},
			"codeAlias":{"$ref":"#/$defs/code"},
			"counts":{"type":"array","items":{"$ref":"#/$defs/positive"}},
			"countsAlias":{"$ref":"#/$defs/counts"},
			"matrix":{"type":"array","items":{"type":"array","items":{"$ref":"#/$defs/positive"}}},
			"matrixAlias":{"$ref":"#/$defs/matrix"},
			"meta":{"type":"object","properties":{"name":{"type":"string","minLength":2}},"required":["name"],"additionalProperties":false},
			"metaAlias":{"$ref":"#/$defs/meta"},
			"mode":{"type":"string","enum":["on","off"]},
			"modeAlias":{"$ref":"#/$defs/mode"},
			"choice":{"oneOf":[
				{"type":"object","properties":{"kind":{"const":"text"},"text":{"type":"string"}},"required":["kind","text"],"additionalProperties":false},
				{"type":"object","properties":{"kind":{"const":"count"},"count":{"type":"integer"}},"required":["kind","count"],"additionalProperties":false}
			]},
			"choiceAlias":{"$ref":"#/$defs/choice"},
			"positive":{"type":"integer","minimum":1}
		}
	}`
	testSrc := `
func TestAliases(t *testing.T) {
	input := []byte("{\"code\":\"ok\",\"counts\":[],\"matrix\":[[]],\"meta\":{\"name\":\"ok\"},\"mode\":\"on\",\"choice\":{\"kind\":\"text\",\"text\":\"x\"}}")
	decoded, err := DecodeDoc(input)
	if err != nil { t.Fatal(err) }
	if decoded.Counts == nil || *decoded.Counts == nil || len(*decoded.Counts) != 0 { t.Fatalf("empty integral array lost presence: %#v", decoded.Counts) }
	if decoded.Matrix == nil || *decoded.Matrix == nil || len(*decoded.Matrix) != 1 || (*decoded.Matrix)[0] == nil { t.Fatalf("nested empty integral array lost presence: %#v", decoded.Matrix) }
	if decoded.Meta == nil || decoded.Meta.Name != "ok" { t.Fatalf("object alias: %#v", decoded.Meta) }
	if decoded.Mode == nil || *decoded.Mode != DocModeOn { t.Fatalf("enum alias: %#v", decoded.Mode) }
	if decoded.Choice == nil { t.Fatal("union alias missing") }
	if _, ok := decoded.Choice.Value.(*DocChoiceValueText); !ok { t.Fatalf("union alias branch: %T", decoded.Choice.Value) }

	bad := DocPositive(0)
	counts := DocCountsAlias{bad}
	doc := Doc{Counts: &counts}
	before, _ := json.Marshal(doc)
	issues := doc.Validate()
	after, _ := json.Marshal(doc)
	if string(before) != string(after) { t.Fatal("typed Validate mutated receiver") }
	if len(issues) != 1 || issues[0] != (Issue{Code:"minimum", Path:"/counts/0"}) { t.Fatalf("array ref constraints: %+v", issues) }
}
`
	runGenerated(t, vs, "Doc", "smoke", testSrc)
}

func TestDecodeStructuralErrorExactIssuesAndInputImmutability(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"requiredField":{"type":"string"},
			"count":{"type":"integer"},
			"matrix":{"type":"array","items":{"type":"array","items":{"type":"integer"}}},
			"action":{"oneOf":[
				{"type":"object","properties":{"kind":{"const":"run"}},"required":["kind"],"additionalProperties":false},
				{"type":"object","properties":{"kind":{"const":"stop"}},"required":["kind"],"additionalProperties":false}
			]}
		},
		"required":["requiredField"],
		"additionalProperties":false
	}`
	testSrc := `
func TestStructuralError(t *testing.T) {
	_ = json.Valid
	input := []byte("{\"action\":{\"kind\":\"other\"},\"count\":\"bad\",\"matrix\":[[\"bad\"]]}")
	before := append([]byte(nil), input...)
	_, err := DecodeDoc(input)
	structural, ok := err.(*DocStructuralError)
	if !ok { t.Fatalf("DecodeDoc error = %T %v", err, err) }
	want := []DocStructuralIssue{
		{Source:"json-schema", Code:"discriminator", Path:"/action/kind", SchemaPath:"/properties/action/properties/kind", Message:"discriminator"},
		{Source:"json-schema", Code:"type", Path:"/count", SchemaPath:"/properties/count", Message:"type"},
		{Source:"json-schema", Code:"type", Path:"/matrix/0/0", SchemaPath:"/properties/matrix/items/items", Message:"type"},
		{Source:"json-schema", Code:"required", Path:"/requiredField", SchemaPath:"", Message:"required"},
	}
	if len(structural.Issues) != len(want) { t.Fatalf("issues = %+v, want %+v", structural.Issues, want) }
	for i := range want { if structural.Issues[i] != want[i] { t.Fatalf("issue[%d] = %+v, want %+v", i, structural.Issues[i], want[i]) } }
	if string(input) != string(before) { t.Fatal("DecodeDoc mutated raw input") }
}
`
	runGenerated(t, vs, "Doc", "smoke", testSrc)
}
