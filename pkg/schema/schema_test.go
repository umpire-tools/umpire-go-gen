package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePublicSchema(t *testing.T) {
	s, err := Parse([]byte(`{
		"version": 1,
		"fields": {"country": {"required": true}, "promoCode": {"isEmpty": "string"}},
		"conditions": {"userRole": {"type": "string"}, "isGuest": {"type": "boolean"}},
		"rules": [
			{"type": "enabledWhen", "field": "country", "when": {"op": "present", "field": "country"}, "reason": "Country is required"},
			{"type": "requires", "field": "promoCode", "dependency": "country"}
		],
		"excluded": [{"type": "legacy", "description": "Legacy shipping rule"}]
	}`))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(s.Fields) != 2 || s.Fields[0].Name != "country" || !s.Fields[0].Required || s.Fields[1].IsEmpty != "string" {
		t.Fatalf("unexpected fields: %+v", s.Fields)
	}
	if len(s.Conditions) != 2 || s.Conditions[0] != (ConditionDef{Name: "isGuest", Type: "boolean"}) || s.Conditions[1] != (ConditionDef{Name: "userRole", Type: "string"}) {
		t.Fatalf("unexpected conditions: %+v", s.Conditions)
	}
	if len(s.Rules) != 2 || s.Rules[0].Type != "enabledWhen" || s.Rules[0].Expr == nil || s.Rules[0].Expr.Op != "present" || s.Rules[1].Type != "requires" || len(s.Rules[1].Requires) != 1 || s.Rules[1].Requires[0] != "country" {
		t.Fatalf("unexpected rules: %+v", s.Rules)
	}
}

func TestExprUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected Expr
	}{
		{
			name:     "eq operator",
			json:     `{"op": "eq", "field": "country", "value": "US"}`,
			expected: Expr{Op: "eq", Field: "country", Value: "US"},
		},
		{
			name: "and combinator",
			json: `{"op": "and", "exprs": [{"op": "present", "field": "country"}, {"op": "eq", "field": "country", "value": "US"}]}`,
			expected: Expr{
				Op: "and",
				Exprs: []Expr{
					{Op: "present", Field: "country"},
					{Op: "eq", Field: "country", Value: "US"},
				},
			},
		},
		{
			name:     "condEq operator",
			json:     `{"op": "condEq", "condition": "userRole", "value": "admin"}`,
			expected: Expr{Op: "condEq", Condition: "userRole", Value: "admin"},
		},
		{
			name:     "not operator",
			json:     `{"op": "not", "exprs": [{"op": "present", "field": "country"}]}`,
			expected: Expr{Op: "not", Exprs: []Expr{{Op: "present", Field: "country"}}},
		},
		{
			name:     "gt operator with number",
			json:     `{"op": "gt", "field": "itemsCount", "value": 5}`,
			expected: Expr{Op: "gt", Field: "itemsCount", Value: float64(5)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Expr
			if err := json.Unmarshal([]byte(tt.json), &got); err != nil {
				t.Fatalf("Unmarshal() error: %v", err)
			}
			if got.Op != tt.expected.Op {
				t.Errorf("Op = %q, want %q", got.Op, tt.expected.Op)
			}
			if got.Field != tt.expected.Field {
				t.Errorf("Field = %q, want %q", got.Field, tt.expected.Field)
			}
			if got.Condition != tt.expected.Condition {
				t.Errorf("Condition = %q, want %q", got.Condition, tt.expected.Condition)
			}
			if got.Value != tt.expected.Value {
				t.Errorf("Value = %v, want %v", got.Value, tt.expected.Value)
			}
			if len(got.Exprs) != len(tt.expected.Exprs) {
				t.Errorf("Exprs count = %d, want %d", len(got.Exprs), len(tt.expected.Exprs))
			}
			for i := range tt.expected.Exprs {
				if got.Exprs[i].Op != tt.expected.Exprs[i].Op {
					t.Errorf("Exprs[%d].Op = %q, want %q", i, got.Exprs[i].Op, tt.expected.Exprs[i].Op)
				}
			}
		})
	}
}

func TestSchemaRoundTrip(t *testing.T) {
	original := Schema{
		Fields: []FieldDef{
			{Name: "country", Required: true, IsEmpty: "string"},
			{Name: "promoCode"},
		},
		Conditions: []ConditionDef{
			{Name: "userRole", Type: "string"},
			{Name: "isGuest", Type: "boolean"},
		},
		Rules: []Rule{
			{Type: "enabledWhen", Field: "country", Reason: "Required field"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Schema
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded.Fields) != len(original.Fields) {
		t.Errorf("fields count: got %d, want %d", len(decoded.Fields), len(original.Fields))
	}
	if len(decoded.Conditions) != len(original.Conditions) {
		t.Errorf("conditions count: got %d, want %d", len(decoded.Conditions), len(original.Conditions))
	}
	if len(decoded.Rules) != len(original.Rules) {
		t.Errorf("rules count: got %d, want %d", len(decoded.Rules), len(original.Rules))
	}
}

func TestEmptySchema(t *testing.T) {
	data := `{"fields": [], "conditions": [], "rules": []}`
	var s Schema
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Fields) != 0 || len(s.Conditions) != 0 || len(s.Rules) != 0 {
		t.Error("expected all empty arrays")
	}
}

func TestEmptyJSON(t *testing.T) {
	data := `{}`
	var s Schema
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Fields) != 0 || len(s.Conditions) != 0 || len(s.Rules) != 0 {
		t.Error("expected all empty arrays for empty object")
	}
}

func TestSchemaValidate(t *testing.T) {
	empty := Schema{}
	if err := empty.Validate(); err == nil {
		t.Error("expected error for empty schema")
	}
	withFields := Schema{Fields: []FieldDef{{Name: "test"}}}
	if err := withFields.Validate(); err != nil {
		t.Errorf("expected no error for schema with fields, got: %v", err)
	}
}

func TestParseRejectsLegacyArrayShape(t *testing.T) {
	assertParseError(t, `{"fields":[{"name":"email"}],"conditions":{},"rules":[]}`, "field")
}

func TestParseRejectsV1Failures(t *testing.T) {
	base := `"version":1,"fields":{"email":{}},"rules":[]`
	for _, test := range []struct{ name, document, want string }{
		{"unknown top-level", `{` + base + `,"unexpected":true}`, "field"},
		{"missing version", `{"fields":{"email":{}},"rules":[]}`, "version"},
		{"wrong version", `{"version":2,"fields":{"email":{}},"rules":[]}`, "version"},
		{"empty fields", `{"version":1,"fields":{},"rules":[]}`, "field"},
		{"unknown field member", `{"version":1,"fields":{"email":{"description":"no"}},"rules":[]}`, "unexpected"},
		{"non primitive default", `{"version":1,"fields":{"email":{"default":{}}},"rules":[]}`, "default"},
		{"invalid condition", `{"version":1,"fields":{"email":{}},"conditions":{"role":{"type":"role"}},"rules":[]}`, "condition"},
		{"unknown dependency", `{"version":1,"fields":{"email":{}},"rules":[{"type":"requires","field":"email","dependency":"missing"}]}`, "field"},
		{"unknown validator field", `{"version":1,"fields":{"email":{}},"rules":[],"validators":{"phone":{"op":"email"}}}`, "validator"},
		{"empty dependencies", `{"version":1,"fields":{"email":{}},"rules":[{"type":"requires","field":"email","dependencies":[]}]}`, "dependencies"},
		{"empty anyOf", `{"version":1,"fields":{"email":{}},"rules":[{"type":"anyOf","rules":[]}]}`, "anyOf"},
		{"empty eitherOf branch", `{"version":1,"fields":{"email":{}},"rules":[{"type":"eitherOf","group":"g","branches":{"a":[]}}]}`, "branch"},
		{"empty eitherOf", `{"version":1,"fields":{"email":{}},"rules":[{"type":"eitherOf","group":"g","branches":{}}]}`, "at least one branch"},
		{"mixed anyOf targets", `{"version":1,"fields":{"email":{},"phone":{}},"rules":[{"type":"anyOf","rules":[{"type":"enabledWhen","field":"email","when":{"op":"present","field":"email"}},{"type":"enabledWhen","field":"phone","when":{"op":"present","field":"phone"}}]}]}`, "same fields"},
		{"mixed anyOf constraints", `{"version":1,"fields":{"email":{}},"rules":[{"type":"anyOf","rules":[{"type":"enabledWhen","field":"email","when":{"op":"present","field":"email"}},{"type":"fairWhen","field":"email","when":{"op":"present","field":"email"}}]}]}`, "cannot mix"},
		{"mixed eitherOf targets", `{"version":1,"fields":{"email":{},"phone":{}},"rules":[{"type":"eitherOf","group":"contact","branches":{"email":[{"type":"enabledWhen","field":"email","when":{"op":"present","field":"email"}}],"phone":[{"type":"enabledWhen","field":"phone","when":{"op":"present","field":"phone"}}]}}]}`, "same fields"},
		{"invalid named validator regex", `{"version":1,"fields":{"email":{}},"rules":[],"validators":{"email":{"op":"matches","pattern":"["}}}`, "Invalid regex pattern"},
		{"empty excluded", `{"version":1,"fields":{"email":{}},"rules":[],"excluded":[{"type":"","description":""}]}`, "Excluded"},
	} {
		t.Run(test.name, func(t *testing.T) { assertParseError(t, test.document, test.want) })
	}
}

func TestParseAcceptsEmptyExpressionCombinators(t *testing.T) {
	for _, op := range []string{"and", "or"} {
		t.Run(op, func(t *testing.T) {
			document := `{"version":1,"fields":{"target":{}},"rules":[{"type":"enabledWhen","field":"target","when":{"op":"` + op + `","exprs":[]}}]}`
			if _, err := Parse([]byte(document)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
		})
	}
}

func assertParseError(t *testing.T, document, want string) {
	t.Helper()
	_, err := Parse([]byte(document))
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Parse() error = %v, want substring %q", err, want)
	}
}
