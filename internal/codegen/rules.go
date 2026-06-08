package codegen

import "github.com/umpire-tools/umpire-gen/internal/schema"

// RuleContribution holds the compiled contribution of a single rule to a FieldStatus.
type RuleContribution struct {
	RuleType string // "enabledWhen", "disables", "requires", "fairWhen", "check"
	Field    string // Go field name (e.g. "Country")
	Expr     string // compiled Go boolean expression (empty for requires)
	Reason   string // reason string (may be empty)
}

// FieldRuleData holds all rule contributions for a single field, plus metadata.
type FieldRuleData struct {
	GoName   string
	Required bool // from FieldDef.Required
	IsEmpty  bool // from FieldDef.IsEmpty
	Enabled  RuleContribution
	Disabled RuleContribution
	Requires []RuleContribution
	Fair     RuleContribution
	Check    RuleContribution
}

// RuleCompiler compiles all schema rules into per-field contribution data.
type RuleCompiler struct {
	fieldTypes  map[string]GoType
	condTypes   map[string]GoType
	fieldMap    map[string]*schema.FieldDef
	result      map[string]*FieldRuleData
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

// CompileRules processes all rules and returns per-field data.
func (rc *RuleCompiler) CompileRules(rules []schema.Rule) map[string]*FieldRuleData {
	for _, f := range rc.fieldMap {
		gn := GoFieldName(f.Name)
		rc.result[gn] = &FieldRuleData{
			GoName:   gn,
			Required: f.Required,
			IsEmpty:  f.IsEmpty != "",
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
				compiled, err := comp.Compile(rule.Check)
				if err == nil {
					fd.Check = RuleContribution{
						RuleType: "check",
						Field:    gn,
						Expr:     compiled,
						Reason:   rule.Reason,
					}
				}
			}
		}
	}

	return rc.result
}
