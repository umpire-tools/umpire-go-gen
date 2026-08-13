package umpiregen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseProfile_ValidInline(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"title": { "type": "string" },
				"count": { "type": "integer" }
			},
			"required": ["title"],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"title": { "isEmpty": "string", "required": true },
				"count": { "isEmpty": "number" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	if result.Profile == nil {
		t.Fatal("expected non-nil profile")
	}
	if len(result.Issues) > 0 {
		t.Fatalf("expected no issues, got: %v", result.Issues)
	}

	// Verify umpireJSON and valueSchemaJSON are populated.
	if len(result.Profile.UmpireJSON) == 0 {
		t.Error("expected non-empty umpire JSON")
	}
	if len(result.Profile.ValueSchemaJSON) == 0 {
		t.Error("expected non-empty valueSchema JSON")
	}
}

func TestParseProfile_MissingKeys(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "missing $schema",
			data: `{"profileVersion":1,"valueSchema":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"},"umpire":{"version":1,"fields":{},"rules":[]}}`,
			want: `missing required key "$schema"`,
		},
		{
			name: "missing profileVersion",
			data: `{"$schema":"https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json","valueSchema":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"},"umpire":{"version":1,"fields":{},"rules":[]}}`,
			want: `missing required key "profileVersion"`,
		},
		{
			name: "missing valueSchema",
			data: `{"$schema":"https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json","profileVersion":1,"umpire":{"version":1,"fields":{},"rules":[]}}`,
			want: `missing required key "valueSchema"`,
		},
		{
			name: "missing umpire",
			data: `{"$schema":"https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json","profileVersion":1,"valueSchema":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}}`,
			want: `missing required key "umpire"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseProfile([]byte(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseProfile() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestParseProfile_WrongSchemaURI(t *testing.T) {
	data := `{
		"$schema": "https://wrong.uri/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object"
		},
		"umpire": {
			"version": 1,
			"fields": {},
			"rules": []
		}
	}`

	_, err := ParseProfile([]byte(data))
	if err == nil || !strings.Contains(err.Error(), "$schema") {
		t.Fatalf("expected error about $schema, got: %v", err)
	}
}

func TestParseProfile_WrongProfileVersion(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 2,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object"
		},
		"umpire": {
			"version": 1,
			"fields": {},
			"rules": []
		}
	}`

	_, err := ParseProfile([]byte(data))
	if err == nil || !strings.Contains(err.Error(), "profileVersion must be 1") {
		t.Fatalf("expected error about profileVersion, got: %v", err)
	}
}

func TestParseProfile_ValueSchemaWrongType(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "array",
			"items": { "type": "string" }
		},
		"umpire": {
			"version": 1,
			"fields": {},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidProfile", "/valueSchema")
}

func TestParseProfile_ExcludedKeywordAllOf(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"x": {
					"allOf": [{ "type": "string" }, { "minLength": 1 }]
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"x": { "isEmpty": "string" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "unsupportedKeyword", "/valueSchema/properties/x/allOf")
}

func TestParseProfile_ExcludedKeywordNot(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"x": {
					"type": "string",
					"not": { "pattern": "bad" }
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"x": { "isEmpty": "string" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "unsupportedKeyword", "/valueSchema/properties/x/not")
}

func TestParseProfile_ExcludedKeywordPattern(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"email": {
					"type": "string",
					"pattern": "^.+@.+\\..+$"
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"email": { "isEmpty": "string" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "unsupportedKeyword", "/valueSchema/properties/email/pattern")
}

func TestParseProfile_ExcludedKeywordFormat(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"email": {
					"type": "string",
					"format": "email"
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"email": { "isEmpty": "string" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "unsupportedKeyword", "/valueSchema/properties/email/format")
}

func TestParseProfile_FieldMismatchExtra(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"title": { "type": "string" }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"title": { "isEmpty": "string" },
				"extraField": { "isEmpty": "string" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "fieldMismatch", "/valueSchema")
}

func TestParseProfile_FieldMismatchValueSchemaExtra(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"title": { "type": "string" },
				"schemaOnly": { "type": "boolean" }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"title": { "isEmpty": "string" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "fieldMismatch", "/valueSchema")
}

func TestParseProfile_IncompatibleIsEmpty(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"count": { "type": "integer" }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"count": { "isEmpty": "string" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "incompatibleIsEmpty", "/umpire/fields/count")
}

func TestParseProfile_IncompatibleIsEmptyResolvedLocalReference(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"value": { "$ref": "#/$defs/text" }
			},
			"required": [],
			"additionalProperties": false,
			"$defs": {
				"text": { "type": "string" }
			}
		},
		"umpire": {
			"version": 1,
			"fields": {
				"value": { "isEmpty": "number" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "incompatibleIsEmpty", "/umpire/fields/value")
}

func TestParseProfile_InvalidReferenceBadFormat(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"item": { "$ref": "#/definitions/Item" }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"item": { "isEmpty": "present" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidReference", "/valueSchema/properties/item/$ref")
}

func TestParseProfile_MissingLocalReferenceTarget(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"item": { "$ref": "#/$defs/Missing" }
			},
			"required": [],
			"additionalProperties": false,
			"$defs": {
				"Present": { "type": "string" }
			}
		},
		"umpire": {
			"version": 1,
			"fields": {
				"item": { "isEmpty": "present" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidReference", "/valueSchema/properties/item/$ref")
}

func TestParseProfile_RemoteReference(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"item": { "$ref": "https://example.com/schema#/$defs/Item" }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"item": { "isEmpty": "present" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidReference", "/valueSchema/properties/item/$ref")
}

func TestParseProfile_ExcludedKeywordAnyOf(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"x": {
					"anyOf": [
						{ "type": "string" },
						{ "type": "number" }
					]
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"x": { "isEmpty": "string" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "unsupportedKeyword", "/valueSchema/properties/x/anyOf")
}

func TestParseProfile_InvalidDefault(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"count": { "type": "integer" }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"count": { "isEmpty": "number", "default": "not-a-number" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidDefault", "/umpire/fields/count/default")
}

func TestParseProfile_DefaultWithMinMax(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"count": { "type": "integer", "minimum": 10, "maximum": 100 }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"count": { "isEmpty": "number", "default": 5 }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidDefault", "/umpire/fields/count/default")
}

func TestParseProfile_DefaultConstraintsThroughLocalReference(t *testing.T) {
	tests := []struct {
		name        string
		schema      string
		defaultJSON string
		valid       bool
	}{
		{name: "type", schema: `{"type":"string"}`, defaultJSON: `7`},
		{name: "enum", schema: `{"type":"string","enum":["draft","ready"]}`, defaultJSON: `"other"`},
		{name: "const", schema: `{"type":"boolean","const":true}`, defaultJSON: `false`},
		{name: "valid number", schema: `{"type":"number"}`, defaultJSON: `1.5`, valid: true},
		{name: "valid boolean", schema: `{"type":"boolean"}`, defaultJSON: `true`, valid: true},
		{name: "minimum", schema: `{"type":"number","minimum":2}`, defaultJSON: `1.5`},
		{name: "maximum", schema: `{"type":"number","maximum":2}`, defaultJSON: `2.5`},
		{name: "exclusiveMinimum", schema: `{"type":"number","exclusiveMinimum":2}`, defaultJSON: `2`},
		{name: "exclusiveMaximum", schema: `{"type":"number","exclusiveMaximum":2}`, defaultJSON: `2`},
		{name: "minLength", schema: `{"type":"string","minLength":2}`, defaultJSON: `"x"`},
		{name: "maxLength", schema: `{"type":"string","maxLength":2}`, defaultJSON: `"xxx"`},
		{name: "safeInteger lower boundary", schema: `{"type":"integer"}`, defaultJSON: `-9007199254740991`, valid: true},
		{name: "safeInteger upper boundary", schema: `{"type":"integer"}`, defaultJSON: `9007199254740991`, valid: true},
		{name: "safeInteger", schema: `{"type":"integer"}`, defaultJSON: `9007199254740992`},
		{name: "object base contract", schema: `{"type":"object","properties":{},"additionalProperties":false}`, defaultJSON: `{}`},
		{name: "array base contract", schema: `{"type":"array","items":{"type":"string"}}`, defaultJSON: `[]`},
		{name: "irrelevant keyword tolerated", schema: `{"type":"string","contains":{"type":"string"},"unevaluatedProperties":false}`, defaultJSON: `"ok"`, valid: true},
		{
			name:        "valid complete constraints",
			schema:      `{"type":"string","enum":["ok","yes"],"const":"ok","minLength":2,"maxLength":2}`,
			defaultJSON: `"ok"`,
			valid:       true,
		},
		{
			name:        "valid exclusive bounds",
			schema:      `{"type":"integer","minimum":1,"maximum":9,"exclusiveMinimum":1,"exclusiveMaximum":9}`,
			defaultJSON: `5`,
			valid:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := parseProfileDefaultFixture(t, test.schema, test.defaultJSON)
			if test.valid {
				if len(result.Issues) != 0 {
					t.Fatalf("valid default returned issues: %+v", result.Issues)
				}
				return
			}
			if len(result.Issues) != 1 {
				t.Fatalf("invalid default issues = %+v, want exactly one", result.Issues)
			}
			assertHasIssue(t, result.Issues, "invalidDefault", "/umpire/fields/value/default")
		})
	}
}

func TestParseProfile_DefinitionIssuesAreDedupedAndSorted(t *testing.T) {
	data := `{
		"$schema":"https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion":1,
		"valueSchema":{
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"type":"object",
			"properties":{
				"a":{"type":"integer","pattern":"ignored"},
				"schemaOnly":{"type":"string"}
			},
			"required":[],
			"additionalProperties":false
		},
		"umpire":{
			"version":1,
			"fields":{
				"a":{"isEmpty":"number","default":"wrong"},
				"umpireOnly":{"isEmpty":"string"}
			},
			"rules":[]
		}
	}`
	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	want := []DefinitionIssue{
		{Code: "invalidDefault", Path: "/umpire/fields/a/default"},
		{Code: "fieldMismatch", Path: "/valueSchema"},
		{Code: "unsupportedKeyword", Path: "/valueSchema/properties/a/pattern"},
	}
	if !reflect.DeepEqual(result.Issues, want) {
		t.Fatalf("definition issues = %+v, want %+v", result.Issues, want)
	}
}

func TestParseProfile_DefaultOneOfPrimitiveInvalid(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"value": {
					"oneOf": [
						{
							"type": "object",
							"properties": { "kind": { "const": "a" } },
							"required": ["kind"],
							"additionalProperties": false
						},
						{
							"type": "object",
							"properties": { "kind": { "const": "b" } },
							"required": ["kind"],
							"additionalProperties": false
						}
					]
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"value": { "isEmpty": "present", "default": true }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidDefault", "/umpire/fields/value/default")
}

func parseProfileDefaultFixture(t *testing.T, referencedSchema, defaultJSON string) *ProfileResult {
	t.Helper()
	data := fmt.Sprintf(`{
		"$schema":"https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion":1,
		"valueSchema":{
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"type":"object",
			"properties":{"value":{"$ref":"#/$defs/rule"}},
			"required":[],
			"additionalProperties":false,
			"$defs":{"rule":%s}
		},
		"umpire":{"version":1,"fields":{"value":{"isEmpty":"present","default":%s}},"rules":[]}
	}`, referencedSchema, defaultJSON)
	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	return result
}

func TestParseProfile_ReferenceCycle(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"a": { "$ref": "#/$defs/A" }
			},
			"required": [],
			"additionalProperties": false,
			"$defs": {
				"A": {
					"type": "object",
					"properties": {
						"child": { "$ref": "#/$defs/B" }
					},
					"required": [],
					"additionalProperties": false
				},
				"B": {
					"type": "object",
					"properties": {
						"parent": { "$ref": "#/$defs/A" }
					},
					"required": [],
					"additionalProperties": false
				}
			}
		},
		"umpire": {
			"version": 1,
			"fields": {
				"a": { "isEmpty": "present" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "referenceCycle", "/valueSchema/$defs/B")
}

func TestParseProfile_InvalidDiscriminatorNotString(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"flag": {
					"oneOf": [
						{ "type": "object", "properties": { "kind": { "const": true } }, "required": ["kind"], "additionalProperties": false },
						{ "type": "object", "properties": { "kind": { "const": false } }, "required": ["kind"], "additionalProperties": false }
					]
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"flag": { "isEmpty": "present" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidDiscriminator", "/valueSchema/properties/flag/oneOf")
}

func TestParseProfile_DisparateDiscriminators(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"cmd": {
					"oneOf": [
						{
							"type": "object",
							"properties": { "kind": { "const": "run" }, "cmd": { "type": "string" } },
							"required": ["kind", "cmd"],
							"additionalProperties": false
						},
						{
							"type": "object",
							"properties": { "type": { "const": "manual" }, "desc": { "type": "string" } },
							"required": ["type", "desc"],
							"additionalProperties": false
						}
					]
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"cmd": { "isEmpty": "present" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidDiscriminator", "/valueSchema/properties/cmd/oneOf")
}

func TestParseComposed_Valid(t *testing.T) {
	umpireJSON := []byte(`{
		"version": 1,
		"fields": {
			"title": { "isEmpty": "string", "required": true }
		},
		"rules": []
	}`)

	valueSchemaJSON := []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": {
			"title": { "type": "string" }
		},
		"required": ["title"],
		"additionalProperties": false
	}`)

	result, err := ParseComposed(umpireJSON, valueSchemaJSON)
	if err != nil {
		t.Fatalf("ParseComposed() error: %v", err)
	}
	if result.Profile == nil {
		t.Fatal("expected non-nil profile")
	}
	if len(result.Issues) > 0 {
		t.Fatalf("expected no issues, got: %v", result.Issues)
	}
}

func TestParseComposed_MissingArgs(t *testing.T) {
	_, err := ParseComposed(nil, []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`))
	if err == nil || !strings.Contains(err.Error(), "umpire document is required") {
		t.Fatalf("expected error about missing umpire, got: %v", err)
	}

	_, err = ParseComposed([]byte(`{"version":1,"fields":{},"rules":[]}`), nil)
	if err == nil || !strings.Contains(err.Error(), "value-schema is required") {
		t.Fatalf("expected error about missing value-schema, got: %v", err)
	}
}

// TestParseProfileFromConformanceFixture loads the avenor-workflow fixture and verifies
// the profile parses without definition issues.
func TestParseProfileFromConformanceFixture(t *testing.T) {
	// Try multiple locations for spec.
	paths := []string{
		"../../spec/profiles/conformance/fixtures/avenor-workflow.json",
		"../spec/profiles/conformance/fixtures/avenor-workflow.json",
	}

	var fixtureData []byte
	var err error
	for _, p := range paths {
		fixtureData, err = os.ReadFile(filepath.FromSlash(p))
		if err == nil {
			break
		}
	}
	if fixtureData == nil {
		t.Skip("avenor-workflow fixture not found (spec may not be synced)")
	}

	var fixture struct {
		Profile json.RawMessage `json:"profile"`
	}
	if err := json.Unmarshal(fixtureData, &fixture); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	result, err := ParseProfile(fixture.Profile)
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}

	// Should parse without definition issues.
	if err := result.IssuesError(); err != nil {
		t.Fatalf("expected clean profile, got issues: %v", err)
	}
}

// TestParseProfileFailureFixtures loads the profile-definition-failures fixture and
// verifies each rejection case is correctly detected.
func TestParseProfileFailureFixtures(t *testing.T) {
	paths := []string{
		"../../spec/profiles/conformance/failures/profile-definition-failures.json",
		"../spec/profiles/conformance/failures/profile-definition-failures.json",
	}

	var fixtureData []byte
	var err error
	for _, p := range paths {
		fixtureData, err = os.ReadFile(filepath.FromSlash(p))
		if err == nil {
			break
		}
	}
	if fixtureData == nil {
		t.Skip("profile-definition-failures fixture not found (spec may not be synced)")
	}

	var failures struct {
		Failures []struct {
			ID                       string            `json:"id"`
			Profile                  json.RawMessage   `json:"profile"`
			ExpectedDefinitionIssues []json.RawMessage `json:"expectedDefinitionIssues"`
		} `json:"failures"`
	}
	if err := json.Unmarshal(fixtureData, &failures); err != nil {
		t.Fatalf("unmarshal failures: %v", err)
	}

	for _, f := range failures.Failures {
		t.Run(f.ID, func(t *testing.T) {
			result, err := ParseProfile(f.Profile)
			if err != nil {
				t.Fatalf("ParseProfile() error: %v, want definition issues not parse error", err)
			}
			if len(result.Issues) == 0 {
				t.Fatal("expected definition issues, got none")
			}
			for _, expected := range f.ExpectedDefinitionIssues {
				var exp struct {
					Code string `json:"code"`
					Path string `json:"path"`
				}
				if err := json.Unmarshal(expected, &exp); err != nil {
					t.Fatalf("unmarshal expected issue: %v", err)
				}
				assertHasIssue(t, result.Issues, exp.Code, exp.Path)
			}
		})
	}
}

// TestIssuesError tests that IssuesError produces a readable string.
func TestIssuesError(t *testing.T) {
	pr := &ProfileResult{
		Issues: []DefinitionIssue{
			{Code: "unsupportedKeyword", Path: "/valueSchema/properties/x/allOf"},
			{Code: "incompatibleIsEmpty", Path: "/umpire/fields/count"},
		},
	}
	err := pr.IssuesError()
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unsupportedKeyword") || !strings.Contains(msg, "incompatibleIsEmpty") {
		t.Fatalf("error missing expected codes: %s", msg)
	}
}

// TestParseProfile_InvalidJSON tests that malformed JSON returns a parse error.
func TestParseProfile_InvalidJSON(t *testing.T) {
	_, err := ParseProfile([]byte(`{invalid json`))
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got: %v", err)
	}
}

// TestParseProfile_NoIssuesEmptyProfile validates a minimal but valid profile.
func TestParseProfile_NoIssuesEmptyProfile(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	if len(result.Issues) > 0 {
		t.Fatalf("expected no issues for empty profile, got: %v", result.Issues)
	}
}

func assertHasIssue(t *testing.T, issues []DefinitionIssue, code, path string) {
	t.Helper()
	for _, iss := range issues {
		if iss.Code == code && iss.Path == path {
			return
		}
	}
	// Report all issues for debuggability.
	var msgs []string
	for _, iss := range issues {
		msgs = append(msgs, iss.Code+"@"+iss.Path)
	}
	t.Fatalf("expected issue code=%q path=%q, not found in [%s]", code, path, strings.Join(msgs, ", "))
}

// TestParseProfile_WrongTypeString checks that a non-object valueSchema type triggers invalidProfile.
func TestParseProfile_WrongTypeString(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "string"
		},
		"umpire": {
			"version": 1,
			"fields": {},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidProfile", "/valueSchema")
}

// TestParseProfile_DefaultMinLength checks that a default below minLength triggers invalidDefault.
func TestParseProfile_DefaultMinLength(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"name": { "type": "string", "minLength": 3 }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"name": { "isEmpty": "string", "default": "ab" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidDefault", "/umpire/fields/name/default")
}

// TestParseProfile_RefInOneOfCycleDetection checks that $ref targets inside oneOf branches
// are extracted for cycle detection.
func TestParseProfile_RefInOneOfCycleDetection(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"item": { "$ref": "#/$defs/A" }
			},
			"required": [],
			"additionalProperties": false,
			"$defs": {
				"A": {
					"oneOf": [
						{
							"type": "object",
							"properties": { "kind": { "const": "x" }, "val": { "$ref": "#/$defs/B" } },
							"required": ["kind"],
							"additionalProperties": false
						}
					]
				},
				"B": {
					"type": "object",
					"properties": {
						"parent": { "$ref": "#/$defs/A" }
					},
					"required": [],
					"additionalProperties": false
				}
			}
		},
		"umpire": {
			"version": 1,
			"fields": {
				"item": { "isEmpty": "present" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	want := []DefinitionIssue{{Code: "referenceCycle", Path: "/valueSchema/$defs/B"}}
	if len(result.Issues) != len(want) {
		t.Fatalf("issues length = %d, want %d: %+v", len(result.Issues), len(want), result.Issues)
	}
	if !reflect.DeepEqual(result.Issues, want) {
		t.Fatalf("issues = %+v, want %+v", result.Issues, want)
	}
}

// TestParseProfile_ExcludedKeywordUnevaluatedProperties recurses into
// unevaluatedProperties to detect excluded keywords in its subschema.
func TestParseProfile_ExcludedKeywordUnevaluatedProperties(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"extra": {
					"type": "object",
					"unevaluatedProperties": { "allOf": [{ "type": "string" }] }
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"extra": { "isEmpty": "object" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "unsupportedKeyword", "/valueSchema/properties/extra/unevaluatedProperties/allOf")
}

// TestParseProfile_ExcludedKeywordUnevaluatedPropertiesNested checks that
// unevaluatedProperties recursion reaches deeper nesting levels.
func TestParseProfile_ExcludedKeywordUnevaluatedPropertiesNested(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"extra": {
					"type": "object",
					"unevaluatedProperties": {
						"type": "object",
						"properties": {
							"nested": { "unevaluatedProperties": { "allOf": [{ "type": "string" }] } }
						}
					}
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"extra": { "isEmpty": "object" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "unsupportedKeyword", "/valueSchema/properties/extra/unevaluatedProperties/properties/nested/unevaluatedProperties/allOf")
}

// TestParseProfile_DefaultBooleanMismatch checks that a non-boolean default for a
// boolean property triggers invalidDefault.
func TestParseProfile_DefaultBooleanMismatch(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"enabled": { "type": "boolean" }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"enabled": { "isEmpty": "boolean", "default": "true" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidDefault", "/umpire/fields/enabled/default")
}

// TestParseProfile_DefaultBoolean checks that a valid boolean default passes cleanly.
func TestParseProfile_DefaultBoolean(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"enabled": { "type": "boolean" }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"enabled": { "isEmpty": "boolean", "default": true }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	if len(result.Issues) > 0 {
		t.Fatalf("expected no issues for valid boolean default, got: %v", result.Issues)
	}
}

// TestParseProfile_DefaultMaxLengthBoundary checks that a string default exactly at
// the maxLength limit is accepted.
func TestParseProfile_DefaultMaxLengthBoundary(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"name": { "type": "string", "maxLength": 3 }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"name": { "isEmpty": "string", "default": "abc" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	if len(result.Issues) > 0 {
		t.Fatalf("expected no issues for default at maxLength boundary, got: %v", result.Issues)
	}
}

// TestParseProfile_DefaultMaxLengthMultibyteOvershoot checks that a multi-byte string
// whose rune count exceeds maxLength triggers invalidDefault.
func TestParseProfile_DefaultMaxLengthMultibyteOvershoot(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"name": { "type": "string", "maxLength": 2 }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"name": { "isEmpty": "string", "default": "\u00e9\u00e9\u00e9" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidDefault", "/umpire/fields/name/default")
}

// TestParseProfile_DefaultMaxLength checks that a string default exceeding maxLength
// triggers invalidDefault.
func TestParseProfile_DefaultMaxLength(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"name": { "type": "string", "maxLength": 3 }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"name": { "isEmpty": "string", "default": "toolong" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidDefault", "/umpire/fields/name/default")
}

// TestParseProfile_DefaultMaxLengthMultibyte checks that maxLength counts Unicode
// code points, not bytes, so a multi-byte string at the limit stays valid.
func TestParseProfile_DefaultMaxLengthMultibyte(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"name": { "type": "string", "maxLength": 2 }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"name": { "isEmpty": "string", "default": "\u00e9\u00e9" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	if len(result.Issues) > 0 {
		t.Fatalf("expected no issues for 2-rune default with maxLength 2, got: %v", result.Issues)
	}
}

// TestParseProfile_OneOfBranchMissingProperties checks that a oneOf branch lacking
// a properties key triggers invalidDiscriminator.
func TestParseProfile_OneOfBranchMissingProperties(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"flag": {
					"oneOf": [
						{ "type": "string" },
						{ "type": "object", "properties": { "kind": { "const": "x" } }, "required": ["kind"], "additionalProperties": false }
					]
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"flag": { "isEmpty": "present" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidDiscriminator", "/valueSchema/properties/flag/oneOf")
}

// TestParseProfile_OneOfMultipleDiscriminators checks that a branch with more than
// one required string-const property is rejected as an ambiguous discriminator.
func TestParseProfile_OneOfMultipleDiscriminators(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"cmd": {
					"oneOf": [
						{
							"type": "object",
							"properties": { "kind": { "const": "run" }, "sub": { "const": "a" } },
							"required": ["kind", "sub"],
							"additionalProperties": false
						},
						{
							"type": "object",
							"properties": { "kind": { "const": "stop" }, "sub": { "const": "b" } },
							"required": ["kind", "sub"],
							"additionalProperties": false
						}
					]
				}
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"cmd": { "isEmpty": "present" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	assertHasIssue(t, result.Issues, "invalidDiscriminator", "/valueSchema/properties/cmd/oneOf")
}

// TestParseProfile_IsEmptyPresentAnyType checks that the "present" isEmpty strategy
// is compatible with any structural type (spec rule 5 constrains non-present only).
func TestParseProfile_IsEmptyPresentAnyType(t *testing.T) {
	data := `{
		"$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion": 1,
		"valueSchema": {
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"count": { "type": "integer" }
			},
			"required": [],
			"additionalProperties": false
		},
		"umpire": {
			"version": 1,
			"fields": {
				"count": { "isEmpty": "present" }
			},
			"rules": []
		}
	}`

	result, err := ParseProfile([]byte(data))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	if len(result.Issues) > 0 {
		t.Fatalf("expected no issues for present strategy on integer, got: %v", result.Issues)
	}
}

// TestComposedMode_OutputDirFromProfile checks that profile mode name defaults work.
func TestComposedMode_OutputDirFromProfile(t *testing.T) {
	umpireJSON := []byte(`{"version":1,"fields":{"x":{"isEmpty":"string"}},"rules":[]}`)
	valueSchemaJSON := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"x":{"type":"string"}},"additionalProperties":false}`)

	source, issues, err := GenerateComposed(umpireJSON, valueSchemaJSON, Config{PkgName: "p", SchemaName: "X"})
	if err != nil {
		t.Fatalf("GenerateComposed() error: %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("expected no issues, got: %v", issues)
	}
	if !strings.Contains(source, "type XFields struct") {
		t.Fatalf("missing expected struct: %s", source)
	}
}
