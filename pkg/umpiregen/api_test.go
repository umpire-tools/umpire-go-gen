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
