package codegen

import (
	"fmt"
	"strings"

	"github.com/umpire-tools/umpire-gen/pkg/schema"
)

// ExprCompiler compiles JsonExpr AST nodes to inline Go boolean expression strings.
type ExprCompiler struct {
	fieldTypes  map[string]GoType
	condTypes   map[string]GoType
	condPrefix  string // "c." for conditions
	fieldPrefix string // "f." for fields
}

// NewExprCompiler creates a new ExprCompiler with the given field and condition type maps.
func NewExprCompiler(fieldTypes, condTypes map[string]GoType) *ExprCompiler {
	return &ExprCompiler{
		fieldTypes:  fieldTypes,
		condTypes:   condTypes,
		condPrefix:  "c.",
		fieldPrefix: "f.",
	}
}

// GoFieldNameSafe returns the Go-safe field name for a JSON field name.
func (c *ExprCompiler) GoFieldNameSafe(name string) string {
	return GoFieldName(name)
}

// Compile converts a schema.Expr to a Go boolean expression string.
func (c *ExprCompiler) Compile(e *schema.Expr) (string, error) {
	if e == nil {
		return "", fmt.Errorf("nil expression")
	}
	return c.compileExpr(e), nil
}

// compileExpr is the main dispatch for expression compilation.
func (c *ExprCompiler) compileExpr(e *schema.Expr) string {
	switch e.Op {
	case "and":
		return c.compileAnd(e)
	case "or":
		return c.compileOr(e)
	case "not":
		return c.compileNot(e)
	case "eq":
		return c.compileEq(e)
	case "neq":
		return c.compileNeq(e)
	case "gt":
		return c.compileGt(e)
	case "gte":
		return c.compileGte(e)
	case "lt":
		return c.compileLt(e)
	case "lte":
		return c.compileLte(e)
	case "in":
		return c.compileIn(e)
	case "present":
		return c.compilePresent(e)
	case "absent":
		return c.compileAbsent(e)
	case "truthy":
		return c.compileTruthy(e)
	case "falsy":
		return c.compileFalsy(e)
	case "cond":
		return c.compileCond(e)
	case "condEq":
		return c.compileCondEq(e)
	case "condNot":
		return c.compileCondNot(e)
	case "condGt":
		return c.compileCondGt(e)
	case "condLt":
		return c.compileCondLt(e)
	case "condIn":
		return c.compileCondIn(e)
	case "notIn":
		return c.compileNotIn(e)
	case "check":
		return c.compileCheck(e)
	case "email":
		return c.compileCheckEmail(e)
	case "minLength", "maxLength", "matches", "url", "integer", "max", "min", "range":
		return c.compileCheck(e)
	case "fieldInCond":
		// fieldInCond checks if a field value is in an array condition
		condName := GoFieldName(e.Condition)
		fieldName := c.GoFieldNameSafe(e.Field)
		goType := c.fieldTypes[e.Field]
		if goType.Nullable() {
			return fmt.Sprintf("f.%s != nil && contains(c.%s, *f.%s)", fieldName, condName, fieldName)
		}
		return fmt.Sprintf("contains(c.%s, f.%s)", condName, fieldName)
	default:
		return fmt.Sprintf("/* unknown op: %s */", e.Op)
	}
}

// compileAnd combines sub-expressions with &&.
func (c *ExprCompiler) compileAnd(e *schema.Expr) string {
	var parts []string
	for _, sub := range e.Exprs {
		parts = append(parts, c.compileExpr(&sub))
	}
	return "(" + strings.Join(parts, " && ") + ")"
}

// compileOr combines sub-expressions with ||.
func (c *ExprCompiler) compileOr(e *schema.Expr) string {
	var parts []string
	for _, sub := range e.Exprs {
		parts = append(parts, c.compileExpr(&sub))
	}
	return "(" + strings.Join(parts, " || ") + ")"
}

// compileNot negates a sub-expression with !.
func (c *ExprCompiler) compileNot(e *schema.Expr) string {
	if len(e.Exprs) == 0 {
		return ""
	}
	return "!(" + c.compileExpr(&e.Exprs[0]) + ")"
}

// compileEq emits a field or condition equality comparison.
func (c *ExprCompiler) compileEq(e *schema.Expr) string {
	if e.Condition != "" {
		return c.compileCondEq(e)
	}
	return c.compileFieldOp(e, "==")
}

// compileNeq emits a field or condition inequality comparison.
func (c *ExprCompiler) compileNeq(e *schema.Expr) string {
	if e.Condition != "" {
		return c.compileCondNot(e)
	}
	return c.compileFieldOp(e, "!=")
}

// compileGt emits a field greater-than comparison.
func (c *ExprCompiler) compileGt(e *schema.Expr) string {
	return c.compileFieldOp(e, ">")
}

// compileGte emits a field greater-than-or-equal comparison.
func (c *ExprCompiler) compileGte(e *schema.Expr) string {
	return c.compileFieldOp(e, ">=")
}

// compileLt emits a field less-than comparison.
func (c *ExprCompiler) compileLt(e *schema.Expr) string {
	return c.compileFieldOp(e, "<")
}

// compileLte emits a field less-than-or-equal comparison.
func (c *ExprCompiler) compileLte(e *schema.Expr) string {
	return c.compileFieldOp(e, "<=")
}

// compileIn emits a field membership check (field value is in a slice).
func (c *ExprCompiler) compileIn(e *schema.Expr) string {
	return c.compileFieldOp(e, "in")
}

// compilePresent checks if a field is non-nil/non-empty.
func (c *ExprCompiler) compilePresent(e *schema.Expr) string {
	fieldName := c.GoFieldNameSafe(e.Field)
	goType := c.fieldTypes[e.Field]
	if goType.Nullable() {
		return fmt.Sprintf("f.%s != nil", fieldName)
	}
	// For string fields, present means non-empty
	if goType == GoString {
		return fmt.Sprintf("f.%s != \"\"", fieldName)
	}
	// For slice/map fields, present means non-empty
	if goType == GoStringSlice || goType == GoFloat64Slice || goType == GoMap {
		return fmt.Sprintf("len(f.%s) > 0", fieldName)
	}
	// Non-pointer bool/int/float are always "present" in Go; return true.
	return "true"
}

// compileAbsent checks if a field is nil.
func (c *ExprCompiler) compileAbsent(e *schema.Expr) string {
	fieldName := c.GoFieldNameSafe(e.Field)
	goType := c.fieldTypes[e.Field]
	if goType.Nullable() {
		return fmt.Sprintf("f.%s == nil", fieldName)
	}
	// For string fields, absent means empty
	if goType == GoString {
		return fmt.Sprintf("f.%s == \"\"", fieldName)
	}
	// For slice/map fields, absent means empty
	if goType == GoStringSlice || goType == GoFloat64Slice || goType == GoMap {
		return fmt.Sprintf("len(f.%s) == 0", fieldName)
	}
	// Non-pointer bool/int/float can never be absent; return false.
	return "false"
}

// compileTruthy checks if a field value is truthy.
func (c *ExprCompiler) compileTruthy(e *schema.Expr) string {
	fieldName := c.GoFieldNameSafe(e.Field)
	goType := c.fieldTypes[e.Field]
	if goType.Nullable() {
		return fmt.Sprintf("f.%s != nil && *f.%s", fieldName, fieldName)
	}
	switch goType {
	case GoBool:
		return fmt.Sprintf("f.%s", fieldName)
	case GoString:
		return fmt.Sprintf("f.%s != \"\"", fieldName)
	default:
		return fmt.Sprintf("f.%s != 0", fieldName)
	}
}

// compileFalsy checks if a field value is falsy.
func (c *ExprCompiler) compileFalsy(e *schema.Expr) string {
	fieldName := c.GoFieldNameSafe(e.Field)
	goType := c.fieldTypes[e.Field]
	if goType.Nullable() {
		return fmt.Sprintf("f.%s == nil || *f.%s == false", fieldName, fieldName)
	}
	switch goType {
	case GoBool:
		return fmt.Sprintf("!f.%s", fieldName)
	case GoString:
		return fmt.Sprintf("f.%s == \"\"", fieldName)
	default:
		return fmt.Sprintf("f.%s == 0", fieldName)
	}
}

// compileCond checks if a condition is truthy.
func (c *ExprCompiler) compileCond(e *schema.Expr) string {
	condName := GoFieldName(e.Condition)
	goType := c.condTypes[e.Condition]
	if goType == GoBool {
		return fmt.Sprintf("c.%s", condName)
	}
	return fmt.Sprintf("c.%s != nil", condName)
}

// compileCondEq emits a condition equality comparison: c.Field == value.
func (c *ExprCompiler) compileCondEq(e *schema.Expr) string {
	condName := GoFieldName(e.Condition)
	val := formatValue(e.Value)
	goType := c.condTypes[e.Condition]
	if goType.Nullable() {
		return fmt.Sprintf("c.%s != nil && *c.%s == %s", condName, condName, val)
	}
	return fmt.Sprintf("c.%s == %s", condName, val)
}

// compileCondNot emits a condition inequality: c.Field != value.
func (c *ExprCompiler) compileCondNot(e *schema.Expr) string {
	condName := GoFieldName(e.Condition)
	val := formatValue(e.Value)
	goType := c.condTypes[e.Condition]
	if goType.Nullable() {
		return fmt.Sprintf("c.%s != nil && *c.%s != %s", condName, condName, val)
	}
	return fmt.Sprintf("c.%s != %s", condName, val)
}

// compileCondGt emits a condition greater-than comparison.
func (c *ExprCompiler) compileCondGt(e *schema.Expr) string {
	condName := GoFieldName(e.Condition)
	val := formatValue(e.Value)
	goType := c.condTypes[e.Condition]
	if goType.Nullable() {
		return fmt.Sprintf("c.%s != nil && *c.%s > %s", condName, condName, val)
	}
	return fmt.Sprintf("c.%s > %s", condName, val)
}

// compileCondLt emits a condition less-than comparison.
func (c *ExprCompiler) compileCondLt(e *schema.Expr) string {
	condName := GoFieldName(e.Condition)
	val := formatValue(e.Value)
	goType := c.condTypes[e.Condition]
	if goType.Nullable() {
		return fmt.Sprintf("c.%s != nil && *c.%s < %s", condName, condName, val)
	}
	return fmt.Sprintf("c.%s < %s", condName, val)
}

// compileCondIn emits a condition "in" check (membership in a set of values).
func (c *ExprCompiler) compileCondIn(e *schema.Expr) string {
	condName := GoFieldName(e.Condition)
	goType := c.condTypes[e.Condition]

	// Extract values to check against
	var vals []any
	if v, ok := e.Value.([]any); ok {
		vals = v
	} else if e.Value != nil {
		vals = []any{e.Value}
	}

	// For slice conditions (string[], number[]) use contains()
	if goType == GoStringSlice || goType == GoFloat64Slice {
		var parts []string
		for _, v := range vals {
			parts = append(parts, fmt.Sprintf("contains(c.%s, %s)", condName, formatValue(v)))
		}
		if len(parts) == 1 {
			return parts[0]
		}
		return "(" + strings.Join(parts, " || ") + ")"
	}

	// For scalar conditions (string, number, bool) use equality with ||
	var parts []string
	for _, v := range vals {
		val := formatValue(v)
		if goType.Nullable() {
			parts = append(parts, fmt.Sprintf("*c.%s == %s", condName, val))
		} else {
			parts = append(parts, fmt.Sprintf("c.%s == %s", condName, val))
		}
	}
	if len(parts) == 1 {
		if goType.Nullable() {
			return fmt.Sprintf("c.%s != nil && %s", condName, parts[0])
		}
		return parts[0]
	}
	if goType.Nullable() {
		return fmt.Sprintf("c.%s != nil && (%s)", condName, strings.Join(parts, " || "))
	}
	return "(" + strings.Join(parts, " || ") + ")"
}

// compileNotIn emits a field/condition "not in" check: value is NOT in a slice.
func (c *ExprCompiler) compileNotIn(e *schema.Expr) string {
	if e.Condition != "" {
		condName := GoFieldName(e.Condition)
		goType := c.condTypes[e.Condition]
		if vals, ok := e.Value.([]any); ok && len(vals) > 0 {
			var parts []string
			for _, v := range vals {
				val := formatValue(v)
				if goType.Nullable() {
					parts = append(parts, fmt.Sprintf("*c.%s != %s", condName, val))
				} else {
					parts = append(parts, fmt.Sprintf("c.%s != %s", condName, val))
				}
			}
			return "(" + strings.Join(parts, " && ") + ")"
		}
		val := formatValue(e.Value)
		if goType.Nullable() {
			return fmt.Sprintf("c.%s != nil && *c.%s != %s", condName, condName, val)
		}
		return fmt.Sprintf("c.%s != %s", condName, val)
	}
	fieldName := c.GoFieldNameSafe(e.Field)
	goType := c.fieldTypes[e.Field]
	if vals, ok := e.Value.([]any); ok && len(vals) > 0 {
		var parts []string
		for _, v := range vals {
			val := formatValue(v)
			if goType.Nullable() {
				parts = append(parts, fmt.Sprintf("*f.%s != %s", fieldName, val))
			} else {
				parts = append(parts, fmt.Sprintf("f.%s != %s", fieldName, val))
			}
		}
		return "(" + strings.Join(parts, " && ") + ")"
	}
	val := formatValue(e.Value)
	if goType.Nullable() {
		return fmt.Sprintf("f.%s != nil && *f.%s != %s", fieldName, fieldName, val)
	}
	return fmt.Sprintf("f.%s != %s", fieldName, val)
}

// compileFieldOp emits a field comparison: f.Field <op> value (with nil guard for pointers).
func (c *ExprCompiler) compileFieldOp(e *schema.Expr, op string) string {
	fieldName := c.GoFieldNameSafe(e.Field)
	goType := c.fieldTypes[e.Field]

	if op == "in" {
		// For "in" operator: check if field value equals one of the allowed values
		if vals, ok := e.Value.([]any); ok && len(vals) > 0 {
			var parts []string
			for _, v := range vals {
				val := formatValue(v)
				if goType.Nullable() {
					parts = append(parts, fmt.Sprintf("f.%s != nil && *f.%s == %s", fieldName, fieldName, val))
				} else {
					parts = append(parts, fmt.Sprintf("f.%s == %s", fieldName, val))
				}
			}
			return "(" + strings.Join(parts, " || ") + ")"
		}
		val := formatValue(e.Value)
		// For slice fields with a single value, check membership in the slice.
		if goType == GoStringSlice || goType == GoFloat64Slice {
			return fmt.Sprintf("contains(f.%s, %s)", fieldName, val)
		}
		if goType.Nullable() {
			return fmt.Sprintf("f.%s != nil && *f.%s == %s", fieldName, fieldName, val)
		}
		return fmt.Sprintf("f.%s == %s", fieldName, val)
	}

	if goType.Nullable() {
		val := formatValue(e.Value)
		return fmt.Sprintf("f.%s != nil && *f.%s %s %s", fieldName, fieldName, op, val)
	}

	val := formatValue(e.Value)
	return fmt.Sprintf("f.%s %s %s", fieldName, op, val)
}

// formatValue formats a JSON value into a Go literal string.
func formatValue(v any) string {
	if v == nil {
		return "nil"
	}
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%q", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case int:
		return fmt.Sprintf("%d", val)
	case []any:
		var parts []string
		for _, item := range val {
			parts = append(parts, formatValue(item))
		}
		return fmt.Sprintf("[]string{%s}", strings.Join(parts, ", "))
	default:
		return fmt.Sprintf("%v", val)
	}
}

// isNumericType reports whether the Go type is a numeric type.
func isNumericType(t GoType) bool {
	return t == GoFloat64 || t == GoInt || t == GoFloat64Ptr || t == GoIntPtr
}

// extractRangeMin extracts the minimum value from a range string like "[1, 10]".
func extractRangeMin(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimPrefix(s, "(")
	parts := strings.Split(s, ",")
	if len(parts) < 1 {
		return "0"
	}
	return strings.TrimSpace(parts[0])
}

// extractRangeMax extracts the maximum value from a range string like "[1, 10]".
func extractRangeMax(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "]")
	s = strings.TrimSuffix(s, ")")
	parts := strings.Split(s, ",")
	if len(parts) < 2 {
		return "0"
	}
	return strings.TrimSpace(parts[1])
}

// compileCheck compiles a check expression into a Go boolean expression.
// The check expression has Op="check" and the actual validation op is in Exprs[0].
func (c *ExprCompiler) compileCheck(e *schema.Expr) string {
	fieldName := c.GoFieldNameSafe(e.Field)
	goType := c.fieldTypes[e.Field]

	checkOp := e.Op
	val := formatValue(e.Value)

	// Check for nested check op in Exprs[0] (from fixture conversion)
	if len(e.Exprs) > 0 {
		checkOp = e.Exprs[0].Op
		val = formatValue(e.Exprs[0].Value)
	}

	// For numeric types, use direct comparison
	if isNumericType(goType.Base()) {
		fieldRef := fmt.Sprintf("f.%s", fieldName)
		if goType.Nullable() {
			fieldRef = fmt.Sprintf("*f.%s", fieldName)
		}
		switch checkOp {
		case "min":
			return fmt.Sprintf("f.%s != nil && %s >= %s", fieldName, fieldRef, val)
		case "max":
			return fmt.Sprintf("f.%s != nil && %s <= %s", fieldName, fieldRef, val)
		case "range":
			// Extract min/max from the range value
			var minVal, maxVal string
			if m, ok := e.Value.(map[string]float64); ok {
				minVal = fmt.Sprintf("%g", m["min"])
				maxVal = fmt.Sprintf("%g", m["max"])
			} else {
				// Fallback: try to parse as a string
				rawVal := strings.TrimPrefix(val, "\"")
				rawVal = strings.TrimSuffix(rawVal, "\"")
				minVal = extractRangeMin(rawVal)
				maxVal = extractRangeMax(rawVal)
			}
			return fmt.Sprintf("f.%s != nil && %s >= %s && %s <= %s", fieldName, fieldRef, minVal, fieldRef, maxVal)
		case "integer":
			return fmt.Sprintf("f.%s != nil && %s == float64(int64(%s))", fieldName, fieldRef, fieldRef)
		default:
			return "true"
		}
	}

	// For slice types
	if goType == GoStringSlice || goType == GoFloat64Slice {
		switch checkOp {
		case "minLength":
			return fmt.Sprintf("len(f.%s) >= %s", fieldName, val)
		case "maxLength":
			return fmt.Sprintf("len(f.%s) <= %s", fieldName, val)
		default:
			return "true"
		}
	}

	// For string types (pointer and non-pointer)
	if goType.Nullable() {
		switch checkOp {
		case "email":
			return fmt.Sprintf("f.%s != nil && isValidEmail(*f.%s)", fieldName, fieldName)
		case "url":
			return fmt.Sprintf("f.%s != nil && isValidURL(*f.%s)", fieldName, fieldName)
		case "minLength":
			return fmt.Sprintf("f.%s != nil && len(*f.%s) >= %s", fieldName, fieldName, val)
		case "maxLength":
			return fmt.Sprintf("f.%s != nil && len(*f.%s) <= %s", fieldName, fieldName, val)
		case "matches":
			return fmt.Sprintf("f.%s != nil && isValidRegexMatch(*f.%s, %s)", fieldName, fieldName, val)
		case "integer":
			return fmt.Sprintf("f.%s != nil && isValidInteger(*f.%s)", fieldName, fieldName)
		case "max":
			return fmt.Sprintf("f.%s != nil && isValidNumber(*f.%s) && parseFloat(*f.%s) <= %s", fieldName, fieldName, fieldName, val)
		case "min":
			return fmt.Sprintf("f.%s != nil && isValidNumber(*f.%s) && parseFloat(*f.%s) >= %s", fieldName, fieldName, fieldName, val)
		case "range":
			return fmt.Sprintf("f.%s != nil && isValidNumber(*f.%s) && isInRange(*f.%s, %s)", fieldName, fieldName, fieldName, val)
		default:
			return "true"
		}
	}

	switch checkOp {
	case "email":
		return fmt.Sprintf("isValidEmail(f.%s)", fieldName)
	case "url":
		return fmt.Sprintf("isValidURL(f.%s)", fieldName)
	case "minLength":
		return fmt.Sprintf("len(f.%s) >= %s", fieldName, val)
	case "maxLength":
		return fmt.Sprintf("len(f.%s) <= %s", fieldName, val)
	case "matches":
		return fmt.Sprintf("isValidRegexMatch(f.%s, %s)", fieldName, val)
	case "integer":
		return fmt.Sprintf("isValidInteger(f.%s)", fieldName)
	case "max":
		return fmt.Sprintf("isValidNumber(f.%s) && parseFloat(f.%s) <= %s", fieldName, fieldName, val)
	case "min":
		return fmt.Sprintf("isValidNumber(f.%s) && parseFloat(f.%s) >= %s", fieldName, fieldName, val)
	case "range":
		return fmt.Sprintf("isValidNumber(f.%s) && isInRange(f.%s, %s)", fieldName, fieldName, val)
	default:
		return "true"
	}
}

// compileCheckEmail compiles an email validation check.
func (c *ExprCompiler) compileCheckEmail(e *schema.Expr) string {
	fieldName := c.GoFieldNameSafe(e.Field)
	goType := c.fieldTypes[e.Field]

	if goType.Nullable() {
		return fmt.Sprintf("f.%s == nil || *f.%s == \"\" || isValidEmail(*f.%s)", fieldName, fieldName, fieldName)
	}
	return fmt.Sprintf("f.%s == \"\" || isValidEmail(f.%s)", fieldName, fieldName)
}
