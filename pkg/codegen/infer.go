package codegen

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
)

// FieldTypeInfo holds the inferred type info for a schema field.
type FieldTypeInfo struct {
	Name    string
	GoType  GoType
	JSONTag string
	// UnderlyingType records scalar semantics when GoType is a generated named
	// type (for example *WorkflowType with underlying string). It is empty for
	// ordinary inferred types, where GoType itself supplies the semantics.
	UnderlyingType GoType
}

// SemanticBase returns the primitive/container type used for emptiness checks.
func (f FieldTypeInfo) SemanticBase() GoType {
	if f.UnderlyingType != "" {
		return f.UnderlyingType.Base()
	}
	return f.GoType.Base()
}

// ConditionTypeInfo holds the type info for a schema condition.
type ConditionTypeInfo struct {
	Name    string
	GoType  GoType
	JSONTag string
}

// OneOfBranch represents a branch of a oneOf or eitherOf group.
type OneOfBranch struct {
	GroupName    string // e.g. "PaymentBranch"
	Branch       string // e.g. "CreditCard" (Go/PascalCase field name)
	OriginalName string // e.g. "creditCard" (original JSON field name, used for reason text)
	Index        int
	Expression   *schema.Expr // combined condition that activates this branch (from when clauses)
	IsOneOf      bool         // true for oneOf (mutually exclusive), false for eitherOf
	RuleType     string       // rule type that produced the branch expression: "enabledWhen" or "fairWhen"
}

// InferredSchema holds all inferred type information from a schema.
type InferredSchema struct {
	Fields     []FieldTypeInfo
	Conditions []ConditionTypeInfo
	Branches   []OneOfBranch
}

// InferTypes traverses a schema and infers types for fields, conditions, and oneOf/eitherOf branches.
func InferTypes(s *schema.Schema) (*InferredSchema, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}

	// Collect all field names for validation
	fieldNames := make(map[string]bool)
	for _, f := range s.Fields {
		fieldNames[f.Name] = false // false = has not been referenced in an expression
	}

	// Collect condition names
	conditionNames := make(map[string]bool)
	for _, c := range s.Conditions {
		conditionNames[c.Name] = false
	}

	// First pass: collect field/condition references from all expressions
	for _, rule := range s.Rules {
		if rule.Expr != nil {
			traverseExprForRefs(rule.Expr, fieldNames, conditionNames)
		}
		if rule.DisabledWhen != nil {
			traverseExprForRefs(rule.DisabledWhen, fieldNames, conditionNames)
		}
		if rule.FairWhen != nil {
			traverseExprForRefs(rule.FairWhen, fieldNames, conditionNames)
		}
		if rule.Check != nil {
			traverseExprForRefs(rule.Check, fieldNames, conditionNames)
		}
	}

	// Build field type info
	inferred := &InferredSchema{}
	for _, f := range s.Fields {
		ti := FieldTypeInfo{
			Name:    f.Name,
			JSONTag: f.Name,
		}

		uses := fieldUses(s, f.Name)
		// Use explicit type hint if present
		if f.TypeHint != "" {
			ti.GoType = GoTypeName(f.TypeHint)
		} else if f.IsEmpty == "array" {
			ti.GoType = arrayFieldType(uses)
		} else if f.IsEmpty != "" {
			// Use isEmpty to infer the type
			ti.GoType = GoTypeForField(GoTypeName(f.IsEmpty), true)
			if f.IsEmpty == "present" && holdsJSONValue(s, uses, ti.GoType) {
				ti.GoType = GoAny
			}
		} else if validatorType, ok := inferNamedValidatorType(f.Name, s); ok {
			ti.GoType = validatorType
		} else if checkType, ok := inferCheckRuleType(f.Name, s); ok {
			ti.GoType = checkType
		} else {
			// Infer from expression usage, default to *string if ambiguous
			ti.GoType = inferFieldTypeFromUsage(f.Name, s, fieldNames)
			if holdsJSONValue(s, uses, ti.GoType) {
				ti.GoType = GoAny
			}
		}
		if ti.GoType == GoAny {
			if err := checkAnyFieldOps(s, f.Name, uses); err != nil {
				return nil, err
			}
		}

		inferred.Fields = append(inferred.Fields, ti)
	}

	// Build condition type info
	for _, c := range s.Conditions {
		ti := ConditionTypeInfo{
			Name:    c.Name,
			GoType:  GoTypeName(c.Type),
			JSONTag: c.Name,
		}
		inferred.Conditions = append(inferred.Conditions, ti)
	}

	// Detect oneOf/eitherOf branches from field names
	branches := detectOneOfBranches(s)
	inferred.Branches = branches

	return inferred, nil
}

// traverseExprForRefs walks an expression tree, marking field/condition names as referenced.
func traverseExprForRefs(e *schema.Expr, fieldRefs, condRefs map[string]bool) {
	if e == nil {
		return
	}
	if e.Field != "" {
		fieldRefs[e.Field] = true
	}
	if e.Condition != "" {
		condRefs[e.Condition] = true
	}
	for _, child := range e.Exprs {
		traverseExprForRefs(&child, fieldRefs, condRefs)
	}
}

func inferNamedValidatorType(fieldName string, s *schema.Schema) (GoType, bool) {
	validator, ok := s.Validators[fieldName]
	if !ok {
		return "", false
	}

	switch validator.Op {
	case "min", "max", "range", "integer":
		return GoFloat64Ptr, true
	case "email", "url", "matches", "minLength", "maxLength":
		return GoStringPtr, true
	default:
		return "", false
	}
}

// inferCheckRuleType types a field from the validation operator of a check rule.
func inferCheckRuleType(fieldName string, s *schema.Schema) (GoType, bool) {
	for _, rule := range s.Rules {
		if rule.Type == "check" && rule.Field == fieldName && rule.Check != nil {
			checkOp := rule.Check.Op
			if len(rule.Check.Exprs) > 0 {
				checkOp = rule.Check.Exprs[0].Op
			}
			switch checkOp {
			case "minLength", "maxLength":
				return GoStringSlice, true // arrays
			case "min", "max", "range", "integer":
				return GoFloat64Ptr, true // numbers
			}
		}
	}
	return "", false
}

// anyFieldOps lists the expression ops that compile against a GoAny field.
var anyFieldOps = map[string]bool{
	"eq": true, "neq": true, "gt": true, "gte": true, "lt": true, "lte": true,
	"in": true, "notIn": true, "contains": true, "fieldInCond": true,
	"present": true, "absent": true, "truthy": true, "falsy": true,
}

// checkAnyFieldOps rejects a GoAny field that an expression or a named
// validator uses with an op that only compiles against a concrete type.
func checkAnyFieldOps(s *schema.Schema, fieldName string, uses []*schema.Expr) error {
	for _, e := range uses {
		if !anyFieldOps[e.Op] {
			return fmt.Errorf("field %q can hold any JSON value, so %q cannot be generated for it; declare its isEmpty", fieldName, e.Op)
		}
	}
	if v, ok := s.Validators[fieldName]; ok {
		return fmt.Errorf("field %q can hold any JSON value, so validator %q cannot be generated for it; declare its isEmpty", fieldName, v.Op)
	}
	return nil
}

// fieldUses returns every expression node that targets the field, from rule
// expressions and anyOf/eitherOf branch expressions.
func fieldUses(s *schema.Schema, fieldName string) []*schema.Expr {
	var uses []*schema.Expr
	var walk func(e *schema.Expr)
	walk = func(e *schema.Expr) {
		if e == nil {
			return
		}
		if e.Field == fieldName {
			uses = append(uses, e)
		}
		for i := range e.Exprs {
			walk(&e.Exprs[i])
		}
	}
	for i := range s.Rules {
		walk(s.Rules[i].Expr)
		walk(s.Rules[i].DisabledWhen)
		walk(s.Rules[i].FairWhen)
		walk(s.Rules[i].Check)
	}
	branches := make([]string, 0, len(s.BranchExpressions))
	for name := range s.BranchExpressions {
		branches = append(branches, name)
	}
	sort.Strings(branches)
	for _, name := range branches {
		walk(s.BranchExpressions[name])
	}
	return uses
}

// arrayFieldType types an "isEmpty": "array" field from the values contains
// looks for in it: []string or []float64 when every value has that type, and
// []any when the values mix types or include a boolean or null.
func arrayFieldType(uses []*schema.Expr) GoType {
	var elem GoType
	for _, e := range uses {
		if e.Op != "contains" {
			continue
		}
		var t GoType
		switch e.Value.(type) {
		case string:
			t = GoString
		case float64:
			t = GoFloat64
		default:
			return GoAnySlice
		}
		if elem != "" && elem != t {
			return GoAnySlice
		}
		elem = t
	}
	if elem == GoFloat64 {
		return GoFloat64Slice
	}
	return GoStringSlice
}

// holdsJSONValue reports whether a field without a declared array or scalar
// isEmpty needs GoAny: contains reads it as an array while other ops may read
// it as a scalar, fieldInCond compares it with a condition whose elements are
// of another Go type, or a comparison holds a value of another JSON type than
// its inferred Go type.
func holdsJSONValue(s *schema.Schema, uses []*schema.Expr, t GoType) bool {
	for _, e := range uses {
		switch e.Op {
		case "contains":
			return true
		case "fieldInCond":
			if elem := conditionFieldElem(s, e.Condition); elem != "" && elem != t.Base() {
				return true
			}
		case "in", "notIn":
			values, _ := e.Value.([]any)
			for _, v := range values {
				if !typeHolds(t, v) {
					return true
				}
			}
		}
	}
	return false
}

// conditionFieldElem returns the Go element type of the array condition that
// name refers to, or "" when the schema does not declare it as an array of
// strings or numbers.
func conditionFieldElem(s *schema.Schema, name string) GoType {
	for i := range s.Conditions {
		if s.Conditions[i].Name == name {
			return conditionElem(GoTypeName(s.Conditions[i].Type))
		}
	}
	return ""
}

// typeHolds reports whether a value of Go type t can equal the JSON primitive v.
func typeHolds(t GoType, v any) bool {
	switch v := v.(type) {
	case string:
		return t.Base() == GoString
	case bool:
		return t.Base() == GoBool
	case float64:
		switch t.Base() {
		case GoFloat64, "float32":
			return true
		case GoInt, "int64", "int32":
			return v == float64(int64(v))
		}
	}
	return false
}

// inferFieldTypeFromUsage tries to determine a field's type by scanning all expressions
// that reference it. Returns *string as the default fallback.
func inferFieldTypeFromUsage(fieldName string, s *schema.Schema, fieldRefs map[string]bool) GoType {
	// Look at all rules and their expressions for this field
	for _, rule := range s.Rules {
		if rule.Field == fieldName {
			if rule.Expr != nil {
				if t := peekFieldType(rule.Expr); t != "" {
					return GoTypeForField(t, true)
				}
			}
		}
		if rule.DependsOn == fieldName {
			if rule.Expr != nil {
				if t := peekFieldType(rule.Expr); t != "" {
					return GoTypeForField(t, true)
				}
			}
		}
	}

	// Check all rule.Expr (when conditions) that reference this field
	for _, rule := range s.Rules {
		if rule.Expr != nil {
			if fieldInExpr(fieldName, rule.Expr) {
				if t := peekFieldType(rule.Expr); t != "" {
					return GoTypeForField(t, true)
				}
			}
		}
	}

	// Check disabledWhen and fairWhen rules that reference this field
	for _, rule := range s.Rules {
		if rule.DisabledWhen != nil {
			if fieldInExpr(fieldName, rule.DisabledWhen) {
				if t := peekFieldType(rule.DisabledWhen); t != "" {
					return GoTypeForField(t, true)
				}
			}
		}
		if rule.FairWhen != nil {
			if fieldInExpr(fieldName, rule.FairWhen) {
				if t := peekFieldType(rule.FairWhen); t != "" {
					return GoTypeForField(t, true)
				}
			}
		}
		if rule.Check != nil {
			if fieldInExpr(fieldName, rule.Check) {
				if t := peekFieldType(rule.Check); t != "" {
					return GoTypeForField(t, true)
				}
			}
		}
	}

	// Default fallback: *string
	return GoStringPtr
}

// peekFieldType looks at an expression and tries to determine the type of its field reference.
func peekFieldType(e *schema.Expr) GoType {
	if e == nil {
		return ""
	}

	// If this expression compares a field to a value, infer from the value type
	if e.Field != "" && e.Value != nil && isComparisonOp(e.Op) {
		return GoTypeFromJSONValue(e.Value)
	}

	// Recurse into nested expressions
	for _, child := range e.Exprs {
		if t := peekFieldType(&child); t != "" {
			return t
		}
	}

	return ""
}

// fieldInExpr returns true if the given field name is referenced anywhere in the expression tree.
func fieldInExpr(fieldName string, e *schema.Expr) bool {
	if e == nil {
		return false
	}
	if e.Field == fieldName {
		return true
	}
	for _, child := range e.Exprs {
		if fieldInExpr(fieldName, &child) {
			return true
		}
	}
	return false
}

// isComparisonOp returns true if the op is a comparison that has a value to infer from.
func isComparisonOp(op string) bool {
	switch op {
	case "eq", "neq", "gt", "gte", "lt", "lte":
		return true
	default:
		return false
	}
}

// GoTypeFromJSONValue infers a GoType from a JSON value (interface{}).
func GoTypeFromJSONValue(v any) GoType {
	switch val := v.(type) {
	case string:
		return GoString
	case bool:
		return GoBool
	case float64:
		return GoFloat64
	case int:
		return GoInt
	case []any:
		// If all elements are strings, return []string; if numbers, return []float64
		if len(val) == 0 {
			return GoStringSlice
		}
		allString := true
		allFloat := true
		for _, item := range val {
			switch item.(type) {
			case string:
				allFloat = false
			case float64:
				allString = false
			default:
				allString = false
				allFloat = false
			}
		}
		if allString {
			return GoStringSlice
		}
		if allFloat {
			return GoFloat64Slice
		}
		return GoStringSlice
	default:
		return GoString
	}
}

// detectOneOfBranches looks for fields that appear to be oneOf/eitherOf branches.
// First, it uses explicit oneOf/eitherOf rules with Group and Branches fields to
// determine the group name and branch field names. Then it falls back to
// prefix-based grouping for schemas without explicit oneOf rules.
func detectOneOfBranches(s *schema.Schema) []OneOfBranch {
	type groupCollector struct {
		groupBase string
		branches  []OneOfBranch
		isOneOf   bool
	}

	groups := make(map[string]*groupCollector)
	var order []string

	// Scan rules for "oneOf" or "eitherOf" type rules with explicit Group/Branches fields
	for _, rule := range s.Rules {
		if rule.Type == "oneOf" || rule.Type == "eitherOf" {
			groupName := rule.Group
			if groupName == "" {
				// Fallback: derive from the shared prefix of the rule's target field
				// and any sibling fields, e.g. "paymentMethod" → "Payment".
				targetField := rule.Field
				if targetField == "" && len(rule.Fields) > 0 {
					targetField = rule.Fields[0]
				}
				if targetField != "" {
					groupName = sharedPrefixOf(s, targetField)
				}
				if groupName == "" {
					groupName = GoFieldName(targetField)
				}
			}
			branchBase := groupName + "Branch"

			if _, ok := groups[branchBase]; !ok {
				gc := &groupCollector{groupBase: groupName, isOneOf: rule.Type == "oneOf"}
				// Use explicit branch field names if available
				for _, bf := range rule.Branches {
					b := OneOfBranch{
						GroupName:    branchBase,
						Branch:       GoFieldName(bf),
						OriginalName: bf,
						Index:        len(gc.branches),
						IsOneOf:      gc.isOneOf,
						RuleType:     branchRuleType(s, bf),
					}
					// If the schema has a BranchKeys map, prefer the original branch key
					// (e.g. "phone" instead of "bullpenPhone") for the reason text.
					if s.BranchKeys != nil {
						if key, ok := s.BranchKeys[b.Branch]; ok {
							b.OriginalName = key
						}
					}
					// For eitherOf, try to load activation expression from schema-level map
					// BranchExpressions keys use the original lowercase branch name from fixtures
					if expr, ok := s.BranchExpressions[bf]; ok {
						b.Expression = expr
					}
					gc.branches = append(gc.branches, b)
				}
				// If no explicit branches but we have expressions for this group, create from them
				if len(gc.branches) == 0 {
					for exprKey, expr := range s.BranchExpressions {
						b := OneOfBranch{
							GroupName:    branchBase,
							Branch:       GoFieldName(exprKey),
							OriginalName: exprKey,
							Index:        len(gc.branches),
							Expression:   expr,
							IsOneOf:      gc.isOneOf,
						}
						gc.branches = append(gc.branches, b)
					}
				}
				// Last resort: discover branches by shared camelCase prefix of the
				// rule's target field. e.g. a oneOf rule with field "paymentMethod"
				// and no explicit branches will pick up "paymentProvider" as a sibling.
				if len(gc.branches) < 2 && rule.Field != "" {
					for _, sibling := range detectPrefixBranchesFor(s, groupName) {
						sibling.GroupName = branchBase
						sibling.Index = len(gc.branches)
						sibling.IsOneOf = gc.isOneOf
						gc.branches = append(gc.branches, sibling)
					}
				}
				groups[branchBase] = gc
				order = append(order, branchBase)
			}
		}
	}

	// Build ordered result
	var branches []OneOfBranch
	for _, key := range order {
		if g, ok := groups[key]; ok {
			branches = append(branches, g.branches...)
		}
	}

	// Fallback: prefix-based grouping for fields that don't appear in any rule.
	// Such fields are not part of the expression graph and are likely branches
	// of a synthetic oneOf group (e.g. paymentCreditCard / paymentBankTransfer).
	if len(branches) < 2 {
		branches = append(branches, detectPrefixBranches(s)...)
	}

	return branches
}

// fieldHasAnyRule reports whether the given field name is referenced in any
// rule (as the target, dependency, or inside an expression).
func fieldHasAnyRule(s *schema.Schema, fieldName string) bool {
	for _, r := range s.Rules {
		if r.Excluded {
			continue
		}
		if r.Field == fieldName || r.DependsOn == fieldName || r.Source == fieldName {
			return true
		}
		for _, t := range r.Targets {
			if t == fieldName {
				return true
			}
		}
		for _, dep := range r.Requires {
			if dep == fieldName {
				return true
			}
		}
		if r.Expr != nil && exprReferencesField(r.Expr, fieldName) {
			return true
		}
		if r.DisabledWhen != nil && exprReferencesField(r.DisabledWhen, fieldName) {
			return true
		}
		if r.FairWhen != nil && exprReferencesField(r.FairWhen, fieldName) {
			return true
		}
		if r.Check != nil && exprReferencesField(r.Check, fieldName) {
			return true
		}
	}
	return false
}

func exprReferencesField(e *schema.Expr, fieldName string) bool {
	if e == nil {
		return false
	}
	if e.Field == fieldName {
		return true
	}
	for i := range e.Exprs {
		if exprReferencesField(&e.Exprs[i], fieldName) {
			return true
		}
	}
	return false
}

// detectPrefixBranches groups fields by shared camelCase prefix when no
// explicit oneOf/eitherOf rule is present. Only considers fields that don't
// appear in any rule.
func detectPrefixBranches(s *schema.Schema) []OneOfBranch {
	prefixMap := make(map[string][]string) // prefix (PascalCase) -> []PascalCase field name
	for _, f := range s.Fields {
		if fieldHasAnyRule(s, f.Name) {
			continue
		}
		parts := splitCamelCase(f.Name)
		if len(parts) < 3 {
			// Need at least "prefix + suffix + suffix" to be a discriminator.
			continue
		}
		prefix := GoFieldName(parts[0])
		gn := GoFieldName(f.Name)
		prefixMap[prefix] = append(prefixMap[prefix], gn)
	}
	var out []OneOfBranch
	for prefix, fields := range prefixMap {
		if len(fields) < 2 {
			continue
		}
		groupBase := prefix + "Branch"
		for i, fn := range fields {
			out = append(out, OneOfBranch{
				GroupName: groupBase,
				Branch:    fn,
				Index:     i,
				IsOneOf:   true,
			})
		}
	}
	return out
}

// detectPrefixBranchesFor returns branches for the given groupName (a PascalCase
// shared prefix like "Payment"). Used when an explicit oneOf/eitherOf rule is
// present but lacks explicit Branches.
func detectPrefixBranchesFor(s *schema.Schema, groupName string) []OneOfBranch {
	groupLower := ""
	if len(groupName) > 0 {
		groupLower = string(groupName[0]+32) + groupName[1:]
	}
	var out []OneOfBranch
	for _, f := range s.Fields {
		if !strings.HasPrefix(f.Name, groupLower) {
			continue
		}
		out = append(out, OneOfBranch{
			Branch: GoFieldName(f.Name),
		})
	}
	return out
}

// sharedPrefixOf returns the PascalCase camelCase prefix shared between the
// given target field and at least one other field in the schema. Returns the
// empty string if no shared prefix is found.
func sharedPrefixOf(s *schema.Schema, fieldName string) string {
	targetParts := splitCamelCase(fieldName)
	if len(targetParts) == 0 {
		return ""
	}
	for _, other := range s.Fields {
		if other.Name == fieldName {
			continue
		}
		otherParts := splitCamelCase(other.Name)
		if len(otherParts) == 0 || otherParts[0] != targetParts[0] {
			continue
		}
		return GoFieldName(targetParts[0])
	}
	return ""
}

// branchRuleType returns the rule type associated with a branch name in the
// schema (e.g. "enabledWhen" or "fairWhen"). Defaults to "enabledWhen" if
// the schema does not record a specific type.
func branchRuleType(s *schema.Schema, branchName string) string {
	if s.BranchRuleTypes != nil {
		if t, ok := s.BranchRuleTypes[branchName]; ok && t != "" {
			return t
		}
	}
	return "enabledWhen"
}

// splitCamelCase splits a camelCase string into its component words.
// Acronym runs (e.g., HTTP in HTTPResponse) are kept together.
func splitCamelCase(s string) []string {
	var parts []string
	runes := []rune(s)

	start := 0
	for i := 1; i < len(runes); i++ {
		// Word boundary: lowercase followed by uppercase
		if unicode.IsLower(runes[i-1]) && unicode.IsUpper(runes[i]) {
			parts = append(parts, string(runes[start:i]))
			start = i
			continue
		}
		// Acronym boundary: uppercase run followed by lowercase
		// (e.g., HTTP → R in HTTPResponse)
		if i < len(runes)-1 &&
			unicode.IsUpper(runes[i]) &&
			unicode.IsUpper(runes[i-1]) &&
			unicode.IsLower(runes[i+1]) {
			parts = append(parts, string(runes[start:i]))
			start = i
		}
	}
	parts = append(parts, string(runes[start:]))

	if len(parts) == 0 {
		parts = append(parts, string(runes))
	}

	// Lowercase all parts
	for i := range parts {
		p := []rune(parts[i])
		for j := range p {
			p[j] = unicode.ToLower(p[j])
		}
		parts[i] = string(p)
	}

	return parts
}

// camelToJSONTag converts a Go field name (PascalCase) back to a JSON tag (camelCase).
// Lowercases the first character and acronym runs (2+ consecutive uppercase letters),
// except the last uppercase in an acronym if it's followed by lowercase (it starts a new word).
func camelToJSONTag(name string) string {
	if name == "" {
		return ""
	}
	runes := []rune(name)
	runes[0] = unicode.ToLower(runes[0])

	var processed []rune
	processed = append(processed, runes[0])

	i := 1
	for i < len(runes) {
		if unicode.IsUpper(runes[i]) {
			// Check if this is an acronym run (2+ consecutive uppercase letters)
			start := i
			for i < len(runes) && unicode.IsUpper(runes[i]) {
				i++
			}
			// Acronym run: 2+ uppercase letters
			if i-start >= 2 {
				// Lowercase all except the last one if followed by lowercase (new word)
				end := i
				if i < len(runes) && unicode.IsLower(runes[i]) {
					end-- // Keep last uppercase as-is (starts new word)
				}
				for j := start; j < end; j++ {
					processed = append(processed, unicode.ToLower(runes[j]))
				}
				// The last uppercase (if not consumed) is processed in the next iteration
				if end < i {
					processed = append(processed, runes[end])
				}
			} else {
				// Single uppercase letter (start of new word) → keep as-is
				processed = append(processed, runes[start])
			}
		} else {
			processed = append(processed, runes[i])
			i++
		}
	}

	return string(processed)
}
