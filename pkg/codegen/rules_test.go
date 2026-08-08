package codegen

import (
	"testing"

	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
)

func TestCompileRules_NamedValidators(t *testing.T) {
	pattern := "^[A-Z]{2}$"
	minVal := 18.0
	maxVal := 100.0
	rangeMin := 0.0
	rangeMax := 999.0

	schema := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "country", TypeHint: "string"},
			{Name: "age", TypeHint: "number"},
			{Name: "score", TypeHint: "number"},
			{Name: "rating", TypeHint: "number"},
		},
		Validators: map[string]schema.ValidatorDef{
			"country": {Op: "matches", Pattern: pattern, Error: "must be a two-letter country code"},
			"age":     {Op: "min", Value: &minVal, Error: "age must be at least 18"},
			"score":   {Op: "max", Value: &maxVal, Error: "score must not exceed 100"},
			"rating":  {Op: "range", Min: &rangeMin, Max: &rangeMax, Error: "rating must be between 0 and 999"},
		},
	}

	rc := NewRuleCompiler(
		map[string]GoType{"country": GoString, "age": GoFloat64, "score": GoFloat64, "rating": GoFloat64},
		map[string]GoType{},
		schema.Fields,
	).WithSchema(schema)

	got := rc.CompileRules(nil)

	// Check country (matches)
	if got["Country"] == nil {
		t.Fatal("missing compiled data for Country")
	}
	if got["Country"].Validator == nil {
		t.Fatal("expected validator for Country")
	}
	wantExpr := `isValidRegexMatch(f.Country, "^[A-Z]{2}$")`
	if got["Country"].Validator.Expr != wantExpr {
		t.Fatalf("Country Validator.Expr = %q, want %q", got["Country"].Validator.Expr, wantExpr)
	}
	if got["Country"].Validator.Error != "must be a two-letter country code" {
		t.Fatalf("Country Validator.Error = %q, want %q", got["Country"].Validator.Error, "must be a two-letter country code")
	}

	// Check age (min)
	if got["Age"] == nil {
		t.Fatal("missing compiled data for Age")
	}
	if got["Age"].Validator == nil {
		t.Fatal("expected validator for Age")
	}
	wantExpr = "f.Age >= 18"
	if got["Age"].Validator.Expr != wantExpr {
		t.Fatalf("Age Validator.Expr = %q, want %q", got["Age"].Validator.Expr, wantExpr)
	}
	if got["Age"].Validator.Error != "age must be at least 18" {
		t.Fatalf("Age Validator.Error = %q, want %q", got["Age"].Validator.Error, "age must be at least 18")
	}

	// Check score (max)
	if got["Score"] == nil {
		t.Fatal("missing compiled data for Score")
	}
	if got["Score"].Validator == nil {
		t.Fatal("expected validator for Score")
	}
	wantExpr = "f.Score <= 100"
	if got["Score"].Validator.Expr != wantExpr {
		t.Fatalf("Score Validator.Expr = %q, want %q", got["Score"].Validator.Expr, wantExpr)
	}
	if got["Score"].Validator.Error != "score must not exceed 100" {
		t.Fatalf("Score Validator.Error = %q, want %q", got["Score"].Validator.Error, "score must not exceed 100")
	}

	// Check rating (range)
	if got["Rating"] == nil {
		t.Fatal("missing compiled data for Rating")
	}
	if got["Rating"].Validator == nil {
		t.Fatal("expected validator for Rating")
	}
	wantExpr = "f.Rating >= 0 && f.Rating <= 999"
	if got["Rating"].Validator.Expr != wantExpr {
		t.Fatalf("Rating Validator.Expr = %q, want %q", got["Rating"].Validator.Expr, wantExpr)
	}
	if got["Rating"].Validator.Error != "rating must be between 0 and 999" {
		t.Fatalf("Rating Validator.Error = %q, want %q", got["Rating"].Validator.Error, "rating must be between 0 and 999")
	}
}

func TestCompileRules_CheckUsesTargetFieldWhenExpressionFieldMissing(t *testing.T) {
	rc := NewRuleCompiler(
		map[string]GoType{"age": GoFloat64, "score": GoFloat64Ptr},
		map[string]GoType{},
		[]schema.FieldDef{
			{Name: "age", TypeHint: "number"},
			{Name: "score", IsEmpty: "number"},
		},
	)

	rules := []schema.Rule{
		{
			Type:   "check",
			Field:  "age",
			Check:  &schema.Expr{Op: "min", Value: float64(18)},
			Reason: "too young",
		},
		{
			Type:   "check",
			Field:  "score",
			Check:  &schema.Expr{Op: "max", Value: float64(100)},
			Reason: "too high",
		},
	}

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

	nullableField := got["Score"]
	if nullableField == nil {
		t.Fatal("missing compiled rule data for Score")
	}
	if nullableField.Check.Expr != "f.Score != nil && *f.Score <= 100" {
		t.Fatalf("nullable Check.Expr = %q, want %q", nullableField.Check.Expr, "f.Score != nil && *f.Score <= 100")
	}
}
