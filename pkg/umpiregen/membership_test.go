package umpiregen

import (
	"encoding/json"
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

func TestGenerateComposedRejectsInOnDeclaredArrayField(t *testing.T) {
	umpire := strings.Replace(membershipUmpire,
		`{"op": "contains", "field": "tags", "value": "a"}`,
		`{"op": "in", "field": "tags", "values": ["a"]}`, 1)
	_, _, err := GenerateComposed([]byte(umpire), []byte(membershipValueSchema), Config{PkgName: "membership", SchemaName: "Membership"})
	if err == nil || !strings.Contains(err.Error(), `"in" requires a scalar field; "tags"`) {
		t.Fatalf("GenerateComposed error = %v, want the in-on-array definition error naming tags", err)
	}
}
