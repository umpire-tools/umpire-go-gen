package umpiregen

import (
	"os"
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
