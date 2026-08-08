package codegen

import (
	"strings"

	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
)

// RuleContribution holds the compiled contribution of a single rule to a FieldStatus.
type RuleContribution struct {
	RuleType string // "enabledWhen", "disables", "requires", "fairWhen", "check"
	Field    string // Go field name (e.g. "Country")
	Expr     string // compiled Go boolean expression (empty for requires)
	Reason   string // reason string (may be empty)
	// For eitherOf branches: per-branch compiled expressions and reasons
	BranchExprs   []string
	BranchReasons []string
	// For eitherOf branches: per-branch sub-conditions and their reasons
	// Used to find the first failing sub-condition in the first failing branch
	BranchSubExprs   [][]string // branchName -> [subExpr1, subExpr2, ...]
	BranchSubReasons [][]string // branchName -> [reason1, reason2, ...]
	BranchNames      []string   // ordered list of branch names
}

// joinAnd combines two boolean expressions with && and minimal parentheses.
// Empty strings are treated as "true" and elided.
func joinAnd(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return "(" + a + " && " + b + ")"
}

// ValidatorRule holds the compiled expression and error for a named validator.
type ValidatorRule struct {
	Expr  string
	Error string
}

// FieldRuleData holds all rule contributions for a single field, plus metadata.
type FieldRuleData struct {
	GoName     string
	Required   bool // from FieldDef.Required
	IsEmpty    bool // from FieldDef.IsEmpty
	Enabled    RuleContribution
	Disabled   RuleContribution
	Requires   []RuleContribution
	Fair       RuleContribution
	Check      RuleContribution
	Validator  *ValidatorRule
	IsEitherOf bool // true if this field's enabledWhen comes from an eitherOf
	IsFairOf   bool // true if this field's eitherOf branches are fairWhen (not enabledWhen)
}

// RuleCompiler compiles all schema rules into per-field contribution data.
type RuleCompiler struct {
	fieldTypes map[string]GoType
	condTypes  map[string]GoType
	fieldMap   map[string]*schema.FieldDef
	result     map[string]*FieldRuleData
	schema     *schema.Schema // for accessing BranchExpressions
}

// NewRuleCompiler creates a RuleCompiler.
func NewRuleCompiler(fieldTypes, condTypes map[string]GoType, fields []schema.FieldDef) *RuleCompiler {
	rc := &RuleCompiler{
		fieldTypes: fieldTypes,
		condTypes:  condTypes,
		fieldMap:   make(map[string]*schema.FieldDef),
		result:     make(map[string]*FieldRuleData),
	}
	for _, f := range fields {
		rc.fieldMap[f.Name] = &f
	}
	return rc
}

// WithSchema attaches the schema to the RuleCompiler for accessing BranchExpressions.
func (rc *RuleCompiler) WithSchema(s *schema.Schema) *RuleCompiler {
	rc.schema = s
	return rc
}

// CompileRules processes all rules and returns per-field data.
func (rc *RuleCompiler) CompileRules(rules []schema.Rule) map[string]*FieldRuleData {
	for _, f := range rc.fieldMap {
		gn := GoFieldName(f.Name)
		// IsEmpty tracks whether the field has a defined "presence" semantics
		// (i.e. an empty value is unsatisfied). This is true when the schema
		// sets IsEmpty OR when the type hint implies emptiness (string/number
		// type hints mark the field as a value whose zero state is "empty").
		isEmpty := f.IsEmpty != ""
		if !isEmpty && f.TypeHint != "" {
			switch f.TypeHint {
			case "string", "number":
				isEmpty = true
			}
		}
		rc.result[gn] = &FieldRuleData{
			GoName:   gn,
			Required: f.Required,
			IsEmpty:  isEmpty,
		}
	}

	comp := NewExprCompiler(rc.fieldTypes, rc.condTypes)

	for _, rule := range rules {
		if rule.Excluded {
			continue
		}

		targetField := rule.Field
		if targetField == "" && len(rule.Fields) > 0 {
			targetField = rule.Fields[0]
		}
		if targetField == "" {
			continue
		}

		gn := GoFieldName(targetField)
		fd, ok := rc.result[gn]
		if !ok {
			continue
		}

		switch rule.Type {
		case "enabledWhen":
			if rule.Expr != nil {
				expr, err := comp.Compile(rule.Expr)
				if err == nil {
					fd.Enabled = RuleContribution{
						RuleType: "enabledWhen",
						Field:    gn,
						Expr:     expr,
						Reason:   rule.Reason,
					}
				}
			}
		case "disables":
			expr := rule.Expr
			if expr == nil {
				expr = rule.DisabledWhen
			}
			if expr != nil {
				compiled, err := comp.Compile(expr)
				if err == nil {
					fd.Disabled = RuleContribution{
						RuleType: "disables",
						Field:    gn,
						Expr:     compiled,
						Reason:   rule.Reason,
					}
				}
			}
		case "requires":
			// If the requires rule has a "when" expression, treat it as an
			// additional enabled check that must also pass.
			if rule.Expr != nil {
				compiled, err := comp.Compile(rule.Expr)
				if err == nil && compiled != "" {
					fd.Enabled.Expr = joinAnd(fd.Enabled.Expr, compiled)
					// Use the rule's reason for the when expression so the
					// generated code surfaces it when the when check fails.
					if rule.Reason != "" {
						fd.Enabled.Reason = rule.Reason
					}
				}
			}
			for _, dep := range rule.Requires {
				depGN := GoFieldName(dep)
				fd.Requires = append(fd.Requires, RuleContribution{
					RuleType: "requires",
					Field:    depGN,
					Reason:   rule.Reason,
				})
			}
		case "fairWhen":
			if rule.FairWhen != nil {
				compiled, err := comp.Compile(rule.FairWhen)
				if err == nil {
					fd.Fair = RuleContribution{
						RuleType: "fairWhen",
						Field:    gn,
						Expr:     compiled,
						Reason:   rule.Reason,
					}
				}
			}
		case "check":
			if rule.Check != nil {
				checkExpr := *rule.Check
				if checkExpr.Field == "" {
					checkExpr.Field = targetField
				}
				compiled, err := comp.Compile(&checkExpr)
				if err == nil {
					fd.Check = RuleContribution{
						RuleType: "check",
						Field:    gn,
						Expr:     compiled,
						Reason:   rule.Reason,
					}
				}
			}
		case "oneOf", "eitherOf":
			// Marker rule - the actual branch logic comes from BranchExpressions
			// handled in CheckGenerator via oneOfGroups
			fd.IsEitherOf = (rule.Type == "eitherOf")
			// Populate branch expressions and reasons from the schema
			if rc.schema != nil && len(rc.schema.BranchExpressions) > 0 {
				// Use FieldBranches to scope branches to the current field
				// (otherwise branches from different fields would conflict)
				var branchOrder []string
				if rc.schema.FieldBranches != nil {
					if fieldBranches, ok := rc.schema.FieldBranches[targetField]; ok {
						branchOrder = fieldBranches
					}
				}
				if len(branchOrder) == 0 {
					// Fall back to using the rule's Branches field
					branchOrder = rule.Branches
				}
				// Determine if all branches are fairWhen (in which case this is a "fairOf" group)
				fairBranchCount := 0
				if rc.schema.BranchRuleTypes != nil {
					for _, bn := range branchOrder {
						if t, ok := rc.schema.BranchRuleTypes[bn]; ok && t == "fairWhen" {
							fairBranchCount++
						}
					}
				}
				if len(branchOrder) > 0 && fairBranchCount == len(branchOrder) {
					fd.IsFairOf = true
				}
				for _, branchName := range branchOrder {
					if expr, ok := rc.schema.BranchExpressions[branchName]; ok {
						compiled, err := comp.Compile(expr)
						if err == nil {
							fd.Enabled.BranchExprs = append(fd.Enabled.BranchExprs, compiled)
							fd.Enabled.BranchNames = append(fd.Enabled.BranchNames, branchName)
							// Get reasons for this branch (all reasons in order)
							if reasons, ok := rc.schema.BranchReasons[branchName]; ok && len(reasons) > 0 {
								// Use the first reason as the branch's primary reason
								fd.Enabled.BranchReasons = append(fd.Enabled.BranchReasons, reasons[0])
							} else {
								fd.Enabled.BranchReasons = append(fd.Enabled.BranchReasons, "")
							}
							// Compile sub-conditions for this branch
							if subExprs, ok := rc.schema.BranchSubConditions[branchName]; ok {
								var compiledSubs []string
								for _, subExpr := range subExprs {
									if subExpr != nil {
										subCompiled, err := comp.Compile(subExpr)
										if err == nil {
											compiledSubs = append(compiledSubs, subCompiled)
										}
									}
								}
								fd.Enabled.BranchSubExprs = append(fd.Enabled.BranchSubExprs, compiledSubs)
							}
							if subReasons, ok := rc.schema.BranchSubReasons[branchName]; ok {
								fd.Enabled.BranchSubReasons = append(fd.Enabled.BranchSubReasons, subReasons)
							}
						}
					}
				}
			}
		}
	}

	if rc.schema != nil && rc.schema.Validators != nil {
		for name, validator := range rc.schema.Validators {
			fd, ok := rc.result[GoFieldName(name)]
			if !ok {
				continue
			}

			validatorExpr := &schema.Expr{Op: validator.Op, Field: name}
			switch validator.Op {
			case "matches":
				validatorExpr.Value = validator.Pattern
				validatorExpr.Pattern = validator.Pattern
			case "minLength", "maxLength", "min", "max":
				if validator.Value == nil {
					continue
				}
				validatorExpr.Value = *validator.Value
			case "range":
				if validator.Min == nil || validator.Max == nil {
					continue
				}
				validatorExpr.Value = map[string]float64{"min": *validator.Min, "max": *validator.Max}
			}

			compiled, err := comp.Compile(validatorExpr)
			if err == nil {
				fd.Validator = &ValidatorRule{Expr: compiled, Error: validator.Error}
			}
		}
	}

	return rc.result
}
