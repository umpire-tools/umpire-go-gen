package umpiregen

import (
	"os"
	"strings"
	"testing"

	"github.com/umpire-tools/umpire-go-gen/pkg/internal/testutil"
)

func TestGenerateSampleSchema(t *testing.T) {
	schemaJSON, err := os.ReadFile("../../testdata/sample.umpire.json")
	if err != nil {
		schemaJSON, err = os.ReadFile("testdata/sample.umpire.json")
		if err != nil {
			// try from package dir itself for go run / go test from repo root
			schemaJSON, err = os.ReadFile("../testdata/sample.umpire.json")
			if err != nil {
				// final fallback: repo root relative
				schemaJSON, err = os.ReadFile("testdata/sample.umpire.json")
				if err != nil {
					t.Fatalf("read fixture: %v", err)
				}
			}
		}
	}

	source, err := Generate(schemaJSON, Config{
		PkgName:    "availability",
		SchemaName: "Sample",
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	wantTokens := []string{
		"package availability",
		"type SampleFields struct",
		"type SampleConditions struct",
		"type FieldStatus struct",
		"func Check(f SampleFields,",
	}
	for _, tok := range wantTokens {
		if !strings.Contains(source, tok) {
			t.Errorf("generated source missing %q", tok)
		}
	}
}

func TestGenerateAcceptsSpecShapeAndCompiles(t *testing.T) {
	schemaJSON := []byte(`{
		"version": 1,
		"fields": {
			"email": {"required": true, "isEmpty": "string"},
			"password": {"isEmpty": "string"},
			"submit": {}
		},
		"conditions": {
			"role": {"type": "string"}
		},
		"rules": [
			{"type": "enabledWhen", "field": "submit", "when": {"op": "cond", "condition": "role"}},
			{"type": "check", "field": "email", "op": "email", "reason": "Invalid email"}
		]
	}`)

	source, err := Generate(schemaJSON, Config{PkgName: "availability", SchemaName: "Login"})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	for _, want := range []string{"type LoginFields struct", "type LoginAvailability struct", "func Check(f LoginFields"} {
		if !strings.Contains(source, want) {
			t.Fatalf("generated source missing %q:\n%s", want, source)
		}
	}
	testutil.AssertGeneratedPackageCompiles(t, source)
}

func TestGenerateRejectsInvalidPublicSchemas(t *testing.T) {
	for _, test := range []struct {
		name, document, want string
	}{
		{"malformed JSON", `{`, "parse schema: invalid JSON"},
		{"wrong version", `{"version":2,"fields":{"email":{}},"rules":[]}`, "parse schema: version"},
		{"missing fields", `{"version":1,"rules":[]}`, "parse schema: fields"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, err := Generate([]byte(test.document), Config{PkgName: "availability", SchemaName: "Invalid"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Generate() error = %v, want substring %q", err, test.want)
			}
			if source != "" {
				t.Fatalf("Generate() source = %q, want empty output", source)
			}
		})
	}
}

func TestGenerateNoRulesCompiles(t *testing.T) {
	source, err := Generate([]byte(`{
		"version": 1,
		"fields": {"email": {"required": true}},
		"conditions": {},
		"rules": []
	}`), Config{PkgName: "availability", SchemaName: "Minimal"})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if !strings.Contains(source, "func Check(f MinimalFields") {
		t.Fatalf("generated source missing Check:\n%s", source)
	}
	testutil.AssertGeneratedPackageCompiles(t, source)
}

func TestGenerateProfile_ValidInline(t *testing.T) {
	profileJSON := []byte(`{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"email": { "type": "string" },
				"password": { "type": "string" }
			},
			"required": ["email"],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"email": { "required": true, "isEmpty": "string" },
				"password": { "isEmpty": "string" }
			},
			"rules": []
		}
	}`)

	source, issues, err := GenerateProfile(profileJSON, Config{PkgName: "profile", SchemaName: "ProfileTest"})
	if err != nil {
		t.Fatalf("GenerateProfile() error: %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("expected no definition issues, got: %v", issues)
	}
	if !strings.Contains(source, "type ProfileTestFields struct") {
		t.Fatalf("generated source missing struct:\n%s", source)
	}
	testutil.AssertGeneratedPackageCompiles(t, source)
}

func TestGenerateProfile_ReturnsIssues(t *testing.T) {
	// Profile with an excluded keyword should return issues but still generate code.
	profileJSON := []byte(`{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"x": { "allOf": [{ "type": "string" }] }
			},
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": { "x": { "isEmpty": "string" } },
			"rules": []
		}
	}`)

	source, issues, err := GenerateProfile(profileJSON, Config{PkgName: "issues", SchemaName: "IssuesTest"})
	if err != nil {
		t.Fatalf("GenerateProfile() error: %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("expected definition issues, got none")
	}
	if source == "" {
		t.Fatal("expected source even with issues")
	}
}

func TestGenerateProfile_FieldMismatchRejectsDuplicateMergedDeclaration(t *testing.T) {
	profileJSON := []byte(`{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"fields": {
					"type": "object",
					"properties": {},
					"additionalProperties": false
				}
			},
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": { "other": { "isEmpty": "string" } },
			"rules": []
		}
	}`)

	// With no schema name, availability's default Fields type clashes with the
	// structural type inferred for the valueSchema-only "fields" property.
	// This declared field mismatch must not return non-compilable merged source.
	source, issues, err := GenerateProfile(profileJSON, Config{PkgName: "profile"})
	if source != "" {
		t.Fatalf("GenerateProfile() returned source despite merge failure:\n%s", source)
	}
	if err == nil || !strings.Contains(err.Error(), `duplicate declaration "Fields"`) {
		t.Fatalf("GenerateProfile() error = %v, want duplicate Fields declaration", err)
	}
	assertHasIssue(t, issues, "fieldMismatch", "/valueSchema")
}

func TestGenerateProfile_InvalidJSON(t *testing.T) {
	_, _, err := GenerateProfile([]byte(`{invalid}`), Config{PkgName: "x", SchemaName: "X"})
	if err == nil || !strings.Contains(err.Error(), "parse profile") {
		t.Fatalf("expected parse profile error, got: %v", err)
	}
}

func TestGenerateComposed_Valid(t *testing.T) {
	umpireJSON := []byte(`{
		"version": 1,
		"fields": { "title": { "isEmpty": "string", "required": true } },
		"rules": []
	}`)
	valueSchemaJSON := []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": { "title": { "type": "string" } },
		"additionalProperties": false
	}`)

	source, issues, err := GenerateComposed(umpireJSON, valueSchemaJSON, Config{PkgName: "composed", SchemaName: "Composed"})
	if err != nil {
		t.Fatalf("GenerateComposed() error: %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("expected no issues, got: %v", issues)
	}
	if !strings.Contains(source, "type ComposedFields struct") {
		t.Fatalf("generated source missing struct:\n%s", source)
	}
	testutil.AssertGeneratedPackageCompiles(t, source)
}

func TestGenerateComposed_MissingValueSchema(t *testing.T) {
	_, _, err := GenerateComposed([]byte(`{"version":1,"fields":{},"rules":[]}`), nil, Config{PkgName: "x", SchemaName: "X"})
	if err == nil || !strings.Contains(err.Error(), "value-schema is required") {
		t.Fatalf("expected value-schema error, got: %v", err)
	}
}

func TestGenerateComposed_ReturnsIssues(t *testing.T) {
	// Composed mode with an excluded keyword should propagate definition issues.
	umpireJSON := []byte(`{
		"version": 1,
		"fields": { "x": { "isEmpty": "string" } },
		"rules": []
	}`)
	valueSchemaJSON := []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": {
			"x": { "not": { "type": "string" } }
		},
		"additionalProperties": false
	}`)

	_, issues, err := GenerateComposed(umpireJSON, valueSchemaJSON, Config{PkgName: "issues", SchemaName: "IssuesComposed"})
	if err != nil {
		t.Fatalf("GenerateComposed() error: %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("expected definition issues from composed profile, got none")
	}
	foundUnsupportedKeyword := false
	for _, iss := range issues {
		if iss.Code == "unsupportedKeyword" {
			foundUnsupportedKeyword = true
			break
		}
	}
	if !foundUnsupportedKeyword {
		t.Fatalf("expected unsupportedKeyword issue in issues: %+v", issues)
	}
}

func TestGenerate_ExistingBehaviorUnchanged(t *testing.T) {
	// Existing Generate calls must still work identically.
	source, err := Generate([]byte(`{
		"version": 1,
		"fields": { "email": { "required": true } },
		"conditions": {},
		"rules": []
	}`), Config{PkgName: "existing", SchemaName: "Existing"})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if !strings.Contains(source, "type ExistingFields struct") {
		t.Fatalf("generated source missing struct:\n%s", source)
	}
	testutil.AssertGeneratedPackageCompiles(t, source)
}

func TestGenerateTruthyAndNumericChecksCompile(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{
			name: "truthy-inferred-string",
			json: `{"version":1,"fields":{"name":{}},"conditions":{},"rules":[{"type":"enabledWhen","field":"name","when":{"op":"truthy","field":"name"}}]}`,
		},
		{
			name: "numeric-check",
			json: `{"version":1,"fields":{"age":{"isEmpty":"number"}},"conditions":{},"rules":[{"type":"check","field":"age","op":"min","value":18,"reason":"too young"}]}`,
		},
		{
			name: "numeric-named-validator",
			json: `{"version":1,"fields":{"age":{}},"conditions":{},"rules":[],"validators":{"age":{"op":"min","value":18,"error":"too young"}}}`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			source, err := Generate([]byte(tt.json), Config{PkgName: "availability", SchemaName: "Smoke"})
			if err != nil {
				t.Fatalf("Generate() error: %v", err)
			}
			testutil.AssertGeneratedPackageCompiles(t, source)
		})
	}
}
