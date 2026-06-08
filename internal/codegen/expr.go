package codegen

import (
	"fmt"
	"strings"

	"github.com/umpire-tools/umpire-gen/internal/schema"
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
	case "check":
		return c.compileCheck(e)
	case "email":
		return c.compileCheckEmail(e)
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
	// Non-pointer fields are always "present" in Go; return true.
	return "true"
}

// compileAbsent checks if a field is nil.
func (c *ExprCompiler) compileAbsent(e *schema.Expr) string {
	fieldName := c.GoFieldNameSafe(e.Field)
	goType := c.fieldTypes[e.Field]
	if goType.Nullable() {
		return fmt.Sprintf("f.%s == nil", fieldName)
	}
	// Non-pointer fields can never be absent; return false.
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

// compileCondIn emits a condition "in" check.
func (c *ExprCompiler) compileCondIn(e *schema.Expr) string {
	condName := GoFieldName(e.Condition)
	val := formatValue(e.Value)
	goType := c.condTypes[e.Condition]
	if goType.Nullable() {
		return fmt.Sprintf("c.%s != nil && contains(c.%s, %s)", condName, condName, val)
	}
	return fmt.Sprintf("contains(c.%s, %s)", condName, val)
}

// compileFieldOp emits a field comparison: f.Field <op> value (with nil guard for pointers).
func (c *ExprCompiler) compileFieldOp(e *schema.Expr, op string) string {
	fieldName := c.GoFieldNameSafe(e.Field)
	goType := c.fieldTypes[e.Field]

	if op == "in" {
		// For "in" operator, check if field value is contained in a slice
		val := formatValue(e.Value)
		if goType.Nullable() {
			return fmt.Sprintf("f.%s != nil && contains(f.%s, *f.%s)", fieldName, fieldName, fieldName)
		}
		return fmt.Sprintf("contains(f.%s, %s)", fieldName, val)
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

// compileCheck compiles a check expression into a Go boolean expression.
// The check expression has a "check" sub-expression with an op like "email", "minLength", "matches", etc.
func (c *ExprCompiler) compileCheck(e *schema.Expr) string {
	if len(e.Exprs) == 0 {
		return "/* missing check expression */"
	}
	checkExpr := &e.Exprs[0]
	checkOp := checkExpr.Op

	fieldName := c.GoFieldNameSafe(e.Field)
	goType := c.fieldTypes[e.Field]

	switch checkOp {
	case "email":
		if goType.Nullable() {
			return fmt.Sprintf("f.%s == nil || *f.%s == \"\" || isValidEmail(*f.%s)", fieldName, fieldName, fieldName)
		}
		return fmt.Sprintf("f.%s == \"\" || isValidEmail(f.%s)", fieldName, fieldName)

	case "minLength":
		minVal, _ := checkExpr.Value.(float64)
		if minVal == 0 {
			minVal = 1
		}
		if goType.Nullable() {
			return fmt.Sprintf("f.%s == nil || *f.%s == \"\" || len(*f.%s) >= %g", fieldName, fieldName, fieldName, minVal)
		}
		return fmt.Sprintf("f.%s == \"\" || len(f.%s) >= %g", fieldName, fieldName, minVal)

	case "maxLength":
		maxVal, _ := checkExpr.Value.(float64)
		if goType.Nullable() {
			return fmt.Sprintf("f.%s == nil || *f.%s == \"\" || len(*f.%s) <= %g", fieldName, fieldName, fieldName, maxVal)
		}
		return fmt.Sprintf("f.%s == \"\" || len(f.%s) <= %g", fieldName, fieldName, maxVal)

	case "matches":
		pattern, _ := checkExpr.Value.(string)
		if pattern == "" {
			return "/* missing pattern */"
		}
		if goType.Nullable() {
			return fmt.Sprintf("f.%s == nil || *f.%s == \"\" || isValidRegexPattern(%q, *f.%s)", fieldName, fieldName, pattern, fieldName)
		}
		return fmt.Sprintf("f.%s == \"\" || isValidRegexPattern(%q, f.%s)", fieldName, pattern, fieldName)

	case "gt":
		val := formatValue(checkExpr.Value)
		if goType.Nullable() {
			return fmt.Sprintf("f.%s == nil || *f.%s > %s", fieldName, fieldName, val)
		}
		return fmt.Sprintf("f.%s > %s", fieldName, val)

	case "gte":
		val := formatValue(checkExpr.Value)
		if goType.Nullable() {
			return fmt.Sprintf("f.%s == nil || *f.%s >= %s", fieldName, fieldName, val)
		}
		return fmt.Sprintf("f.%s >= %s", fieldName, val)

	case "lt":
		val := formatValue(checkExpr.Value)
		if goType.Nullable() {
			return fmt.Sprintf("f.%s == nil || *f.%s < %s", fieldName, fieldName, val)
		}
		return fmt.Sprintf("f.%s < %s", fieldName, val)

	case "lte":
		val := formatValue(checkExpr.Value)
		if goType.Nullable() {
			return fmt.Sprintf("f.%s == nil || *f.%s <= %s", fieldName, fieldName, val)
		}
		return fmt.Sprintf("f.%s <= %s", fieldName, val)

	default:
		return fmt.Sprintf("/* unknown check: %s */", checkOp)
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
