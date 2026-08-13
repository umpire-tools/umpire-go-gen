package umpiregen

import (
	"encoding/json"
	"errors"
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

func TestParseProfileRejectsUnknownCanonicalWrapperMember(t *testing.T) {
	data := `{
		"$schema":"https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion":1,
		"valueSchema":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{},"additionalProperties":false},
		"umpire":{"version":1,"fields":{},"rules":[]},
		"extra":true
	}`
	if _, err := ParseProfile([]byte(data)); err == nil || !strings.Contains(err.Error(), `unexpected key "extra"`) {
		t.Fatalf("ParseProfile() error = %v, want unknown wrapper rejection", err)
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

func TestParseProfile_ClosedVocabularyExcludedKeywords(t *testing.T) {
	tests := []struct {
		name    string
		keyword string
		value   string
	}{
		{name: "composition allOf", keyword: "allOf", value: `[]`},
		{name: "composition anyOf", keyword: "anyOf", value: `[]`},
		{name: "composition not", keyword: "not", value: `{}`},
		{name: "numeric multipleOf", keyword: "multipleOf", value: `2`},
		{name: "array contains", keyword: "contains", value: `{}`},
		{name: "conditional if", keyword: "if", value: `{}`},
		{name: "conditional then", keyword: "then", value: `{}`},
		{name: "conditional else", keyword: "else", value: `{}`},
		{name: "tuple prefixItems", keyword: "prefixItems", value: `[]`},
		{name: "array uniqueItems", keyword: "uniqueItems", value: `true`},
		{name: "array minContains", keyword: "minContains", value: `1`},
		{name: "array maxContains", keyword: "maxContains", value: `1`},
		{name: "object dependentSchemas", keyword: "dependentSchemas", value: `{}`},
		{name: "object dependentRequired", keyword: "dependentRequired", value: `{}`},
		{name: "legacy dependencies", keyword: "dependencies", value: `{}`},
		{name: "object patternProperties", keyword: "patternProperties", value: `{}`},
		{name: "object propertyNames", keyword: "propertyNames", value: `{}`},
		{name: "object minProperties", keyword: "minProperties", value: `1`},
		{name: "object maxProperties", keyword: "maxProperties", value: `1`},
		{name: "string pattern", keyword: "pattern", value: `"x"`},
		{name: "format", keyword: "format", value: `"email"`},
		{name: "content encoding", keyword: "contentEncoding", value: `"base64"`},
		{name: "content media type", keyword: "contentMediaType", value: `"text/plain"`},
		{name: "content schema", keyword: "contentSchema", value: `{}`},
		{name: "dynamic ref", keyword: "$dynamicRef", value: `"#node"`},
		{name: "dynamic anchor", keyword: "$dynamicAnchor", value: `"node"`},
		{name: "recursive ref", keyword: "$recursiveRef", value: `"#"`},
		{name: "recursive anchor", keyword: "$recursiveAnchor", value: `true`},
		{name: "schema default", keyword: "default", value: `"x"`},
		{name: "nullable", keyword: "nullable", value: `true`},
		{name: "unevaluated items", keyword: "unevaluatedItems", value: `false`},
		{name: "unevaluated properties", keyword: "unevaluatedProperties", value: `false`},
		{name: "schema id", keyword: "$id", value: `"x"`},
		{name: "schema anchor", keyword: "$anchor", value: `"x"`},
		{name: "schema vocabulary", keyword: "$vocabulary", value: `{}`},
		{name: "schema comment", keyword: "$comment", value: `"x"`},
		{name: "legacy definitions", keyword: "definitions", value: `{}`},
		{name: "legacy additionalItems", keyword: "additionalItems", value: `false`},
		{name: "annotation examples", keyword: "examples", value: `[]`},
		{name: "annotation readOnly", keyword: "readOnly", value: `true`},
		{name: "annotation writeOnly", keyword: "writeOnly", value: `true`},
		{name: "annotation deprecated", keyword: "deprecated", value: `true`},
		{name: "unknown extension", keyword: "x-custom", value: `true`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema := fmt.Sprintf(`{"type":"string",%q:%s}`, test.keyword, test.value)
			source, issues, err := GenerateProfile(profileSchemaFixtureJSON(schema, ""), Config{PkgName: "closedvocabulary", SchemaName: "ClosedVocabulary"})
			var definitionErr *DefinitionError
			if source != "" || !errors.As(err, &definitionErr) {
				t.Fatalf("GenerateProfile() = source %q, err %T %v; want closed DefinitionError", source, err, err)
			}
			assertHasIssue(t, issues, "unsupportedKeyword", "/valueSchema/properties/value/"+escapeProfilePointer(test.keyword))
		})
	}
}

func TestParseProfile_UnsupportedAndUntypedSchemaShapes(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		path   string
	}{
		{name: "untyped", schema: `{}`, path: "/valueSchema/properties/value"},
		{name: "null type", schema: `{"type":"null"}`, path: "/valueSchema/properties/value/type"},
		{name: "nullable type array", schema: `{"type":["string","null"]}`, path: "/valueSchema/properties/value/type"},
		{name: "tuple items array", schema: `{"type":"array","items":[{"type":"string"}]}`, path: "/valueSchema/properties/value/items"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := parseProfileSchemaFixture(t, test.schema, "")
			assertHasIssue(t, result.Issues, "invalidProfile", test.path)
		})
	}
}

func TestParseProfile_AcceptsPrimitiveEnumAndConstVocabulary(t *testing.T) {
	for name, schema := range map[string]string{
		"string enum":            `{"type":"string","enum":["a","b"]}`,
		"boolean enum":           `{"type":"boolean","enum":[true,false]}`,
		"integer enum":           `{"type":"integer","enum":[1,2]}`,
		"number enum":            `{"type":"number","enum":[1.5,2]}`,
		"string const":           `{"type":"string","const":"a"}`,
		"boolean const":          `{"type":"boolean","const":true}`,
		"integer const":          `{"type":"integer","const":1}`,
		"integer decimal const":  `{"type":"integer","const":1.0}`,
		"integer exponent const": `{"type":"integer","const":1e0}`,
		"integer exponent enum":  `{"type":"integer","enum":[1e0,2.0]}`,
		"number const":           `{"type":"number","const":1.5}`,
	} {
		t.Run(name, func(t *testing.T) {
			result := parseProfileSchemaFixture(t, schema, "")
			if len(result.Issues) != 0 {
				t.Fatalf("accepted schema returned issues: %+v", result.Issues)
			}
		})
	}
}

func TestParseProfile_UnsafeSchemaLiteral(t *testing.T) {
	result := parseProfileSchemaFixture(t, `{"type":"integer","const":9007199254740992}`, "")
	assertHasIssue(t, result.Issues, "unsafeNumber", "/valueSchema/properties/value/const")
}

func TestParseProfile_RejectsUnrepresentableCountsAndIntegerBounds(t *testing.T) {
	for _, test := range []struct {
		name   string
		schema string
		path   string
	}{
		{name: "minItems", schema: `{"type":"array","items":{"type":"string"},"minItems":9007199254740992}`, path: "/valueSchema/properties/value/minItems"},
		{name: "maxItems", schema: `{"type":"array","items":{"type":"string"},"maxItems":9007199254740992}`, path: "/valueSchema/properties/value/maxItems"},
		{name: "minLength", schema: `{"type":"string","minLength":9007199254740992}`, path: "/valueSchema/properties/value/minLength"},
		{name: "maxLength", schema: `{"type":"string","maxLength":9007199254740992}`, path: "/valueSchema/properties/value/maxLength"},
		{name: "integer bound", schema: `{"type":"integer","maximum":9007199254740992}`, path: "/valueSchema/properties/value/maximum"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := parseProfileSchemaFixture(t, test.schema, "")
			assertHasIssue(t, result.Issues, "unsafeNumber", test.path)
		})
	}

	result := parseProfileSchemaFixture(t, `{"type":"string","maxLength":9007199254740991}`, "")
	if len(result.Issues) != 0 {
		t.Fatalf("safe-integer count boundary returned issues: %+v", result.Issues)
	}
}

func TestParseProfile_ResolvesEscapedDefinitionReferences(t *testing.T) {
	profile := fmt.Sprintf(`{
		"$schema":%q,
		"profileVersion":1,
		"valueSchema":{
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"type":"object",
			"properties":{"value":{"$ref":"#/$defs/a~1b~0c"}},
			"additionalProperties":false,
			"$defs":{"a/b~c":{"type":"object","properties":{"name":{"type":"string"}},"additionalProperties":false}}
		},
		"umpire":{"version":1,"fields":{"value":{"isEmpty":"present"}},"rules":[]}
	}`, ProfileSchemaURI)
	source, issues, err := GenerateProfile([]byte(profile), Config{PkgName: "escapedref", SchemaName: "EscapedRef"})
	if err != nil {
		t.Fatalf("GenerateProfile() error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("escaped reference returned issues: %+v", issues)
	}
	if !strings.Contains(source, "func DecodeEscapedRef") {
		t.Fatalf("escaped reference did not produce structural output")
	}

	bad := strings.Replace(profile, "#/$defs/a~1b~0c", "#/$defs/a~2b~0c", 1)
	result, err := ParseProfile([]byte(bad))
	if err != nil {
		t.Fatal(err)
	}
	assertHasIssue(t, result.Issues, "invalidReference", "/valueSchema/properties/value/$ref")
}

func TestParseProfile_DefinitionPathsEscapePropertyNames(t *testing.T) {
	schema := `{"type":"object","properties":{"a/b~c":{"type":"string","multipleOf":2}},"additionalProperties":false}`
	result := parseProfileSchemaFixture(t, schema, "")
	assertHasIssue(t, result.Issues, "unsupportedKeyword", "/valueSchema/properties/value/properties/a~1b~0c/multipleOf")
}

func TestParseProfile_OneOfBranchesAreRecursivelyValidated(t *testing.T) {
	schema := `{"oneOf":[
		{"type":"object","properties":{"kind":{"const":"a"},"payload":{"$ref":"#/bad"}},"required":["kind"],"additionalProperties":false},
		{"type":"object","properties":{"kind":{"const":"b"},"count":{"type":"integer","multipleOf":2}},"required":["kind"],"additionalProperties":false}
	]}`
	result := parseProfileSchemaFixture(t, schema, "")
	assertHasIssue(t, result.Issues, "invalidReference", "/valueSchema/properties/value/oneOf/0/properties/payload/$ref")
	assertHasIssue(t, result.Issues, "unsupportedKeyword", "/valueSchema/properties/value/oneOf/1/properties/count/multipleOf")
}

func TestParseProfile_UnionBranchObjectInvariants(t *testing.T) {
	for _, test := range []struct {
		name   string
		branch string
		path   string
	}{
		{name: "missing properties", branch: `{"type":"object","required":[],"additionalProperties":false}`, path: "/valueSchema/properties/value/oneOf/0"},
		{name: "non-object properties", branch: `{"type":"object","properties":[],"required":[],"additionalProperties":false}`, path: "/valueSchema/properties/value/oneOf/0"},
		{name: "undeclared required", branch: `{"type":"object","properties":{"kind":{"const":"a"}},"required":["kind","missing"],"additionalProperties":false}`, path: "/valueSchema/properties/value/oneOf/0/required"},
		{name: "additional properties true", branch: `{"type":"object","properties":{"kind":{"const":"a"}},"required":["kind"],"additionalProperties":true}`, path: "/valueSchema/properties/value/oneOf/0/additionalProperties"},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema := fmt.Sprintf(`{"oneOf":[%s,{"type":"object","properties":{"kind":{"const":"b"}},"required":["kind"],"additionalProperties":false}]}`, test.branch)
			result := parseProfileSchemaFixture(t, schema, "")
			assertHasIssue(t, result.Issues, "invalidProfile", test.path)
		})
	}
}

func TestParseProfile_ObjectInvariants(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		path   string
	}{
		{name: "nested missing explicit type", schema: `{"properties":{},"additionalProperties":false}`, path: "/valueSchema/properties/value"},
		{name: "nested properties null", schema: `{"type":"object","properties":null,"additionalProperties":false}`, path: "/valueSchema/properties/value"},
		{name: "nested properties not object", schema: `{"type":"object","properties":[],"additionalProperties":false}`, path: "/valueSchema/properties/value"},
		{name: "nested not closed", schema: `{"type":"object","properties":{}}`, path: "/valueSchema/properties/value"},
		{name: "nested additional properties null", schema: `{"type":"object","properties":{},"additionalProperties":null}`, path: "/valueSchema/properties/value/additionalProperties"},
		{name: "nested additional properties wrong type", schema: `{"type":"object","properties":{},"additionalProperties":0}`, path: "/valueSchema/properties/value/additionalProperties"},
		{name: "nested required null", schema: `{"type":"object","properties":{},"required":null,"additionalProperties":false}`, path: "/valueSchema/properties/value/required"},
		{name: "nested required wrong type", schema: `{"type":"object","properties":{},"required":{},"additionalProperties":false}`, path: "/valueSchema/properties/value/required"},
		{name: "nested required undeclared", schema: `{"type":"object","properties":{},"required":["missing"],"additionalProperties":false}`, path: "/valueSchema/properties/value/required"},
		{name: "union branch required null", schema: `{"oneOf":[{"type":"object","properties":{"kind":{"const":"a"}},"required":null,"additionalProperties":false},{"type":"object","properties":{"kind":{"const":"b"}},"required":["kind"],"additionalProperties":false}]}`, path: "/valueSchema/properties/value/oneOf/0/required"},
		{name: "union branch not closed", schema: `{"oneOf":[{"type":"object","properties":{"kind":{"const":"a"}},"required":["kind"],"additionalProperties":true},{"type":"object","properties":{"kind":{"const":"b"}},"required":["kind"],"additionalProperties":false}]}`, path: "/valueSchema/properties/value/oneOf/0/additionalProperties"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := parseProfileSchemaFixture(t, test.schema, "")
			assertHasIssue(t, result.Issues, "invalidProfile", test.path)
		})
	}

	rootCases := []struct {
		name        string
		valueSchema string
		path        string
	}{
		{name: "root missing properties", valueSchema: `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false}`, path: "/valueSchema"},
		{name: "root properties null", valueSchema: `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":null,"additionalProperties":false}`, path: "/valueSchema"},
		{name: "root additional properties null", valueSchema: `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{},"additionalProperties":null}`, path: "/valueSchema/additionalProperties"},
		{name: "root not closed", valueSchema: `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{},"additionalProperties":true}`, path: "/valueSchema/additionalProperties"},
		{name: "root required null", valueSchema: `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{},"required":null,"additionalProperties":false}`, path: "/valueSchema/required"},
		{name: "root required undeclared", valueSchema: `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{},"required":["missing"],"additionalProperties":false}`, path: "/valueSchema/required"},
		{name: "root defs null", valueSchema: `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{},"additionalProperties":false,"$defs":null}`, path: "/valueSchema/$defs"},
		{name: "root union", valueSchema: `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{},"additionalProperties":false,"oneOf":[]}`, path: "/valueSchema/oneOf"},
	}
	for _, test := range rootCases {
		t.Run(test.name, func(t *testing.T) {
			profile := fmt.Sprintf(`{"$schema":%q,"profileVersion":1,"valueSchema":%s,"umpire":{"version":1,"fields":{},"rules":[]}}`, ProfileSchemaURI, test.valueSchema)
			result, err := ParseProfile([]byte(profile))
			if err != nil {
				t.Fatalf("ParseProfile() error: %v", err)
			}
			code := "invalidProfile"
			if test.name == "root union" {
				code = "unsupportedKeyword"
			}
			assertHasIssue(t, result.Issues, code, test.path)
		})
	}
}

func TestParseProfile_MalformedSupportedKeywordShapes(t *testing.T) {
	for _, test := range []struct {
		name   string
		schema string
		code   string
		path   string
	}{
		{name: "null title", schema: `{"type":"string","title":null}`, code: "invalidProfile", path: "/valueSchema/properties/value/title"},
		{name: "wrong title type", schema: `{"type":"string","title":false}`, code: "invalidProfile", path: "/valueSchema/properties/value/title"},
		{name: "null description", schema: `{"type":"string","description":null}`, code: "invalidProfile", path: "/valueSchema/properties/value/description"},
		{name: "null type", schema: `{"type":null}`, code: "invalidProfile", path: "/valueSchema/properties/value/type"},
		{name: "null items", schema: `{"type":"array","items":null}`, code: "invalidProfile", path: "/valueSchema/properties/value/items"},
		{name: "null enum", schema: `{"type":"string","enum":null}`, code: "invalidProfile", path: "/valueSchema/properties/value/enum"},
		{name: "null oneOf", schema: `{"oneOf":null}`, code: "invalidDiscriminator", path: "/valueSchema/properties/value/oneOf"},
		{name: "null minItems", schema: `{"type":"array","items":{"type":"string"},"minItems":null}`, code: "invalidProfile", path: "/valueSchema/properties/value/minItems"},
		{name: "wrong minLength type", schema: `{"type":"string","minLength":false}`, code: "invalidProfile", path: "/valueSchema/properties/value/minLength"},
		{name: "null minimum", schema: `{"type":"number","minimum":null}`, code: "unsafeNumber", path: "/valueSchema/properties/value/minimum"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := parseProfileSchemaFixture(t, test.schema, "")
			assertHasIssue(t, result.Issues, test.code, test.path)
		})
	}
}

func TestParseProfile_ExplicitNullDefault(t *testing.T) {
	result := parseProfileSchemaFixture(t, `{"type":"string"}`, `,"default":null`)
	assertHasIssue(t, result.Issues, "invalidDefault", "/umpire/fields/value/default")
}

func parseProfileSchemaFixture(t *testing.T, propertySchema, fieldSuffix string) *ProfileResult {
	t.Helper()
	result, err := ParseProfile(profileSchemaFixtureJSON(propertySchema, fieldSuffix))
	if err != nil {
		t.Fatalf("ParseProfile() error: %v", err)
	}
	return result
}

func profileSchemaFixtureJSON(propertySchema, fieldSuffix string) []byte {
	return []byte(fmt.Sprintf(`{
		"$schema":%q,
		"profileVersion":1,
		"valueSchema":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"value":%s},"additionalProperties":false},
		"umpire":{"version":1,"fields":{"value":{"isEmpty":"present"%s}},"rules":[]}
	}`, ProfileSchemaURI, propertySchema, fieldSuffix))
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
func TestGenerateProfileRejectsGeneratedSymbolCollisions(t *testing.T) {
	profile := profileSchemaFixtureJSON(`{"type":"string"}`, "")
	for _, test := range []struct {
		name string
		cfg  Config
		path string
	}{
		{name: "schema helper", cfg: Config{PkgName: "x", SchemaName: "Issue"}, path: "/generation/helpers"},
		{name: "configured fields", cfg: Config{PkgName: "x", SchemaName: "Doc", FieldsName: "Doc"}, path: "/generation/fieldsName"},
		{name: "invalid configured keyword", cfg: Config{PkgName: "x", SchemaName: "type"}, path: "/generation/schemaName"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, issues, err := GenerateProfile(profile, test.cfg)
			var definitionErr *DefinitionError
			if !errors.As(err, &definitionErr) || source != "" {
				t.Fatalf("GenerateProfile() = source %q, err %T %v; want closed DefinitionError", source, err, err)
			}
			code := "nameCollision"
			if test.name == "invalid configured keyword" {
				code = "invalidName"
			}
			assertHasIssue(t, issues, code, test.path)
		})
	}
}

func TestGenerateProfileRejectsNestedTypeAndGeneratedConstantCollisions(t *testing.T) {
	profile := []byte(fmt.Sprintf(`{
		"$schema":%q,"profileVersion":1,
		"valueSchema":{
			"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object",
			"properties":{"profile":{"type":"object","properties":{},"additionalProperties":false}},
			"additionalProperties":false,
			"$defs":{"profile":{"type":"object","properties":{},"additionalProperties":false}}
		},
		"umpire":{"version":1,"fields":{"profile":{"isEmpty":"object"}},"rules":[]}
	}`, ProfileSchemaURI))
	source, issues, err := GenerateProfile(profile, Config{PkgName: "x", SchemaName: "Doc"})
	var definitionErr *DefinitionError
	if source != "" || !errors.As(err, &definitionErr) {
		t.Fatalf("GenerateProfile() = source %q, err %T %v; want closed DefinitionError", source, err, err)
	}
	assertHasIssue(t, issues, "nameCollision", "/valueSchema/$defs/profile")

	for _, schema := range []string{
		`{"type":"string","enum":["a-b","a_b"]}`,
		`{"oneOf":[
			{"type":"object","properties":{"kind":{"const":"a-b"}},"required":["kind"],"additionalProperties":false},
			{"type":"object","properties":{"kind":{"const":"a_b"}},"required":["kind"],"additionalProperties":false}
		]}`,
	} {
		result := parseProfileSchemaFixture(t, schema, "")
		found := false
		for _, issue := range result.Issues {
			found = found || issue.Code == "nameCollision"
		}
		if !found {
			t.Fatalf("generated constant collision not rejected: %+v", result.Issues)
		}
	}
}

func TestGenerateProfileRejectsStructuralHelperCollision(t *testing.T) {
	profile := []byte(fmt.Sprintf(`{
		"$schema":%q,"profileVersion":1,
		"valueSchema":{
			"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object",
			"properties":{"value":{"$ref":"#/$defs/structuralKind"}},
			"additionalProperties":false,
			"$defs":{"structuralKind":{"type":"string"}}
		},
		"umpire":{"version":1,"fields":{"value":{}},"rules":[]}
	}`, ProfileSchemaURI))
	source, issues, err := GenerateProfile(profile, Config{PkgName: "x", SchemaName: "Doc"})
	var definitionErr *DefinitionError
	if source != "" || !errors.As(err, &definitionErr) {
		t.Fatalf("GenerateProfile() = source %q, err %T %v; want closed DefinitionError", source, err, err)
	}
	assertHasIssue(t, issues, "nameCollision", "/valueSchema/$defs/structuralKind")
}

func TestUnionSymbolValidationIgnoresBranchLocalStringConsts(t *testing.T) {
	schema := `{"oneOf":[
		{"type":"object","properties":{"kind":{"const":"a"},"label":{"type":"string","const":"shared-local"}},"required":["kind"],"additionalProperties":false},
		{"type":"object","properties":{"kind":{"const":"b"},"label":{"type":"string","const":"shared-local"}},"required":["kind"],"additionalProperties":false}
	]}`
	result := parseProfileSchemaFixture(t, schema, "")
	if len(result.Issues) != 0 {
		t.Fatalf("branch-local consts created definition issues: %+v", result.Issues)
	}
}

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

// TestParseProfile_ExcludedKeywordUnevaluatedProperties rejects the unsupported
// keyword at its own path without treating its value as a supported subschema.
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
	assertHasIssue(t, result.Issues, "unsupportedKeyword", "/valueSchema/properties/extra/unevaluatedProperties")
}

// TestParseProfile_ExcludedKeywordUnevaluatedPropertiesNested confirms an
// unsupported schema-valued keyword is rejected rather than silently recursed.
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
	assertHasIssue(t, result.Issues, "unsupportedKeyword", "/valueSchema/properties/extra/unevaluatedProperties")
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
