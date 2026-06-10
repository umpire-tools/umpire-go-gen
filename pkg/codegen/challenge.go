package codegen

import (
	"fmt"
	"strings"
)

// ChallengeGenerator produces the Challenge function, ChallengeResult struct, and rule metadata table.
type ChallengeGenerator struct {
	availName    string
	fieldsName   string
	condName     string
	fields       []FieldTypeInfo
	fieldRuleMap map[string]*FieldRuleData
}

// NewChallengeGenerator creates a ChallengeGenerator.
func NewChallengeGenerator(availName, fieldsName, condName string, fields []FieldTypeInfo, frd map[string]*FieldRuleData) *ChallengeGenerator {
	return &ChallengeGenerator{
		availName:    availName,
		fieldsName:   fieldsName,
		condName:     condName,
		fields:       fields,
		fieldRuleMap: frd,
	}
}

// Generate produces the Challenge function, ChallengeResult struct, and rule metadata table.
func (g *ChallengeGenerator) Generate() string {
	var b strings.Builder
	b.WriteString(g.genRuleMetaTable())
	b.WriteString("\n")
	b.WriteString(g.genChallengeResultStruct())
	b.WriteString("\n")
	b.WriteString(g.genChallengeFunc())
	return b.String()
}

func (g *ChallengeGenerator) genRuleMetaTable() string {
	var b strings.Builder
	b.WriteString("// RuleMetaEntry holds metadata about a rule for Challenge output.\n")
	b.WriteString("type RuleMetaEntry struct {\n")
	b.WriteString("\tField    string\n")
	b.WriteString("\tRuleType string\n")
	b.WriteString("\tExpr     string\n")
	b.WriteString("\tReason   string\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("// ruleMeta holds metadata about all rules for Challenge output.\n")
	b.WriteString("var ruleMeta = []RuleMetaEntry{\n")

	for _, ft := range g.fields {
		gn := GoFieldName(ft.Name)
		fd := g.fieldRuleMap[gn]
		if fd == nil {
			continue
		}
		if fd.Enabled.Expr != "" {
			b.WriteString(fmt.Sprintf("{Field: %q, RuleType: %q, Expr: %q, Reason: %q},\n",
				fd.Enabled.Field, "enabledWhen", fd.Enabled.Expr, fd.Enabled.Reason))
		}
		if fd.Disabled.Expr != "" {
			b.WriteString(fmt.Sprintf("{Field: %q, RuleType: %q, Expr: %q, Reason: %q},\n",
				fd.Disabled.Field, "disables", fd.Disabled.Expr, fd.Disabled.Reason))
		}
		for _, req := range fd.Requires {
			b.WriteString(fmt.Sprintf("{Field: %q, RuleType: %q, Expr: \"\", Reason: %q},\n",
				req.Field, "requires", req.Reason))
		}
		if fd.Fair.Expr != "" {
			b.WriteString(fmt.Sprintf("{Field: %q, RuleType: %q, Expr: %q, Reason: %q},\n",
				fd.Fair.Field, "fairWhen", fd.Fair.Expr, fd.Fair.Reason))
		}
		if fd.Check.Expr != "" {
			b.WriteString(fmt.Sprintf("{Field: %q, RuleType: %q, Expr: %q, Reason: %q},\n",
				fd.Check.Field, "check", fd.Check.Expr, fd.Check.Reason))
		}
	}

	b.WriteString("}\n")
	return b.String()
}

func (g *ChallengeGenerator) genChallengeResultStruct() string {
	b := "// ChallengeResult holds the result of a Challenge call.\n"
	b += "type ChallengeResult struct {\n"
	b += "\tFieldName    string\n"
	b += "\tStatus       FieldStatus\n"
	b += "\tExplanations []string\n"
	b += "}\n"
	return b
}

func (g *ChallengeGenerator) genChallengeFunc() string {
	var b strings.Builder

	b.WriteString("func Challenge(fieldName string, f ")
	b.WriteString(g.fieldsName)
	b.WriteString(", c ")
	b.WriteString(g.condName)
	b.WriteString(", prev ")
	b.WriteString(g.fieldsName)
	b.WriteString(") ChallengeResult {\n")

	b.WriteString("\tavail := Check(f, c, prev)\n")
	b.WriteString("\tvar status FieldStatus\n")
	b.WriteString("\tvar found bool\n")
	b.WriteString("\tvar explanations []string\n")

	// Build a switch that matches the field name (both Go and JSON names)
	b.WriteString("\tswitch fieldName {\n")
	for _, ft := range g.fields {
		gn := GoFieldName(ft.Name)
		b.WriteString(fmt.Sprintf("\tcase %q, %q:\n", gn, ft.Name))
		b.WriteString("\t\tfound = true\n")
		b.WriteString(fmt.Sprintf("\t\tstatus = avail.%s\n", gn))

		// Add explanation from the status Reason
		b.WriteString("\t\tif status.Reason != nil {\n")
		b.WriteString("\t\t\texplanations = append(explanations, \"* \"+*status.Reason)\n")
		b.WriteString("\t\t}\n")

		// Add explanations from rule metadata for this field
		fd := g.fieldRuleMap[gn]
		if fd != nil {
			if fd.Enabled.Expr != "" && fd.Enabled.Reason != "" {
				b.WriteString("\t\texplanations = append(explanations, ")
				b.WriteString(q(fmt.Sprintf("* enabledWhen: %s", fd.Enabled.Reason)))
				b.WriteString(")\n")
			}
			if fd.Disabled.Expr != "" && fd.Disabled.Reason != "" {
				b.WriteString("\t\texplanations = append(explanations, ")
				b.WriteString(q(fmt.Sprintf("* disables: %s", fd.Disabled.Reason)))
				b.WriteString(")\n")
			}
			for _, req := range fd.Requires {
				if req.Reason != "" {
					b.WriteString("\t\texplanations = append(explanations, ")
					b.WriteString(q(fmt.Sprintf("* requires %s: %s", req.Field, req.Reason)))
					b.WriteString(")\n")
				}
			}
			if fd.Fair.Expr != "" && fd.Fair.Reason != "" {
				b.WriteString("\t\texplanations = append(explanations, ")
				b.WriteString(q(fmt.Sprintf("* fairWhen: %s", fd.Fair.Reason)))
				b.WriteString(")\n")
			}
			if fd.Check.Expr != "" && fd.Check.Reason != "" {
				b.WriteString("\t\texplanations = append(explanations, ")
				b.WriteString(q(fmt.Sprintf("* check: %s", fd.Check.Reason)))
				b.WriteString(")\n")
			}
		}
	}

	b.WriteString("\tdefault:\n")
	b.WriteString("\t\t// Try to find by matching against known field names\n")
	b.WriteString("\t\tfor _, entry := range ruleMeta {\n")
	b.WriteString("\t\t\tif entry.Field == fieldName {\n")
	b.WriteString("\t\t\t\tif entry.Reason != \"\" {\n")
	b.WriteString("\t\t\t\t\texplanations = append(explanations, \"* \"+entry.RuleType+\": \"+entry.Reason)\n")
	b.WriteString("\t\t\t\t}\n")
	b.WriteString("\t\t\t}\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t\tif len(explanations) == 0 {\n")
	b.WriteString("\t\t\texplanations = []string{\"unknown field: \" + fieldName}\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n")

	b.WriteString("\tif !found {\n")
	b.WriteString("\t\treturn ChallengeResult{FieldName: fieldName, Status: FieldStatus{}, Explanations: explanations}\n")
	b.WriteString("\t}\n")

	b.WriteString("\treturn ChallengeResult{FieldName: fieldName, Status: status, Explanations: explanations}\n")
	b.WriteString("}\n")

	return b.String()
}
