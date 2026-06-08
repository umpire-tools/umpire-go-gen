package schema

import (
	"fmt"
	"regexp"
)

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
	// IsEmpty holds the "empty" type indicator from the schema: "string", "number", "boolean", "array", "object".
	// When set, this also implies the field's type and that empty values are unsatisfied.
	IsEmpty string `json:"isEmpty,omitempty"`
	// TypeHint is an optional explicit type hint for the field.
	// When omitted, the codegen infers the type from usage in expressions or from IsEmpty.
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
	Pattern   string `json:"pattern,omitempty"`
}

// Rule is a tagged union representing different rule types.
// The "type" field determines which additional fields are relevant.
type Rule struct {
	Type      string   `json:"type"` // "enabledWhen", "disables", "requires", "fairWhen", "check", "excluded"
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

// Validate checks that the schema has required sections and valid references.
func (s *Schema) Validate() error {
	if len(s.Fields) == 0 {
		return fmt.Errorf("schema must have at least one field definition")
	}

	fieldNames := make(map[string]bool)
	for _, f := range s.Fields {
		fieldNames[f.Name] = true
	}

	conditionNames := make(map[string]bool)
	conditionTypes := make(map[string]string)
	for _, c := range s.Conditions {
		conditionNames[c.Name] = true
		conditionTypes[c.Name] = c.Type
	}

	// Check all expression field/condition references
	for _, r := range s.Rules {
		if r.Expr != nil {
			if err := validateExprRefs(r.Expr, fieldNames, conditionNames, conditionTypes); err != nil {
				return err
			}
		}
		if r.DisabledWhen != nil {
			if err := validateExprRefs(r.DisabledWhen, fieldNames, conditionNames, conditionTypes); err != nil {
				return err
			}
		}
		if r.FairWhen != nil {
			if err := validateExprRefs(r.FairWhen, fieldNames, conditionNames, conditionTypes); err != nil {
				return err
			}
		}
		if r.Check != nil {
			if err := validateExprRefs(r.Check, fieldNames, conditionNames, conditionTypes); err != nil {
				return err
			}
		}
	}

	// Check regex patterns in check expressions
	for _, r := range s.Rules {
		if r.Check != nil {
			if err := validateRegexPatterns(r.Check); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateExprRefs walks an expression tree and checks that all field and condition
// references are valid within the schema.
func validateExprRefs(e *Expr, fields map[string]bool, conditions map[string]bool, conditionTypes map[string]string) error {
	if e == nil {
		return nil
	}

	if e.Field != "" && !fields[e.Field] {
		return fmt.Errorf("unknown field %q in expression", e.Field)
	}

	if e.Condition != "" {
		if !conditions[e.Condition] {
			return fmt.Errorf("unknown condition %q in expression", e.Condition)
		}
		if e.Op == "fieldInCond" {
			t := conditionTypes[e.Condition]
			if t != "string[]" && t != "number[]" {
				return fmt.Errorf("%q requires an array condition", "fieldInCond")
			}
		}
	}

	for _, child := range e.Exprs {
		if err := validateExprRefs(&child, fields, conditions, conditionTypes); err != nil {
			return err
		}
	}

	return nil
}

// validateRegexPatterns walks an expression tree and validates that any regex
// patterns compile successfully.
func validateRegexPatterns(e *Expr) error {
	if e == nil {
		return nil
	}

	if e.Op == "matches" && e.Pattern != "" {
		if _, err := regexp.Compile(e.Pattern); err != nil {
			return fmt.Errorf("invalid regex pattern %q: %w", e.Pattern, err)
		}
	}

	for _, child := range e.Exprs {
		if err := validateRegexPatterns(&child); err != nil {
			return err
		}
	}

	return nil
}
