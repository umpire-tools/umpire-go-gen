package codegen

import (
	"testing"

	"github.com/umpire-tools/umpire-gen/pkg/schema"
)

func TestCompileRules_CheckUsesTargetFieldWhenExpressionFieldMissing(t *testing.T) {
	rc := NewRuleCompiler(
		map[string]GoType{"age": GoFloat64},
		map[string]GoType{},
		[]schema.FieldDef{{Name: "age", TypeHint: "number"}},
	)

	rules := []schema.Rule{{
		Type:   "check",
		Field:  "age",
		Check:  &schema.Expr{Op: "min", Value: float64(18)},
		Reason: "too young",
	}}

	got := rc.CompileRules(rules)
	field := got["Age"]
	if field == nil {
		t.Fatal("missing compiled rule data for Age")
	}
	if field.Check.Expr != "f.Age >= 18" {
		t.Fatalf("Check.Expr = %q, want %q", field.Check.Expr, "f.Age >= 18")
	}
	if field.Check.Reason != "too young" {
		t.Fatalf("Check.Reason = %q, want %q", field.Check.Reason, "too young")
	}
}
