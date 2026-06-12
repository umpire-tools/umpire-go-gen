package umpiregen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
			{"type": "check", "field": "email", "check": {"op": "email"}, "reason": "Invalid email"}
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
	assertGeneratedPackageCompiles(t, "availability", source)
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
	assertGeneratedPackageCompiles(t, "availability", source)
}

func TestGenerateTruthyAndNumericChecksCompile(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{
			name: "truthy-inferred-string",
			json: `{"fields":[{"name":"name"}],"conditions":[],"rules":[{"type":"enabledWhen","field":"name","expr":{"op":"truthy","field":"name"}}]}`,
		},
		{
			name: "numeric-check",
			json: `{"fields":[{"name":"age","type":"number"}],"conditions":[],"rules":[{"type":"check","field":"age","check":{"op":"min","value":18},"reason":"too young"}]}`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			source, err := Generate([]byte(tt.json), Config{PkgName: "availability", SchemaName: "Smoke"})
			if err != nil {
				t.Fatalf("Generate() error: %v", err)
			}
			assertGeneratedPackageCompiles(t, "availability", source)
		})
	}
}

func assertGeneratedPackageCompiles(t *testing.T, pkgName, source string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module smoke\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "generated.go"), []byte(source), 0644); err != nil {
		t.Fatalf("write generated.go: %v", err)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cacheDir := filepath.Join(dir, ".gocache")
	modCacheDir := filepath.Join(dir, ".gomodcache")
	cmd.Env = append(os.Environ(),
		"GOCACHE="+cacheDir,
		"GOMODCACHE="+modCacheDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated package %s did not compile: %v\n%s\n--- source ---\n%s", pkgName, err, out, source)
	}
}
