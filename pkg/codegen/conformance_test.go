package codegen_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umpire-tools/umpire-go-gen/pkg/codegen"
	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
	"github.com/umpire-tools/umpire-go-gen/pkg/umpiregen"
)

// PositiveFixture is a full positive test fixture.
type PositiveFixture struct {
	FixtureVersion int             `json:"fixtureVersion"`
	ID             string          `json:"id"`
	Description    string          `json:"description"`
	Schema         json.RawMessage `json:"schema"`
	Cases          []FixtureCase   `json:"cases"`
}

// FixtureCase is a single test case within a positive fixture.
type FixtureCase struct {
	ID                   string              `json:"id"`
	Values               map[string]any      `json:"values"`
	Prev                 map[string]any      `json:"prev,omitempty"`
	Conditions           map[string]any      `json:"conditions"`
	ExpectedAvailability map[string]ExpAvail `json:"expectedAvailability"`
}

// ExpAvail is the expected availability for a field in a test case.
type ExpAvail struct {
	Enabled   bool     `json:"enabled"`
	Required  bool     `json:"required"`
	Satisfied bool     `json:"satisfied"`
	Fair      bool     `json:"fair"`
	Reason    *string  `json:"reason"`
	Reasons   []string `json:"reasons"`
	Valid     *bool    `json:"valid,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// GeneratedAvail mirrors JSON-marshalled generated FieldStatus values.
type GeneratedAvail struct {
	Enabled   bool     `json:"enabled"`
	Required  bool     `json:"required"`
	Satisfied bool     `json:"satisfied"`
	Fair      bool     `json:"fair"`
	Reason    *string  `json:"reason"`
	Reasons   []string `json:"reasons"`
	Valid     *bool    `json:"valid"`
	Error     string   `json:"error"`
}

// ConformanceIndex is the index.json structure.
type ConformanceIndex struct {
	FixtureVersion int          `json:"fixtureVersion"`
	Fixtures       []IndexEntry `json:"fixtures"`
	Failures       []IndexEntry `json:"failures"`
}

// IndexEntry is an entry in the conformance index.
type IndexEntry struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

// FailureFixture is a full failure test fixture.
type FailureFixture struct {
	FixtureVersion int        `json:"fixtureVersion"`
	ID             string     `json:"id"`
	Description    string     `json:"description"`
	Failures       []FailCase `json:"failures"`
}

// FailCase is a single failure test case.
type FailCase struct {
	ID            string          `json:"id"`
	Phase         string          `json:"phase"`
	Schema        json.RawMessage `json:"schema"`
	Values        map[string]any  `json:"values,omitempty"`
	Conditions    map[string]any  `json:"conditions,omitempty"`
	ErrorIncludes string          `json:"errorIncludes"`
}

// collectConditionRefs collects condition names referenced by an expression tree.
func collectConditionRefs(e *schema.Expr, refs map[string]bool) {
	if e == nil {
		return
	}
	if e.Condition != "" {
		refs[e.Condition] = true
	}
	for i := range e.Exprs {
		collectConditionRefs(&e.Exprs[i], refs)
	}
}

// runCheck writes generated code to a temporary module and executes Check.
func runCheck(t *testing.T, schemaName string, s *schema.Schema, source string, values, conditions, prev map[string]any) (map[string]GeneratedAvail, error) {
	t.Helper()

	tmpDir := t.TempDir()
	pkgDir := filepath.Join(tmpDir, "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return nil, fmt.Errorf("MkdirAll: %w", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "gen_gen.go"), []byte(source), 0o644); err != nil {
		return nil, fmt.Errorf("WriteFile: %w", err)
	}

	fieldsJSON := filterDeclaredValues(s.Fields, values)
	fieldsData, err := json.Marshal(fieldsJSON)
	if err != nil {
		return nil, fmt.Errorf("marshal fields: %w", err)
	}
	condsJSON := make(map[string]any)
	for _, c := range s.Conditions {
		if v, ok := conditions[c.Name]; ok {
			condsJSON[c.Name] = v
		}
	}
	condsData, err := json.Marshal(condsJSON)
	if err != nil {
		return nil, fmt.Errorf("marshal conditions: %w", err)
	}
	prevData, err := json.Marshal(filterDeclaredValues(s.Fields, prev))
	if err != nil {
		return nil, fmt.Errorf("marshal previous fields: %w", err)
	}

	referencedConditions := make(map[string]bool)
	for _, rule := range s.Rules {
		collectConditionRefs(rule.Expr, referencedConditions)
		collectConditionRefs(rule.DisabledWhen, referencedConditions)
		collectConditionRefs(rule.FairWhen, referencedConditions)
		collectConditionRefs(rule.Check, referencedConditions)
	}
	for name := range referencedConditions {
		if _, ok := conditions[name]; !ok {
			return nil, fmt.Errorf("Missing runtime condition %q", name)
		}
	}

	mainGo := fmt.Sprintf(`package main

import (
	"encoding/json"
	"os"
	"pkg"
)

func main() {
	var f pkg.%sFields
	var c pkg.%sConditions
	var p pkg.%sFields
	_ = json.Unmarshal([]byte(%s), &f)
	_ = json.Unmarshal([]byte(%s), &c)
	_ = json.Unmarshal([]byte(%s), &p)
	avail := pkg.Check(f, c, p)
	_ = json.NewEncoder(os.Stdout).Encode(avail)
}
`, schemaName, schemaName, schemaName, q(string(fieldsData)), q(string(condsData)), q(string(prevData)))
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainGo), 0o644); err != nil {
		return nil, fmt.Errorf("WriteFile main: %w", err)
	}

	goMod := "module main\n\ngo 1.22\n\nrequire pkg v0.0.0\nreplace pkg => ./pkg\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return nil, fmt.Errorf("WriteFile go.mod: %w", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "go.mod"), []byte("module pkg\ngo 1.22\n"), 0o644); err != nil {
		return nil, fmt.Errorf("WriteFile pkg go.mod: %w", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(),
		"GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(tmpDir, ".gocache"),
		"GOMODCACHE="+filepath.Join(tmpDir, ".gomodcache"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go run failed: %w\n%s", err, output)
	}
	output = []byte(strings.TrimSpace(string(output)))
	if len(output) == 0 {
		return nil, fmt.Errorf("empty output from go run")
	}

	var rawResult map[string]json.RawMessage
	if err := json.Unmarshal(output, &rawResult); err != nil {
		return nil, fmt.Errorf("parse JSON raw: %w\noutput: %s", err, output)
	}
	result := make(map[string]GeneratedAvail, len(rawResult))
	for name, raw := range rawResult {
		var availability GeneratedAvail
		if err := json.Unmarshal(raw, &availability); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		result[name] = availability
	}
	return result, nil
}

func filterDeclaredValues(fields []schema.FieldDef, values map[string]any) map[string]any {
	filtered := make(map[string]any)
	for _, field := range fields {
		if value, ok := values[field.Name]; ok {
			filtered[field.Name] = value
		}
	}
	return filtered
}

func TestConformance(t *testing.T) {
	indexPath := filepath.Join("spec", "conformance", "index.json")
	if _, err := os.Stat(indexPath); err != nil {
		indexPath = filepath.Join("..", "..", "spec", "conformance", "index.json")
	}
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}

	var index ConformanceIndex
	if err := json.Unmarshal(indexData, &index); err != nil {
		t.Fatalf("parse index.json: %v", err)
	}
	for _, entry := range index.Fixtures {
		t.Run(entry.ID, func(t *testing.T) {
			fixturePath := filepath.Join("spec", "conformance", entry.Path)
			if _, err := os.Stat(fixturePath); err != nil {
				fixturePath = filepath.Join("..", "..", "spec", "conformance", entry.Path)
			}
			runPositiveFixture(t, fixturePath, entry.ID)
		})
	}
	for _, entry := range index.Failures {
		t.Run("failures/"+entry.ID, func(t *testing.T) {
			fixturePath := filepath.Join("spec", "conformance", entry.Path)
			if _, err := os.Stat(fixturePath); err != nil {
				fixturePath = filepath.Join("..", "..", "spec", "conformance", entry.Path)
			}
			runFailureFixture(t, fixturePath, entry.ID)
		})
	}
}

func runPositiveFixture(t *testing.T, path, id string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture PositiveFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	schemaJSON, err := json.Marshal(fixture.Schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	goName := sanitizeName(id)
	source, err := umpiregen.Generate(schemaJSON, umpiregen.Config{PkgName: "pkg", SchemaName: goName})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	parsed, err := schema.Parse(schemaJSON)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	for _, tc := range fixture.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			avail, err := runCheck(t, goName, parsed, source, tc.Values, tc.Conditions, tc.Prev)
			if err != nil {
				t.Fatalf("runCheck: %v", err)
			}
			for fieldName, expected := range tc.ExpectedAvailability {
				goFieldName := codegen.GoFieldName(fieldName)
				got, ok := avail[goFieldName]
				if !ok {
					t.Errorf("field %q (Go: %q) not in generated availability", fieldName, goFieldName)
					continue
				}
				if got.Enabled != expected.Enabled {
					t.Errorf("%s.Enabled: want %v, got %v", fieldName, expected.Enabled, got.Enabled)
				}
				if got.Required != expected.Required {
					t.Errorf("%s.Required: want %v, got %v", fieldName, expected.Required, got.Required)
				}
				if got.Satisfied != expected.Satisfied {
					t.Errorf("%s.Satisfied: want %v, got %v", fieldName, expected.Satisfied, got.Satisfied)
				}
				if got.Fair != expected.Fair {
					t.Errorf("%s.Fair: want %v, got %v", fieldName, expected.Fair, got.Fair)
				}
				if expected.Reason == nil {
					if got.Reason != nil {
						t.Errorf("%s.Reason: want nil, got %v", fieldName, got.Reason)
					}
				} else if got.Reason == nil || *got.Reason != *expected.Reason {
					t.Errorf("%s.Reason: want %q, got %v", fieldName, *expected.Reason, got.Reason)
				}
				if !equalStrSlice(got.Reasons, expected.Reasons) {
					t.Errorf("%s.Reasons: want %v, got %v", fieldName, expected.Reasons, got.Reasons)
				}
				if expected.Valid == nil {
					if got.Valid != nil {
						t.Errorf("%s.Valid: want omitted, got %v", fieldName, *got.Valid)
					}
				} else if got.Valid == nil || *got.Valid != *expected.Valid {
					t.Errorf("%s.Valid: want %v, got %v", fieldName, *expected.Valid, got.Valid)
				}
				if got.Error != expected.Error {
					t.Errorf("%s.Error: want %q, got %q", fieldName, expected.Error, got.Error)
				}
			}
		})
	}
}

func runFailureFixture(t *testing.T, path, id string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture FailureFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	for _, fc := range fixture.Failures {
		t.Run(fc.ID, func(t *testing.T) {
			schemaJSON, _ := json.Marshal(fc.Schema)
			goName := sanitizeName(id)
			cfg := umpiregen.Config{PkgName: "pkg", SchemaName: goName}

			switch fc.Phase {
			case "validate":
				_, parseErr := schema.Parse(schemaJSON)
				if parseErr != nil {
					if !strings.Contains(parseErr.Error(), fc.ErrorIncludes) {
						t.Errorf("Parse error %q does not contain %q", parseErr.Error(), fc.ErrorIncludes)
					}
					return
				}
				_, generateErr := umpiregen.Generate(schemaJSON, cfg)
				if generateErr == nil {
					t.Errorf("expected rejection containing %q", fc.ErrorIncludes)
				} else if !strings.Contains(generateErr.Error(), fc.ErrorIncludes) {
					t.Errorf("Generate error %q does not contain %q", generateErr.Error(), fc.ErrorIncludes)
				}
			case "evaluate":
				parsed, parseErr := schema.Parse(schemaJSON)
				if parseErr != nil {
					t.Fatalf("Parse: %v", parseErr)
				}
				source, generateErr := umpiregen.Generate(schemaJSON, cfg)
				if generateErr != nil {
					if !strings.Contains(generateErr.Error(), fc.ErrorIncludes) {
						t.Errorf("Generate error %q does not contain %q", generateErr.Error(), fc.ErrorIncludes)
					}
					return
				}
				_, runErr := runCheck(t, goName, parsed, source, fc.Values, fc.Conditions, nil)
				if runErr == nil {
					t.Errorf("expected Check() to error with string containing %q", fc.ErrorIncludes)
				} else if !strings.Contains(runErr.Error(), fc.ErrorIncludes) {
					t.Errorf("Check error %q does not contain %q", runErr.Error(), fc.ErrorIncludes)
				}
			default:
				t.Fatalf("unknown failure phase %q", fc.Phase)
			}
		})
	}
}

// sanitizeName converts a fixture ID to a valid Go identifier.
func sanitizeName(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i == 0 {
			if r >= 'a' && r <= 'z' {
				b.WriteRune(r - 32)
			} else if r >= '0' && r <= '9' || r == '_' {
				b.WriteString("_")
				b.WriteRune(r)
			} else {
				b.WriteRune(r)
			}
		} else if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func equalStrSlice(a, b []string) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func q(s string) string { return fmt.Sprintf("%q", s) }
