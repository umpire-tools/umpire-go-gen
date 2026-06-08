package schema

import (
	"encoding/json"
	"testing"
)

func TestSchemaUnmarshal(t *testing.T) {
	data := `{
  "fields": [
    {"name": "country", "required": true, "isEmpty": false},
    {"name": "promoCode", "type": "string"}
  ],
  "conditions": [
    {"name": "userRole", "type": "string"},
    {"name": "isGuest", "type": "boolean"}
  ],
  "rules": [
    {
      "type": "enabledWhen",
      "field": "country",
      "expr": {"op": "present", "field": "country"},
      "reason": "Country is required"
    },
    {
      "type": "requires",
      "field": "promoCode",
      "requires": ["country"]
    },
    {
      "type": "excluded",
      "description": "Legacy shipping rule"
    }
  ]
}`

	var s Schema
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if len(s.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(s.Fields))
	}
	if s.Fields[0].Name != "country" {
		t.Errorf("expected field name 'country', got %q", s.Fields[0].Name)
	}
	if !s.Fields[0].Required {
		t.Error("expected country to be required")
	}
	if s.Fields[0].IsEmpty != nil && *s.Fields[0].IsEmpty {
		t.Error("expected isEmpty to be false")
	}
	if s.Fields[1].Name != "promoCode" {
		t.Errorf("expected field name 'promoCode', got %q", s.Fields[1].Name)
	}
	if s.Fields[1].TypeHint != "string" {
		t.Errorf("expected promoCode typeHint 'string', got %q", s.Fields[1].TypeHint)
	}

	if len(s.Conditions) != 2 {
		t.Errorf("expected 2 conditions, got %d", len(s.Conditions))
	}
	if s.Conditions[0].Name != "userRole" || s.Conditions[0].Type != "string" {
		t.Errorf("unexpected first condition: %+v", s.Conditions[0])
	}
	if s.Conditions[1].Name != "isGuest" || s.Conditions[1].Type != "boolean" {
		t.Errorf("unexpected second condition: %+v", s.Conditions[1])
	}

	if len(s.Rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(s.Rules))
	}

	rule := s.Rules[0]
	if rule.Type != "enabledWhen" {
		t.Errorf("expected rule type 'enabledWhen', got %q", rule.Type)
	}
	if rule.Field != "country" {
		t.Errorf("expected rule field 'country', got %q", rule.Field)
	}
	if rule.Reason != "Country is required" {
		t.Errorf("expected rule reason 'Country is required', got %q", rule.Reason)
	}
	if rule.Expr == nil || rule.Expr.Op != "present" {
		t.Errorf("unexpected expr: %+v", rule.Expr)
	}

	rule2 := s.Rules[1]
	if rule2.Type != "requires" {
		t.Errorf("expected rule type 'requires', got %q", rule2.Type)
	}
	if len(rule2.Requires) != 1 || rule2.Requires[0] != "country" {
		t.Errorf("expected requires ['country'], got %v", rule2.Requires)
	}

	rule3 := s.Rules[2]
	if rule3.Type != "excluded" {
		t.Errorf("expected rule type 'excluded', got %q", rule3.Type)
	}
	if rule3.Description != "Legacy shipping rule" {
		t.Errorf("expected description 'Legacy shipping rule', got %q", rule3.Description)
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
	isEmpty := false
	original := Schema{
		Fields: []FieldDef{
			{Name: "country", Required: true, IsEmpty: &isEmpty},
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
