package umpiregen

import (
	"strings"
	"testing"
)

// TestGenerateComposedStructural_RawAPI verifies Stage 7 raw validation and DecodeDoc.
// Its tagged union uses JSON property names that do not round-trip through Go names.
// Generated tags must retain the original schema property names.
func TestGenerateComposedStructural_RawAPI(t *testing.T) {
	umpireJSON := []byte(`{
		"version":1,
		"fields":{"action":{},"count":{"isEmpty":"number"}},
		"rules":[]
	}`)
	valueSchemaJSON := []byte(`{
		"$schema":"https://json-schema.org/draft/2020-12/schema",
		"type":"object",
		"properties":{
			"action":{"oneOf":[
				{"type":"object","properties":{"kind":{"const":"RUN"},"XMLParser":{"type":"string","minLength":3}},"required":["kind","XMLParser"],"additionalProperties":false},
				{"type":"object","properties":{"kind":{"const":"MANUAL"},"snake_case":{"type":"integer","minimum":1}},"required":["kind","snake_case"],"additionalProperties":false}
			]},
			"count":{"type":"integer"}
		},
		"required":["action"],
		"additionalProperties":false
	}`)

	source, issues, err := GenerateComposed(umpireJSON, valueSchemaJSON, Config{PkgName: "smoke", SchemaName: "Doc"})
	if err != nil {
		t.Fatalf("GenerateComposed(): %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("unexpected definition issues: %+v", issues)
	}
	for _, want := range []string{
		"type DocStructuralIssue struct",
		"type DocStructuralError struct",
		"func ValidateDocJSON(data []byte)",
		"func DecodeDoc(data []byte)",
		"XMLParser",
		"`json:\"XMLParser,omitempty\"`",
		"Count *int64",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("generated source missing %q", want)
		}
	}

	testSrc := `
func TestRawAPI(t *testing.T) {
	_ = json.Valid
	has := func(issues []DocStructuralIssue, code, path string) bool {
		for _, issue := range issues {
			if issue.Source == "json-schema" && issue.Code == code && issue.Path == path {
				return true
			}
		}
		return false
	}
	valid := []byte("{\"action\":{\"kind\":\"RUN\",\"XMLParser\":\"okay\"},\"count\":2}")
	out, err := DecodeDoc(valid)
	if err != nil { t.Fatal(err) }
	if out.Action == nil || out.Action.XMLParser == nil || *out.Action.XMLParser != "okay" { t.Fatalf("original union wire did not decode: %+v", out.Action) }
	if out.Count == nil || *out.Count != 2 { t.Fatalf("top-level integer pointer = %+v", out.Count) }

	issues, err := ValidateDocJSON([]byte("{\"action\":{\"kind\":\"RUN\",\"XMLParser\":\"x\",\"extra\":true},\"count\":9007199254740992}"))
	if err != nil { t.Fatal(err) }
	if !has(issues, "minLength", "/action/XMLParser") || !has(issues, "additionalProperties", "/action/extra") || !has(issues, "safeInteger", "/count") { t.Fatalf("unexpected raw issues: %+v", issues) }
	issues, err = ValidateDocJSON([]byte("{\"action\":{\"kind\":\"RUN\",\"XMLParser\":7,\"snake_case\":1},\"count\":\"bad\",\"unknown\":true}"))
	if err != nil { t.Fatal(err) }
	if !has(issues, "type", "/action/XMLParser") || !has(issues, "additionalProperties", "/action/snake_case") || !has(issues, "type", "/count") || !has(issues, "additionalProperties", "/unknown") { t.Fatalf("unexpected type/unknown issues: %+v", issues) }
	issues, err = ValidateDocJSON([]byte("{}"))
	if err != nil || !has(issues, "required", "/action") { t.Fatalf("missing root required issue: %+v, %v", issues, err) }
	issues, err = ValidateDocJSON([]byte("{\"action\":{\"kind\":\"bogus\"}}"))
	if err != nil || !has(issues, "discriminator", "/action/kind") { t.Fatalf("missing discriminator issue: %+v, %v", issues, err) }
	for i := 1; i < len(issues); i++ {
		if issues[i-1].Path > issues[i].Path || (issues[i-1].Path == issues[i].Path && issues[i-1].Code > issues[i].Code) { t.Fatalf("issues are not normalized: %+v", issues) }
	}
	_, err = DecodeDoc([]byte("{\"action\":{\"kind\":\"RUN\"}}"))
	if _, ok := err.(*DocStructuralError); !ok { t.Fatalf("DecodeDoc error = %T, want *DocStructuralError (%v)", err, err) }
	if _, err := ValidateDocJSON([]byte("{\"action\":{\"kind\":\"RUN\",\"XMLParser\":\"okay\"}} {}")); err == nil { t.Fatal("want malformed/trailing error") }
}
`
	runMerged(t, source, "smoke", testSrc)
}
