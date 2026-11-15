package umpiregen

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umpire-tools/umpire-go-gen/pkg/internal/testutil"
)

// Membership schemas go through the public parser, so every case pins Go that
// a JSON document can actually produce.
func TestGenerateMembershipCompiles(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{
			name: "string array field",
			schema: `{"version": 1,
				"fields": {"tags": {"isEmpty": "array"}, "gate": {}},
				"rules": [{"type": "enabledWhen", "field": "gate", "when": {"op": "not", "expr": {"op": "contains", "field": "tags", "value": "a"}}}]}`,
		},
		{
			name: "numeric and mixed array fields",
			schema: `{"version": 1,
				"fields": {"numbers": {"isEmpty": "array"}, "slots": {"isEmpty": "array"}, "gate": {}},
				"rules": [
					{"type": "enabledWhen", "field": "gate", "when": {"op": "contains", "field": "numbers", "value": 2.5}},
					{"type": "fairWhen", "field": "gate", "when": {"op": "or", "exprs": [
						{"op": "contains", "field": "slots", "value": true},
						{"op": "contains", "field": "slots", "value": null}]}}]}`,
		},
		{
			name: "untyped field under every op it supports",
			schema: `{"version": 1,
				"conditions": {"allowed": {"type": "string[]"}},
				"fields": {"raw": {}, "gate": {}},
				"rules": [{"type": "enabledWhen", "field": "gate", "when": {"op": "and", "exprs": [
					{"op": "contains", "field": "raw", "value": 1},
					{"op": "in", "field": "raw", "values": ["a", 2, false, null]},
					{"op": "notIn", "field": "raw", "values": ["b"]},
					{"op": "eq", "field": "raw", "value": 3},
					{"op": "neq", "field": "raw", "value": "c"},
					{"op": "gt", "field": "raw", "value": 4},
					{"op": "truthy", "field": "raw"},
					{"op": "falsy", "field": "raw"},
					{"op": "present", "field": "raw"},
					{"op": "absent", "field": "raw"},
					{"op": "fieldInCond", "field": "raw", "condition": "allowed"}]}}]}`,
		},
		{
			name: "membership values a scalar field cannot hold",
			schema: `{"version": 1,
				"fields": {"name": {"isEmpty": "string"}, "gate": {}},
				"rules": [{"type": "enabledWhen", "field": "gate", "when": {"op": "or", "exprs": [
					{"op": "in", "field": "name", "values": [1, true, null]},
					{"op": "notIn", "field": "name", "values": [2]}]}}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := Generate([]byte(tt.schema), Config{PkgName: "membership", SchemaName: "Membership"})
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			testutil.AssertGeneratedPackageCompiles(t, source)
		})
	}
}

// fieldInCond compares a field with a condition's elements, so the field's Go
// type has to match the condition's element type for contains to compile.
func TestGenerateFieldInCondElementTypesCompile(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{
			name: "number field against a string condition",
			schema: `{"version": 1,
				"conditions": {"names": {"type": "string[]"}},
				"fields": {"count": {"isEmpty": "number"}, "gate": {}},
				"rules": [{"type": "enabledWhen", "field": "gate", "when": {"op": "fieldInCond", "field": "count", "condition": "names"}}]}`,
		},
		{
			name: "boolean field against a number condition",
			schema: `{"version": 1,
				"conditions": {"scores": {"type": "number[]"}},
				"fields": {"flag": {"isEmpty": "boolean"}, "gate": {}},
				"rules": [{"type": "enabledWhen", "field": "gate", "when": {"op": "fieldInCond", "field": "flag", "condition": "scores"}}]}`,
		},
		{
			name: "untyped field against a number condition",
			schema: `{"version": 1,
				"conditions": {"scores": {"type": "number[]"}},
				"fields": {"raw": {}, "gate": {}},
				"rules": [{"type": "enabledWhen", "field": "gate", "when": {"op": "fieldInCond", "field": "raw", "condition": "scores"}}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := Generate([]byte(tt.schema), Config{PkgName: "membership", SchemaName: "Membership"})
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			testutil.AssertGeneratedPackageCompiles(t, source)
		})
	}
}

// eq and neq literals of different JSON types force a field to hold any JSON
// value, or the comparison against the inferred Go type does not compile.
func TestGenerateEqNeqMixedLiteralsCompile(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{
			name: "number then string",
			schema: `{"version": 1,
				"fields": {"raw": {}, "gate": {}},
				"rules": [{"type": "enabledWhen", "field": "gate", "when": {"op": "and", "exprs": [
					{"op": "eq", "field": "raw", "value": 3},
					{"op": "eq", "field": "raw", "value": "a"}]}}]}`,
		},
		{
			name: "number then boolean under neq",
			schema: `{"version": 1,
				"fields": {"raw": {}, "gate": {}},
				"rules": [{"type": "enabledWhen", "field": "gate", "when": {"op": "and", "exprs": [
					{"op": "eq", "field": "raw", "value": 3},
					{"op": "neq", "field": "raw", "value": true}]}}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := Generate([]byte(tt.schema), Config{PkgName: "membership", SchemaName: "Membership"})
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			testutil.AssertGeneratedPackageCompiles(t, source)
		})
	}
}

// A field used only inside an anyOf branch expression still has to be typed from
// that comparison, not from the *string default.
func TestGenerateBranchOnlyFieldUsageCompiles(t *testing.T) {
	schema := `{"version": 1,
		"fields": {"kind": {}, "inning": {}, "gate": {}},
		"rules": [{"type": "anyOf", "rules": [
			{"type": "enabledWhen", "field": "kind", "when": {"op": "eq", "field": "inning", "value": 9}},
			{"type": "enabledWhen", "field": "kind", "when": {"op": "present", "field": "gate"}}]}]}`
	source, err := Generate([]byte(schema), Config{PkgName: "membership", SchemaName: "Membership"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	testutil.AssertGeneratedPackageCompiles(t, source)
	if !strings.Contains(source, "Inning *float64") {
		t.Errorf("Inning is not typed from its branch-only comparison:\n%s", source)
	}
}

func TestGenerateRejectsValidationOnFieldHoldingAnyJSONValue(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		op     string
	}{
		{
			name: "check rule",
			op:   "check",
			schema: `{"version": 1,
				"fields": {"raw": {}, "gate": {}},
				"rules": [{"type": "enabledWhen", "field": "gate", "when": {"op": "and", "exprs": [
					{"op": "contains", "field": "raw", "value": "a"},
					{"op": "check", "field": "raw", "check": {"op": "email"}}]}}]}`,
		},
		{
			name: "named validator on a present field",
			op:   "email",
			schema: `{"version": 1,
				"fields": {"raw": {"isEmpty": "present"}, "gate": {}},
				"rules": [{"type": "enabledWhen", "field": "gate", "when": {"op": "contains", "field": "raw", "value": "a"}}],
				"validators": {"raw": {"op": "email"}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Generate([]byte(tt.schema), Config{PkgName: "membership", SchemaName: "Membership"})
			if err == nil || !strings.Contains(err.Error(), `field "raw"`) || !strings.Contains(err.Error(), `"`+tt.op+`"`) {
				t.Fatalf("Generate error = %v, want a rejection naming field \"raw\" and op %q", err, tt.op)
			}
		})
	}
}

const membershipValueSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"properties": {
		"tags": {"type": "array", "items": {"type": "string"}},
		"scores": {"type": "array", "items": {"type": "number"}},
		"counts": {"type": "array", "items": {"type": "integer"}},
		"colors": {"type": "array", "items": {"type": "string", "enum": ["red", "blue"]}},
		"legs": {"type": "array", "items": {"type": "object", "properties": {"id": {"type": "string"}}, "additionalProperties": false}},
		"title": {"type": "string"},
		"tagGate": {"type": "boolean"},
		"scoreGate": {"type": "boolean"},
		"countGate": {"type": "boolean"},
		"countFractionGate": {"type": "boolean"},
		"colorGate": {"type": "boolean"},
		"legGate": {"type": "boolean"},
		"titleGate": {"type": "boolean"},
		"scoreInGate": {"type": "boolean"},
		"scoreNotInGate": {"type": "boolean"}
	},
	"additionalProperties": false
}`

const membershipUmpire = `{
	"version": 1,
	"fields": {
		"tags": {"isEmpty": "array"}, "scores": {}, "counts": {}, "colors": {}, "legs": {}, "title": {},
		"tagGate": {}, "scoreGate": {}, "countGate": {}, "countFractionGate": {}, "colorGate": {},
		"legGate": {}, "titleGate": {}, "scoreInGate": {}, "scoreNotInGate": {}
	},
	"rules": [
		{"type": "enabledWhen", "field": "tagGate", "when": {"op": "contains", "field": "tags", "value": "a"}},
		{"type": "enabledWhen", "field": "scoreGate", "when": {"op": "contains", "field": "scores", "value": 2.5}},
		{"type": "enabledWhen", "field": "countGate", "when": {"op": "contains", "field": "counts", "value": 3}},
		{"type": "enabledWhen", "field": "countFractionGate", "when": {"op": "contains", "field": "counts", "value": 3.5}},
		{"type": "enabledWhen", "field": "colorGate", "when": {"op": "contains", "field": "colors", "value": "blue"}},
		{"type": "enabledWhen", "field": "legGate", "when": {"op": "contains", "field": "legs", "value": "x"}},
		{"type": "enabledWhen", "field": "titleGate", "when": {"op": "contains", "field": "title", "value": "x"}},
		{"type": "enabledWhen", "field": "scoreInGate", "when": {"op": "in", "field": "scores", "values": [2.5]}},
		{"type": "enabledWhen", "field": "scoreNotInGate", "when": {"op": "notIn", "field": "scores", "values": [2.5]}}
	]
}`

func TestGenerateComposedMembershipOnStructuralArrays(t *testing.T) {
	source, _, err := GenerateComposed([]byte(membershipUmpire), []byte(membershipValueSchema), Config{PkgName: "membership", SchemaName: "Membership"})
	if err != nil {
		t.Fatalf("GenerateComposed: %v", err)
	}

	cases := []profileConformanceFixtureCase{
		{
			ID:                   "every-element-matches",
			Values:               json.RawMessage(`{"tags":["a"],"scores":[2.5],"counts":[3],"colors":["blue"],"legs":[{"id":"x"}],"title":"x"}`),
			ExpectedStructure:    profileExpectedStructure{Valid: true},
			ExpectedAvailability: json.RawMessage(`{}`),
		},
		{
			ID:                   "no-element-matches",
			Values:               json.RawMessage(`{"tags":["b"],"scores":[1],"counts":[4],"colors":["red"],"legs":[],"title":"y"}`),
			ExpectedStructure:    profileExpectedStructure{Valid: true},
			ExpectedAvailability: json.RawMessage(`{}`),
		},
	}
	want := []map[string]bool{
		{"TagGate": true, "ScoreGate": true, "CountGate": true, "ColorGate": true, "CountFractionGate": false, "LegGate": false, "TitleGate": false, "ScoreInGate": false, "ScoreNotInGate": true},
		{"TagGate": false, "ScoreGate": false, "CountGate": false, "ColorGate": false, "CountFractionGate": false, "LegGate": false, "TitleGate": false, "ScoreInGate": false, "ScoreNotInGate": true},
	}

	results := profileRunGeneratedCases(t, source, "Membership", cases)
	for i, result := range results {
		var availability map[string]struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(result.Availability, &availability); err != nil {
			t.Fatalf("%s: decode availability: %v\n%s", cases[i].ID, err, result.Availability)
		}
		for gate, enabled := range want[i] {
			if availability[gate].Enabled != enabled {
				t.Errorf("%s: %s.enabled = %v, want %v", cases[i].ID, gate, availability[gate].Enabled, enabled)
			}
		}
	}
}

// A field with no isEmpty that contains targets holds any JSON value, so every
// op that reads it has to handle a value of an unexpected JSON type at runtime.
// Each schema below keeps a contains rule on "raw" because that op is what makes
// the field resolve to Go's any; the gates under test hang off the same field.
const membershipAnyTruthySchema = `{"version": 1,
	"fields": {"raw": {}, "truthyGate": {}, "falsyGate": {}, "containsGate": {}},
	"rules": [
		{"type": "enabledWhen", "field": "truthyGate", "when": {"op": "truthy", "field": "raw"}},
		{"type": "enabledWhen", "field": "falsyGate", "when": {"op": "falsy", "field": "raw"}},
		{"type": "enabledWhen", "field": "containsGate", "when": {"op": "contains", "field": "raw", "value": "a"}}]}`

const membershipAnyGtSchema = `{"version": 1,
	"fields": {"raw": {}, "gtGate": {}, "containsGate": {}},
	"rules": [
		{"type": "enabledWhen", "field": "gtGate", "when": {"op": "gt", "field": "raw", "value": 5}},
		{"type": "enabledWhen", "field": "containsGate", "when": {"op": "contains", "field": "raw", "value": "a"}}]}`

const membershipAnyFieldInCondSchema = `{"version": 1,
	"conditions": {"names": {"type": "string[]"}, "scores": {"type": "number[]"}},
	"fields": {"raw": {}, "namesGate": {}, "scoresGate": {}, "containsGate": {}},
	"rules": [
		{"type": "enabledWhen", "field": "namesGate", "when": {"op": "fieldInCond", "field": "raw", "condition": "names"}},
		{"type": "enabledWhen", "field": "scoresGate", "when": {"op": "fieldInCond", "field": "raw", "condition": "scores"}},
		{"type": "enabledWhen", "field": "containsGate", "when": {"op": "contains", "field": "raw", "value": "a"}}]}`

// TestGenerateAnyFieldTruthyAndFalsyGates pins truthy and falsy to JavaScript
// truthiness: 0, "" and null are falsy, and true and objects are truthy.
func TestGenerateAnyFieldTruthyAndFalsyGates(t *testing.T) {
	source, err := Generate([]byte(membershipAnyTruthySchema), Config{PkgName: "membership", SchemaName: "Membership"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	membershipAssertAnyFieldType(t, source, "Raw")

	cases := []profileConformanceFixtureCase{
		{ID: "zero", Values: json.RawMessage(`{"raw":0}`)},
		{ID: "empty-string", Values: json.RawMessage(`{"raw":""}`)},
		{ID: "true", Values: json.RawMessage(`{"raw":true}`)},
		{ID: "null", Values: json.RawMessage(`{"raw":null}`)},
		{ID: "absent", Values: json.RawMessage(`{}`)},
		{ID: "object", Values: json.RawMessage(`{"raw":{"a":1}}`)},
	}
	want := []map[string]bool{
		{"TruthyGate": false, "FalsyGate": true},
		{"TruthyGate": false, "FalsyGate": true},
		{"TruthyGate": true, "FalsyGate": false},
		{"TruthyGate": false, "FalsyGate": true},
		{"TruthyGate": false, "FalsyGate": true},
		{"TruthyGate": true, "FalsyGate": false},
	}

	membershipAssertGates(t, cases, membershipRunAvailability(t, source, "Membership", cases), want)
}

// TestGenerateAnyFieldGtGate covers gt against a value that is not a number, a
// number that ties the bound, and a number string no numeric op reads.
func TestGenerateAnyFieldGtGate(t *testing.T) {
	source, err := Generate([]byte(membershipAnyGtSchema), Config{PkgName: "membership", SchemaName: "Membership"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	membershipAssertAnyFieldType(t, source, "Raw")

	cases := []profileConformanceFixtureCase{
		{ID: "above-bound", Values: json.RawMessage(`{"raw":7}`)},
		{ID: "below-bound", Values: json.RawMessage(`{"raw":3}`)},
		{ID: "equal-bound", Values: json.RawMessage(`{"raw":5.0}`)},
		{ID: "numeric-string", Values: json.RawMessage(`{"raw":"7"}`)},
	}
	want := []map[string]bool{
		{"GtGate": true},
		{"GtGate": false},
		{"GtGate": false},
		{"GtGate": false},
	}

	membershipAssertGates(t, cases, membershipRunAvailability(t, source, "Membership", cases), want)
}

// TestGenerateAnyFieldInCondGate matches a value of any type against a
// string[] and a number[] condition, so only the values the elements can equal
// ever match.
func TestGenerateAnyFieldInCondGate(t *testing.T) {
	source, err := Generate([]byte(membershipAnyFieldInCondSchema), Config{PkgName: "membership", SchemaName: "Membership"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	membershipAssertAnyFieldType(t, source, "Raw")

	conditions := json.RawMessage(`{"names":["a","b"],"scores":[2.5,7]}`)
	cases := []profileConformanceFixtureCase{
		{ID: "string-in-strings", Values: json.RawMessage(`{"raw":"b"}`), Conditions: conditions},
		{ID: "string-outside-strings", Values: json.RawMessage(`{"raw":"z"}`), Conditions: conditions},
		{ID: "number-in-numbers", Values: json.RawMessage(`{"raw":2.5}`), Conditions: conditions},
		{ID: "number-outside-numbers", Values: json.RawMessage(`{"raw":9}`), Conditions: conditions},
	}
	want := []map[string]bool{
		{"NamesGate": true, "ScoresGate": false},
		{"NamesGate": false},
		{"NamesGate": false, "ScoresGate": true},
		{"ScoresGate": false},
	}

	membershipAssertGates(t, cases, membershipRunAvailability(t, source, "Membership", cases), want)
}

// membershipAssertAnyFieldType keeps the any-typed runtime tests honest: they
// only reach the paths they name while the field still resolves to Go's any.
func membershipAssertAnyFieldType(t *testing.T, source, fieldName string) {
	t.Helper()
	if !strings.Contains(source, fieldName+" any") {
		t.Fatalf("%s is not typed any, so this test never reaches the any-typed path:\n%s", fieldName, source)
	}
}

// membershipAssertGates compares each case's availability against the enabled
// flag of every gate it names.
func membershipAssertGates(t *testing.T, cases []profileConformanceFixtureCase, results []json.RawMessage, want []map[string]bool) {
	t.Helper()
	if len(results) != len(want) {
		t.Fatalf("runner returned %d results, want %d", len(results), len(want))
	}
	for i, result := range results {
		var availability map[string]struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(result, &availability); err != nil {
			t.Fatalf("%s: decode availability: %v\n%s", cases[i].ID, err, result)
		}
		for gate, enabled := range want[i] {
			if availability[gate].Enabled != enabled {
				t.Errorf("%s: %s.enabled = %v, want %v", cases[i].ID, gate, availability[gate].Enabled, enabled)
			}
		}
	}
}

// membershipRunAvailability compiles the availability-only output of Generate,
// which carries no value-schema types to validate or decode, and calls Check for
// every case. Values decode straight into the generated fields struct, so the
// any-typed field holds whatever encoding/json produces for the JSON value.
func membershipRunAvailability(t *testing.T, source, schemaName string, cases []profileConformanceFixtureCase) []json.RawMessage {
	t.Helper()
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "generated")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("create generated package: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "generated.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write generated source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "go.mod"), []byte("module generated\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write generated go.mod: %v", err)
	}

	var requests strings.Builder
	for _, tc := range cases {
		if len(tc.Values) == 0 {
			t.Fatalf("case %q has no values", tc.ID)
		}
		conditions := tc.Conditions
		if len(conditions) == 0 {
			conditions = json.RawMessage("null")
		}
		fmt.Fprintf(&requests, "{values: json.RawMessage(%q), conditions: json.RawMessage(%q)},\n", string(tc.Values), string(conditions))
	}

	mainSource := fmt.Sprintf(`package main

import (
	"encoding/json"
	"fmt"
	"os"

	generated "generated"
)

type request struct {
	values json.RawMessage
	conditions json.RawMessage
}

func main() {
	requests := []request{
%s	}
	results := make([]json.RawMessage, 0, len(requests))
	for _, request := range requests {
		var fields generated.%sFields
		if err := json.Unmarshal(request.values, &fields); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var conditions generated.%sConditions
		if err := json.Unmarshal(request.conditions, &conditions); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		availability, err := json.Marshal(generated.Check(fields, conditions, generated.%sFields{}))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		results = append(results, availability)
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`, requests.String(), schemaName, schemaName, schemaName)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write generated runner: %v", err)
	}
	goMod := "module membershipavailability\n\ngo 1.22\n\nrequire generated v0.0.0\nreplace generated => ./generated\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write runner go.mod: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(dir, ".gocache"),
		"GOMODCACHE="+filepath.Join(dir, ".gomodcache"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated membership runner failed: %v\n%s\n--- source ---\n%s", err, output, source)
	}
	var results []json.RawMessage
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatalf("decode generated runner output: %v\noutput: %s", err, output)
	}
	return results
}

func TestGenerateComposedRejectsInOnDeclaredArrayField(t *testing.T) {
	umpire := strings.Replace(membershipUmpire,
		`{"op": "contains", "field": "tags", "value": "a"}`,
		`{"op": "in", "field": "tags", "values": ["a"]}`, 1)
	_, _, err := GenerateComposed([]byte(umpire), []byte(membershipValueSchema), Config{PkgName: "membership", SchemaName: "Membership"})
	if err == nil || !strings.Contains(err.Error(), `"in" requires a scalar field; "tags"`) {
		t.Fatalf("GenerateComposed error = %v, want the in-on-array definition error naming tags", err)
	}
}
