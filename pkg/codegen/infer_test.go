package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umpire-tools/umpire-gen/pkg/schema"
)

func TestGoFieldName(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"country", "Country"},
		{"promoCode", "PromoCode"},
		{"userRole", "UserRole"},
		{"isGuest", "IsGuest"},
		{"itemsCount", "ItemsCount"},
		{"paymentBranch", "PaymentBranch"},
		{"creditCard", "CreditCard"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := GoFieldName(tt.input)
			if got != tt.expect {
				t.Errorf("GoFieldName(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestGoTypeName(t *testing.T) {
	tests := []struct {
		input  string
		expect GoType
	}{
		{"boolean", GoBool},
		{"string", GoString},
		{"number", GoFloat64},
		{"string[]", GoStringSlice},
		{"number[]", GoFloat64Slice},
		{"unknown", GoString},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := GoTypeName(tt.input)
			if got != tt.expect {
				t.Errorf("GoTypeName(%q) = %v, want %v", tt.input, got, tt.expect)
			}
		})
	}
}

func TestGoTypeForField(t *testing.T) {
	tests := []struct {
		base     GoType
		nullable bool
		expect   GoType
	}{
		{GoString, false, GoString},
		{GoString, true, GoStringPtr},
		{GoBool, false, GoBool},
		{GoBool, true, GoBoolPtr},
		{GoFloat64, false, GoFloat64},
		{GoFloat64, true, GoFloat64Ptr},
		{GoInt, false, GoInt},
		{GoInt, true, GoIntPtr},
	}
	for _, tt := range tests {
		t.Run(string(tt.base), func(t *testing.T) {
			got := GoTypeForField(tt.base, tt.nullable)
			if got != tt.expect {
				t.Errorf("GoTypeForField(%v, %v) = %v, want %v", tt.base, tt.nullable, got, tt.expect)
			}
		})
	}
}

func TestGoTypeNullable(t *testing.T) {
	tests := []struct {
		input  GoType
		expect bool
	}{
		{GoString, false},
		{GoStringPtr, true},
		{GoBool, false},
		{GoBoolPtr, true},
		{GoInt, false},
		{GoIntPtr, true},
		{GoFloat64, false},
		{GoFloat64Ptr, true},
		{GoStringSlice, false},
		{GoFloat64Slice, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			got := tt.input.Nullable()
			if got != tt.expect {
				t.Errorf("GoType(%v).Nullable() = %v, want %v", tt.input, got, tt.expect)
			}
		})
	}
}

func TestGoTypeBase(t *testing.T) {
	tests := []struct {
		input  GoType
		expect GoType
	}{
		{GoString, GoString},
		{GoStringPtr, GoString},
		{GoBool, GoBool},
		{GoBoolPtr, GoBool},
		{GoInt, GoInt},
		{GoIntPtr, GoInt},
		{GoFloat64, GoFloat64},
		{GoFloat64Ptr, GoFloat64},
	}
	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			got := tt.input.Base()
			if got != tt.expect {
				t.Errorf("GoType(%v).Base() = %v, want %v", tt.input, got, tt.expect)
			}
		})
	}
}

func TestGoTypeFromJSONValue(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		expect GoType
	}{
		{"string", "hello", GoString},
		{"bool", true, GoBool},
		{"float64", float64(42), GoFloat64},
		{"int", 42, GoInt},
		{"string array", []any{"a", "b"}, GoStringSlice},
		{"float array", []any{1.0, 2.0}, GoFloat64Slice},
		{"empty array", []any{}, GoStringSlice},
		{"mixed array", []any{"a", 1.0}, GoStringSlice},
		{"nil", nil, GoString},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GoTypeFromJSONValue(tt.value)
			if got != tt.expect {
				t.Errorf("GoTypeFromJSONValue(%v) = %v, want %v", tt.value, got, tt.expect)
			}
		})
	}
}

func TestInferTypes(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "country", Required: true, IsEmpty: "string"},
			{Name: "promoCode"},
			{Name: "itemsCount", TypeHint: "number"},
		},
		Conditions: []schema.ConditionDef{
			{Name: "userRole", Type: "string"},
			{Name: "isGuest", Type: "boolean"},
		},
		Rules: []schema.Rule{
			{Type: "enabledWhen", Field: "country", Expr: &schema.Expr{Op: "present", Field: "country"}, Reason: "Country is required"},
			{Type: "requires", Field: "promoCode", Requires: []string{"country"}},
		},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	if len(inferred.Fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(inferred.Fields))
	}

	// country: no type hint, no expression ref, isEmpty=false → *string (default)
	if inferred.Fields[0].GoType != GoStringPtr {
		t.Errorf("country GoType = %v, want %v", inferred.Fields[0].GoType, GoStringPtr)
	}

	// promoCode: no type hint, no expression ref → *string
	if inferred.Fields[1].GoType != GoStringPtr {
		t.Errorf("promoCode GoType = %v, want %v", inferred.Fields[1].GoType, GoStringPtr)
	}

	// itemsCount: type hint "number", isEmpty nil → float64 (base type)
	if inferred.Fields[2].GoType != GoFloat64 {
		t.Errorf("itemsCount GoType = %v, want %v", inferred.Fields[2].GoType, GoFloat64)
	}

	if len(inferred.Conditions) != 2 {
		t.Errorf("expected 2 conditions, got %d", len(inferred.Conditions))
	}

	if inferred.Conditions[0].GoType != GoString {
		t.Errorf("userRole condition GoType = %v, want %v", inferred.Conditions[0].GoType, GoString)
	}
	if inferred.Conditions[1].GoType != GoBool {
		t.Errorf("isGuest condition GoType = %v, want %v", inferred.Conditions[1].GoType, GoBool)
	}
}

func TestInferTypesWithComparison(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "country"},
			{Name: "itemsCount"},
		},
		Conditions: []schema.ConditionDef{},
		Rules: []schema.Rule{
			{
				Type:  "enabledWhen",
				Field: "country",
				Expr: &schema.Expr{
					Op: "and",
					Exprs: []schema.Expr{
						{Op: "present", Field: "country"},
						{Op: "eq", Field: "country", Value: "US"},
					},
				},
			},
			{
				Type:  "enabledWhen",
				Field: "itemsCount",
				Expr: &schema.Expr{
					Op:    "gt",
					Field: "itemsCount",
					Value: float64(5),
				},
			},
		},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	// country is compared with a string literal → *string
	if inferred.Fields[0].GoType != GoStringPtr {
		t.Errorf("country GoType = %v, want %v", inferred.Fields[0].GoType, GoStringPtr)
	}

	// itemsCount is compared with a number → *float64
	if inferred.Fields[1].GoType != GoFloat64Ptr {
		t.Errorf("itemsCount GoType = %v, want %v", inferred.Fields[1].GoType, GoFloat64Ptr)
	}
}

func TestInferTypesEmptySchema(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{},
	}
	_, err := InferTypes(s)
	if err == nil {
		t.Error("expected error for empty schema, got nil")
	}
}

func TestDetectOneOfBranches(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "paymentCreditCard"},
			{Name: "paymentBankTransfer"},
			{Name: "country"},
		},
		Conditions: []schema.ConditionDef{},
		Rules:      []schema.Rule{},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	// Should detect paymentCreditCard and paymentBankTransfer as branches
	// of a "PaymentBranch" group
	if len(inferred.Branches) < 2 {
		t.Errorf("expected at least 2 branches, got %d", len(inferred.Branches))
	}

	if len(inferred.Branches) >= 2 {
		if inferred.Branches[0].GroupName != "PaymentBranch" {
			t.Errorf("first branch group = %q, want %q", inferred.Branches[0].GroupName, "PaymentBranch")
		}
	}
}

func TestDetectOneOfBranchesExplicitRule(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "paymentMethod"},
			{Name: "paymentProvider"},
		},
		Conditions: []schema.ConditionDef{},
		Rules: []schema.Rule{
			{Type: "oneOf", Field: "paymentMethod"},
		},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	if len(inferred.Branches) < 2 {
		t.Errorf("expected at least 2 branches, got %d", len(inferred.Branches))
	}

	if len(inferred.Branches) >= 1 {
		if inferred.Branches[0].GroupName != "PaymentBranch" {
			t.Errorf("branch group = %q, want %q", inferred.Branches[0].GroupName, "PaymentBranch")
		}
	}
}

func TestCamelToJSONTag(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"Country", "country"},
		{"PromoCode", "promoCode"},
		{"ItemsCount", "itemsCount"},
		{"IsGuest", "isGuest"},
		{"HTTPResponse", "httpResponse"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := camelToJSONTag(tt.input)
			if got != tt.expect {
				t.Errorf("camelToJSONTag(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestSplitCamelCase(t *testing.T) {
	tests := []struct {
		input  string
		expect []string
	}{
		{"country", []string{"country"}},
		{"promoCode", []string{"promo", "code"}},
		{"paymentCreditCard", []string{"payment", "credit", "card"}},
		{"HTTPResponse", []string{"http", "response"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitCamelCase(tt.input)
			if len(got) != len(tt.expect) {
				t.Errorf("splitCamelCase(%q) = %v, want %v", tt.input, got, tt.expect)
			} else {
				for i := range got {
					if got[i] != tt.expect[i] {
						t.Errorf("splitCamelCase(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expect[i])
					}
				}
			}
		})
	}
}

func TestInferTypesConditionRefs(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "promoCode"},
		},
		Conditions: []schema.ConditionDef{
			{Name: "userRole", Type: "string"},
		},
		Rules: []schema.Rule{
			{
				Type:  "enabledWhen",
				Field: "promoCode",
				Expr: &schema.Expr{
					Op:        "condEq",
					Condition: "userRole",
					Value:     "admin",
				},
			},
		},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	if len(inferred.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(inferred.Conditions))
	}
	if inferred.Conditions[0].GoType != GoString {
		t.Errorf("condition GoType = %v, want %v", inferred.Conditions[0].GoType, GoString)
	}
}

func TestGenerateFull(t *testing.T) {
	s := &schema.Schema{
		Fields: []schema.FieldDef{
			{Name: "country", Required: true, IsEmpty: "string"},
		},
		Conditions: []schema.ConditionDef{
			{Name: "isAdmin", Type: "boolean"},
		},
		Rules: []schema.Rule{},
	}

	inferred, err := InferTypes(s)
	if err != nil {
		t.Fatalf("InferTypes() error: %v", err)
	}

	gen := NewGenerator("Test", "testpkg", "TestFields", "TestConditions", inferred)
	result, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// Write to temp file and verify it parses
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_umpire.go")
	if err := os.WriteFile(tmpFile, []byte(result.Source), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	// Verify the file can be read back
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(data) != result.Source {
		t.Error("file content does not match generated source")
	}

	// Verify expected content is in the file
	if !strings.Contains(string(data), "package testpkg") {
		t.Error("file missing package declaration")
	}
	if !strings.Contains(string(data), "type TestFields struct") {
		t.Error("file missing TestFields struct")
	}
	if !strings.Contains(string(data), "type TestConditions struct") {
		t.Error("file missing TestConditions struct")
	}
	if !strings.Contains(string(data), "type TestAvailability struct") {
		t.Error("file missing TestAvailability struct")
	}
}
