package codegen

import (
	"testing"

	"github.com/umpire-tools/umpire-gen/pkg/schema"
)

func TestCompile_EqOperator(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"country": GoStringPtr,
			"count":   GoInt,
			"enabled": GoBoolPtr,
			"price":   GoFloat64Ptr,
		},
		map[string]GoType{},
	)

	tests := []struct {
		name   string
		expr   *schema.Expr
		want   string
	}{
		{
			name: "pointer string field",
			expr: &schema.Expr{Op: "eq", Field: "country", Value: "US"},
			want: `f.Country != nil && *f.Country == "US"`,
		},
		{
			name: "non-pointer int field",
			expr: &schema.Expr{Op: "eq", Field: "count", Value: 5},
			want: `f.Count == 5`,
		},
		{
			name: "pointer bool field",
			expr: &schema.Expr{Op: "eq", Field: "enabled", Value: true},
			want: `f.Enabled != nil && *f.Enabled == true`,
		},
		{
			name: "pointer float64 field",
			expr: &schema.Expr{Op: "eq", Field: "price", Value: 9.99},
			want: `f.Price != nil && *f.Price == 9.99`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := comp.Compile(tt.expr)
			if err != nil {
				t.Fatalf("Compile() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Compile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompile_NeqOperator(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"status": GoStringPtr,
		},
		map[string]GoType{},
	)

	tests := []struct {
		name string
		expr *schema.Expr
		want string
	}{
		{
			name: "pointer string field",
			expr: &schema.Expr{Op: "neq", Field: "status", Value: "disabled"},
			want: `f.Status != nil && *f.Status != "disabled"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := comp.Compile(tt.expr)
			if err != nil {
				t.Fatalf("Compile() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Compile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompile_ComparisonOperators(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"age":   GoInt,
			"score": GoFloat64,
			"qty":   GoIntPtr,
		},
		map[string]GoType{},
	)

	tests := []struct {
		name string
		expr *schema.Expr
		want string
	}{
		{"gt int", &schema.Expr{Op: "gt", Field: "age", Value: 18}, "f.Age > 18"},
		{"gte int", &schema.Expr{Op: "gte", Field: "age", Value: 21}, "f.Age >= 21"},
		{"lt int", &schema.Expr{Op: "lt", Field: "age", Value: 65}, "f.Age < 65"},
		{"lte int", &schema.Expr{Op: "lte", Field: "age", Value: 100}, "f.Age <= 100"},
		{"gt float", &schema.Expr{Op: "gt", Field: "score", Value: 0.5}, "f.Score > 0.5"},
		{"gte float", &schema.Expr{Op: "gte", Field: "score", Value: 1.0}, "f.Score >= 1"},
		{"lt float", &schema.Expr{Op: "lt", Field: "score", Value: 100}, "f.Score < 100"},
		{"lte float", &schema.Expr{Op: "lte", Field: "score", Value: 50.5}, "f.Score <= 50.5"},
		{"gt pointer int", &schema.Expr{Op: "gt", Field: "qty", Value: 0}, "f.Qty != nil && *f.Qty > 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := comp.Compile(tt.expr)
			if err != nil {
				t.Fatalf("Compile() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Compile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompile_InOperator(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"role":  GoStringPtr,
			"tags":  GoStringSlice,
			"count": GoInt,
		},
		map[string]GoType{},
	)

	tests := []struct {
		name string
		expr *schema.Expr
		want string
	}{
		{
			name: "pointer string field",
			expr: &schema.Expr{Op: "in", Field: "role", Value: []any{"admin", "user"}},
			want: `(f.Role != nil && *f.Role == "admin" || f.Role != nil && *f.Role == "user")`,
		},
		{
			name: "slice field",
			expr: &schema.Expr{Op: "in", Field: "tags", Value: "important"},
			want: `contains(f.Tags, "important")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := comp.Compile(tt.expr)
			if err != nil {
				t.Fatalf("Compile() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Compile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompile_PresentOperator(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"name":    GoStringPtr,
			"count":   GoInt,
			"enabled": GoBoolPtr,
		},
		map[string]GoType{},
	)

	tests := []struct {
		name string
		expr *schema.Expr
		want string
	}{
		{
			name: "pointer field",
			expr: &schema.Expr{Op: "present", Field: "name"},
			want: "f.Name != nil",
		},
		{
			name: "non-pointer field",
			expr: &schema.Expr{Op: "present", Field: "count"},
			want: "true",
		},
		{
			name: "bool pointer field",
			expr: &schema.Expr{Op: "present", Field: "enabled"},
			want: "f.Enabled != nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := comp.Compile(tt.expr)
			if err != nil {
				t.Fatalf("Compile() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Compile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompile_ConditionOperators(t *testing.T) {
	comp := NewExprCompiler(map[string]GoType{}, map[string]GoType{
		"userRole":          GoString,
		"isGuest":           GoBool,
		"level":             GoInt,
		"weatherBand":       GoString,
		"availableStarters": GoStringSlice,
	})

	tests := []struct {
		name string
		expr *schema.Expr
		want string
	}{
		{"condEq string", &schema.Expr{Op: "condEq", Condition: "userRole", Value: "admin"}, `c.UserRole == "admin"`},
		{"condEq bool", &schema.Expr{Op: "condEq", Condition: "isGuest", Value: true}, `c.IsGuest == true`},
		{"condEq number", &schema.Expr{Op: "condEq", Condition: "level", Value: 5}, `c.Level == 5`},
		{"condNot string", &schema.Expr{Op: "condNot", Condition: "userRole", Value: "guest"}, `c.UserRole != "guest"`},
		{"condGt number", &schema.Expr{Op: "condGt", Condition: "level", Value: 1}, `c.Level > 1`},
		{"condLt number", &schema.Expr{Op: "condLt", Condition: "level", Value: 10}, `c.Level < 10`},
		{"condIn string", &schema.Expr{Op: "condIn", Condition: "weatherBand", Value: "cold"}, `c.WeatherBand == "cold"`},
		{"condIn string[]", &schema.Expr{Op: "condIn", Condition: "availableStarters", Value: []any{"Cole", "Holmes"}}, `(contains(c.AvailableStarters, "Cole") || contains(c.AvailableStarters, "Holmes"))`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := comp.Compile(tt.expr)
			if err != nil {
				t.Fatalf("Compile() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Compile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompile_ConditionAsFieldOp(t *testing.T) {
	comp := NewExprCompiler(map[string]GoType{}, map[string]GoType{})

	// When condition is set on an eq/neq operator, it should compile as a condition op
	tests := []struct {
		name string
		expr *schema.Expr
		want string
	}{
		{"eq with condition", &schema.Expr{Op: "eq", Condition: "isAdmin", Value: true}, `c.IsAdmin == true`},
		{"neq with condition", &schema.Expr{Op: "neq", Condition: "isAdmin", Value: false}, `c.IsAdmin != false`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := comp.Compile(tt.expr)
			if err != nil {
				t.Fatalf("Compile() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Compile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompile_AndOperator(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"country":  GoStringPtr,
			"currency": GoStringPtr,
			"enabled":  GoBoolPtr,
		},
		map[string]GoType{},
	)

	expr := &schema.Expr{
		Op: "and",
		Exprs: []schema.Expr{
			{Op: "present", Field: "country"},
			{Op: "eq", Field: "country", Value: "US"},
			{Op: "eq", Field: "currency", Value: "USD"},
		},
	}

	got, err := comp.Compile(expr)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	want := `(f.Country != nil && f.Country != nil && *f.Country == "US" && f.Currency != nil && *f.Currency == "USD")`
	if got != want {
		t.Errorf("Compile() = %q, want %q", got, want)
	}
}

func TestCompile_OrOperator(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"role": GoStringPtr,
			"tier": GoStringPtr,
		},
		map[string]GoType{},
	)

	expr := &schema.Expr{
		Op: "or",
		Exprs: []schema.Expr{
			{Op: "eq", Field: "role", Value: "admin"},
			{Op: "eq", Field: "tier", Value: "premium"},
		},
	}

	got, err := comp.Compile(expr)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	want := `(f.Role != nil && *f.Role == "admin" || f.Tier != nil && *f.Tier == "premium")`
	if got != want {
		t.Errorf("Compile() = %q, want %q", got, want)
	}
}

func TestCompile_NotOperator(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"status": GoStringPtr,
			"a":      GoStringPtr,
			"b":      GoStringPtr,
		},
		map[string]GoType{},
	)

	tests := []struct {
		name string
		expr *schema.Expr
		want string
	}{
		{
			name: "not simple",
			expr: &schema.Expr{Op: "not", Exprs: []schema.Expr{{Op: "eq", Field: "status", Value: "disabled"}}},
			want: `!(f.Status != nil && *f.Status == "disabled")`,
		},
		{
			name: "not present",
			expr: &schema.Expr{Op: "not", Exprs: []schema.Expr{{Op: "present", Field: "status"}}},
			want: `!(f.Status != nil)`,
		},
		{
			name: "not and",
			expr: &schema.Expr{
				Op: "not",
				Exprs: []schema.Expr{{
					Op: "and",
					Exprs: []schema.Expr{
						{Op: "eq", Field: "a", Value: "x"},
						{Op: "eq", Field: "b", Value: "y"},
					},
				}},
			},
			want: `!((f.A != nil && *f.A == "x" && f.B != nil && *f.B == "y"))`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := comp.Compile(tt.expr)
			if err != nil {
				t.Fatalf("Compile() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Compile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompile_NestedCombinators(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"a": GoStringPtr,
			"b": GoStringPtr,
			"c": GoStringPtr,
			"d": GoStringPtr,
		},
		map[string]GoType{},
	)

	// (a == "x" && b == "y") || (c == "z" && d == "w")
	expr := &schema.Expr{
		Op: "or",
		Exprs: []schema.Expr{
			{
				Op:   "and",
				Exprs: []schema.Expr{{Op: "eq", Field: "a", Value: "x"}, {Op: "eq", Field: "b", Value: "y"}},
			},
			{
				Op:   "and",
				Exprs: []schema.Expr{{Op: "eq", Field: "c", Value: "z"}, {Op: "eq", Field: "d", Value: "w"}},
			},
		},
	}

	got, err := comp.Compile(expr)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	want := `((f.A != nil && *f.A == "x" && f.B != nil && *f.B == "y") || (f.C != nil && *f.C == "z" && f.D != nil && *f.D == "w"))`
	if got != want {
		t.Errorf("Compile() = %q, want %q", got, want)
	}
}

func TestCompile_ConditionInAndOperator(t *testing.T) {
	comp := NewExprCompiler(map[string]GoType{}, map[string]GoType{})

	// and with condEq and condNot
	expr := &schema.Expr{
		Op: "and",
		Exprs: []schema.Expr{
			{Op: "condEq", Condition: "isAdmin", Value: true},
			{Op: "condNot", Condition: "isBanned", Value: true},
		},
	}

	got, err := comp.Compile(expr)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	want := `(c.IsAdmin == true && c.IsBanned != true)`
	if got != want {
		t.Errorf("Compile() = %q, want %q", got, want)
	}
}

func TestCompile_MixedFieldAndCondition(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"country": GoStringPtr,
		},
		map[string]GoType{},
	)

	// and with field eq and condition eq
	expr := &schema.Expr{
		Op: "and",
		Exprs: []schema.Expr{
			{Op: "eq", Field: "country", Value: "US"},
			{Op: "condEq", Condition: "isAdmin", Value: true},
		},
	}

	got, err := comp.Compile(expr)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	want := `(f.Country != nil && *f.Country == "US" && c.IsAdmin == true)`
	if got != want {
		t.Errorf("Compile() = %q, want %q", got, want)
	}
}

func TestCompile_NilExpr(t *testing.T) {
	comp := NewExprCompiler(map[string]GoType{}, map[string]GoType{})
	_, err := comp.Compile(nil)
	if err == nil {
		t.Fatal("expected error for nil expr, got nil")
	}
}

func TestCompile_UnknownOperator(t *testing.T) {
	comp := NewExprCompiler(map[string]GoType{}, map[string]GoType{})
	got, err := comp.Compile(&schema.Expr{Op: "unknown"})
	if err != nil {
		t.Fatalf("expected no error for unknown op (graceful fallback), got error: %v", err)
	}
	// Should produce a comment fallback, not a blank string
	if got == "" {
		t.Error("expected comment fallback for unknown operator, got empty string")
	}
}

func TestCompile_CamelCaseFieldNames(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"creditCard":  GoStringPtr,
			"bankAccount": GoStringPtr,
		},
		map[string]GoType{},
	)

	tests := []struct {
		name string
		expr *schema.Expr
		want string
	}{
		{
			name: "camelCase field",
			expr: &schema.Expr{Op: "eq", Field: "creditCard", Value: "visa"},
			want: `f.CreditCard != nil && *f.CreditCard == "visa"`,
		},
		{
			name: "camelCase field with condition",
			expr: &schema.Expr{Op: "condEq", Condition: "userRole", Value: "admin"},
			want: `c.UserRole == "admin"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := comp.Compile(tt.expr)
			if err != nil {
				t.Fatalf("Compile() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Compile() = %q, want %q", got, tt.want)
			}
		})
	}
}
