package schema

import "fmt"

// Schema is the root structure of a .umpire.json file.
type Schema struct {
	Fields     []FieldDef     `json:"fields"`
	Conditions []ConditionDef `json:"conditions"`
	Rules      []Rule         `json:"rules"`
}

// FieldDef defines a single field in the schema.
type FieldDef struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	// IsEmpty is a pointer so we can distinguish unset (nil) from false.
	IsEmpty *bool `json:"isEmpty,omitempty"`
	// TypeHint is an optional explicit type hint for the field.
	// When omitted, the codegen infers the type from usage in expressions.
	TypeHint string `json:"type,omitempty"`
}

// ConditionDef defines a single condition in the schema.
type ConditionDef struct {
	Name string `json:"name"`
	Type string `json:"type"` // "boolean" | "string" | "number" | "string[]" | "number[]"
}

// Expr represents an expression node in the schema.
// This is a tagged union: exactly one of Field, Condition, Value,
// and a subset of Exprs will be populated depending on the op type.
type Expr struct {
	Op        string `json:"op"` // "eq", "neq", "gt", "gte", "lt", "lte", "present", "and", "or", "not", "condEq", "condNot", "condGt", "condLt", etc.
	Field     string `json:"field,omitempty"`
	Condition string `json:"condition,omitempty"`
	Value     any    `json:"value,omitempty"`
	Exprs     []Expr `json:"exprs,omitempty"`
}

// Rule is a tagged union representing different rule types.
// The "type" field determines which additional fields are relevant.
type Rule struct {
	Type      string   `json:"type"`       // "enabledWhen", "disables", "requires", "fairWhen", "check", "excluded"
	Field     string   `json:"field,omitempty"`
	Fields    []string `json:"fields,omitempty"`
	Expr      *Expr    `json:"expr,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	DependsOn string   `json:"dependsOn,omitempty"`
	// Requires lists field names that must be satisfied (for "requires" rules).
	Requires []string `json:"requires,omitempty"`
	// Excluded marks a rule as excluded from evaluation.
	Excluded bool `json:"excluded,omitempty"`
	// Description for excluded rules.
	Description string `json:"description,omitempty"`
	// DisabledWhen is an expression that disables the field.
	DisabledWhen *Expr `json:"disabledWhen,omitempty"`
	// FairWhen is an expression that sets the fair property.
	FairWhen *Expr `json:"fairWhen,omitempty"`
	// Check is an expression used for validation.
	Check *Expr `json:"check,omitempty"`
}

// Validate checks that the schema has required sections.
func (s *Schema) Validate() error {
	if len(s.Fields) == 0 {
		return fmt.Errorf("schema must have at least one field definition")
	}
	return nil
}
