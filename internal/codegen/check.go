package codegen

import (
	"fmt"
	"strings"
)

// OneOfGroup represents a oneOf/eitherOf group with its branches.
type OneOfGroup struct {
	Name     string   // e.g. "BullpenCallBranch"
	Branches []string // field names (Go names) for each branch
}

// CheckGenerator produces the Check function and helper for evaluating FieldStatus.
type CheckGenerator struct {
	availName     string
	fieldsName    string
	condName      string
	fields        []FieldTypeInfo
	fieldRuleMap  map[string]*FieldRuleData
	oneOfGroups   []OneOfGroup
}

// NewCheckGenerator creates a CheckGenerator.
func NewCheckGenerator(availName, fieldsName, condName string, fields []FieldTypeInfo, frd map[string]*FieldRuleData, oneOfGroups []OneOfGroup) *CheckGenerator {
	return &CheckGenerator{
		availName:     availName,
		fieldsName:    fieldsName,
		condName:      condName,
		fields:        fields,
		fieldRuleMap:  frd,
		oneOfGroups:   oneOfGroups,
	}
}

// Generate produces the Check function and helper satisfaction function.
func (g *CheckGenerator) Generate() (checkBody, helper string) {
	checkBody = g.genCheckBody()
	helper = g.genHelper()
	return
}

func (g *CheckGenerator) genHelper() string {
	var b strings.Builder
	b.WriteString("// depSatisfied reports whether a field value is satisfied.\n")
	b.WriteString("func depSatisfied(f ")
	b.WriteString(g.fieldsName)
	b.WriteString(", name string) bool {\n")
	b.WriteString("\tswitch name {\n")
	for _, ft := range g.fields {
		gn := GoFieldName(ft.Name)
		goType := ft.GoType
		isPtr := goType.Nullable()
		base := goType.Base()

		b.WriteString("\tcase ")
		b.WriteString(q(gn))
		b.WriteString(":\n")

		if isPtr {
			switch base {
			case GoString:
				b.WriteString("		v := f.")
				b.WriteString(gn)
				b.WriteString("\n")
				b.WriteString("		return v != nil && *v != \"\"\n")
			default:
				b.WriteString("		return f.")
				b.WriteString(gn)
				b.WriteString(" != nil\n")
			}
		} else {
			switch base {
			case GoString:
				b.WriteString("		return f.")
				b.WriteString(gn)
				b.WriteString(" != \"\"\n")
			case GoStringSlice, GoFloat64Slice:
				b.WriteString("		return len(f.")
				b.WriteString(gn)
				b.WriteString(") > 0\n")
			default:
				if base == "map[string]any" {
					b.WriteString("		return len(f.")
					b.WriteString(gn)
					b.WriteString(") > 0\n")
				} else {
					b.WriteString("		return true\n")
				}
			}
		}
	}
	b.WriteString("\tdefault:\n\t\treturn true\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")

	// Add validation helpers
	b.WriteString("\n// isValidEmail checks if a string is a valid email address.\n")
	b.WriteString("func isValidEmail(s string) bool {\n")
	b.WriteString("\tif s == \"\" {\n")
	b.WriteString("\t\treturn false\n")
	b.WriteString("\t}\n")
	b.WriteString("\tfor i, r := range s {\n")
	b.WriteString("\t\tif r == '@' && i > 0 {\n")
	b.WriteString("\t\t\treturn true\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn false\n")
	b.WriteString("}\n")

	b.WriteString("\n// isValidRegexPattern checks if a string is a valid regex pattern.\n")
	b.WriteString("func isValidRegexPattern(pattern string, s string) bool {\n")
	b.WriteString("\t_, err := regexp.Compile(pattern)\n")
	b.WriteString("\tif err != nil {\n")
	b.WriteString("\t\treturn false\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn true\n")
	b.WriteString("}\n")

	return b.String()
}

func (g *CheckGenerator) genCheckBody() string {
	var b strings.Builder

	b.WriteString("func Check(f ")
	b.WriteString(g.fieldsName)
	b.WriteString(", c ")
	b.WriteString(g.condName)
	b.WriteString(", prev ")
	b.WriteString(g.fieldsName)
	b.WriteString(") ")
	b.WriteString(g.availName)
	b.WriteString(" {\n")
	b.WriteString("\tvar _ = prev\n")

	// Compute oneOf active branches
	for _, group := range g.oneOfGroups {
		b.WriteString("\tvar ")
		b.WriteString(group.Name)
		b.WriteString("Active ")
		b.WriteString(group.Name)
		b.WriteString(" = ")
		b.WriteString(group.Name)
		b.WriteString("None\n")
		for _, branch := range group.Branches {
			b.WriteString("\tif depSatisfied(f, ")
			b.WriteString(q(branch))
			b.WriteString(") {\n")
			b.WriteString("\t\t")
			b.WriteString(group.Name)
			b.WriteString("Active = ")
			b.WriteString(branch)
			b.WriteString("\n")
			b.WriteString("\t}\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("\treturn ")
	b.WriteString(g.availName)
	b.WriteString("{\n")

	for _, ft := range g.fields {
		gn := GoFieldName(ft.Name)
		fd := g.fieldRuleMap[gn]
		if fd == nil {
			fd = &FieldRuleData{GoName: gn}
		}
		g.writeFieldStatus(&b, ft, fd)
	}

	// Set ActiveBranch fields
	for _, group := range g.oneOfGroups {
		b.WriteString("\t\tActive")
		b.WriteString(GoFieldName(group.Name))
		b.WriteString(": ")
		b.WriteString(group.Name)
		b.WriteString("Active,\n")
	}

	b.WriteString("\t}\n")
	b.WriteString("}\n")
	return b.String()
}

func (g *CheckGenerator) writeFieldStatus(b *strings.Builder, ft FieldTypeInfo, fd *FieldRuleData) {
	gn := GoFieldName(ft.Name)
	goType := ft.GoType
	isPtr := goType.Nullable()
	base := goType.Base()

	b.WriteString("\t\t")
	b.WriteString(gn)
	b.WriteString(": FieldStatus{\n")

	// Required
	b.WriteString("\t\t\tRequired: ")
	if fd.Required {
		b.WriteString("true")
	} else {
		b.WriteString("false")
	}
	b.WriteString(",\n")

	// Enabled
	b.WriteString("\t\t\tEnabled: ")
	g.emitEnabled(b, fd)
	b.WriteString(",\n")

	// Satisfied
	b.WriteString("\t\t\tSatisfied: ")
	g.emitSatisfied(b, gn, goType, base, isPtr)
	b.WriteString(",\n")

	// Fair
	b.WriteString("\t\t\tFair: ")
	g.emitFair(b, fd)
	b.WriteString(",\n")

	// Reason
	b.WriteString("\t\t\t")
	b.WriteString("Reason: ")
	g.emitReason(b, fd)

	// Reasons
	b.WriteString("\t\t\t")
	b.WriteString("Reasons: ")
	g.emitReasons(b, fd)

	// Valid / Error - from check rule
	if fd.Check.Expr != "" {
		b.WriteString("\t\t\tValid: func() *bool { v := ")
		b.WriteString(fd.Check.Expr)
		b.WriteString("; return &v }(),\n")
		b.WriteString("\t\t\tError: func() string { if ")
		b.WriteString(fd.Check.Expr)
		b.WriteString(" { return \"\" }; return ")
		if fd.Check.Reason != "" {
			b.WriteString(q(fd.Check.Reason))
		} else {
			b.WriteString("\"validation failed\"")
		}
		b.WriteString(" }(),\n")
	} else {
		b.WriteString("\t\t\tValid:     nil,\n")
		b.WriteString("\t\t\tError:     \"\",\n")
	}

	b.WriteString("\t\t},\n")
}

func (g *CheckGenerator) emitEnabled(b *strings.Builder, fd *FieldRuleData) {
	// Combine enabledWhen, disables, and requires with AND logic
	var parts []string

	if fd.Enabled.Expr != "" {
		parts = append(parts, fd.Enabled.Expr)
	}
	if fd.Disabled.Expr != "" {
		// disables: enabled = NOT disabledExpr
		parts = append(parts, "!("+fd.Disabled.Expr+")")
	}
	if len(fd.Requires) > 0 {
		// requires: enabled only if all dependencies are satisfied
		var reqParts []string
		for _, req := range fd.Requires {
			reqParts = append(reqParts, "depSatisfied(f, "+q(req.Field)+")")
		}
		parts = append(parts, strings.Join(reqParts, " && "))
	}

	// oneOf branch disabling: if this field is in a oneOf group and not the active branch, disable it
	for _, group := range g.oneOfGroups {
		for _, branch := range group.Branches {
			if branch == fd.GoName {
				// This field is a branch in this group
				b.WriteString("func() bool {\n")
				b.WriteString("\tif ")
				b.WriteString(group.Name)
				b.WriteString("Active != ")
				b.WriteString(fd.GoName)
				b.WriteString(" { return false }\n")
				if len(parts) == 0 {
					b.WriteString("\treturn true\n")
				} else if len(parts) == 1 {
					b.WriteString("\treturn ")
					b.WriteString(parts[0])
					b.WriteString("\n")
				} else {
					b.WriteString("\treturn (")
					b.WriteString(strings.Join(parts, " && "))
					b.WriteString(")\n")
				}
				b.WriteString("}()")
				return
			}
		}
	}

	if len(parts) == 0 {
		b.WriteString("true")
	} else if len(parts) == 1 {
		b.WriteString(parts[0])
	} else {
		b.WriteString("(" + strings.Join(parts, " && ") + ")")
	}
}

func (g *CheckGenerator) emitSatisfied(b *strings.Builder, gn string, goType GoType, base GoType, isPtr bool) {
	if isPtr {
		switch base {
		case GoString:
			b.WriteString("func() bool { v := f.")
			b.WriteString(gn)
			b.WriteString("; return v != nil && *v != \"\" }()")
		default:
			b.WriteString("f.")
			b.WriteString(gn)
			b.WriteString(" != nil")
		}
		return
	}
	switch base {
	case GoString:
		b.WriteString("f.")
		b.WriteString(gn)
		b.WriteString(" != \"\"")
	case GoStringSlice, GoFloat64Slice:
		b.WriteString("len(f.")
		b.WriteString(gn)
		b.WriteString(") > 0")
	default:
		if base == "map[string]any" {
			b.WriteString("len(f.")
			b.WriteString(gn)
			b.WriteString(") > 0")
		} else {
			b.WriteString("true")
		}
	}
}

func (g *CheckGenerator) emitFair(b *strings.Builder, fd *FieldRuleData) {
	// Fair is true if fairWhen passes OR if check validator passes
	// If there's a check rule, Fair depends on the validation result
	if fd.Check.Expr != "" {
		// check rule: Fair = checkExpr (validation passes)
		b.WriteString(fd.Check.Expr)
	} else if fd.Fair.Expr != "" {
		b.WriteString(fd.Fair.Expr)
	} else {
		b.WriteString("true")
	}
}

func (g *CheckGenerator) emitReason(b *strings.Builder, fd *FieldRuleData) {
	b.WriteString("func() *string {\n")
	b.WriteString("\t\t\t\tvar reasons []string\n")
	g.addBlockingReasonChecks(b, fd)
	b.WriteString("\t\t\t\tif len(reasons) == 0 { return nil }\n")
	b.WriteString("\t\t\t\treturn &reasons[0]\n")
	b.WriteString("\t\t\t}(),\n")
}

func (g *CheckGenerator) emitReasons(b *strings.Builder, fd *FieldRuleData) {
	b.WriteString("func() []string {\n")
	b.WriteString("\t\t\t\tvar reasons []string\n")
	g.addBlockingReasonChecks(b, fd)
	b.WriteString("\t\t\t\tif reasons == nil { reasons = []string{} }\n")
	b.WriteString("\t\t\t\treturn reasons\n")
	b.WriteString("\t\t\t}(),\n")
}

func (g *CheckGenerator) addBlockingReasonChecks(b *strings.Builder, fd *FieldRuleData) {
	if fd.Enabled.Expr != "" && fd.Enabled.Reason != "" {
		b.WriteString("\t\t\t\tif !(")
		b.WriteString(fd.Enabled.Expr)
		b.WriteString(") && ")
		b.WriteString(q(fd.Enabled.Reason))
		b.WriteString(" != \"\" { reasons = append(reasons, ")
		b.WriteString(q(fd.Enabled.Reason))
		b.WriteString(") }\n")
	}

	if fd.Disabled.Expr != "" && fd.Disabled.Reason != "" {
		b.WriteString("\t\t\t\tif ")
		b.WriteString(fd.Disabled.Expr)
		b.WriteString(" && ")
		b.WriteString(q(fd.Disabled.Reason))
		b.WriteString(" != \"\" { reasons = append(reasons, ")
		b.WriteString(q(fd.Disabled.Reason))
		b.WriteString(") }\n")
	}

	for _, req := range fd.Requires {
		if req.Reason == "" {
			continue
		}
		b.WriteString("\t\t\t\tif !depSatisfied(f, ")
		b.WriteString(q(req.Field))
		b.WriteString(") && ")
		b.WriteString(q(req.Reason))
		b.WriteString(" != \"\" { reasons = append(reasons, ")
		b.WriteString(q(req.Reason))
		b.WriteString(") }\n")
	}

	// fairWhen: reason when fair condition fails
	if fd.Fair.Expr != "" && fd.Fair.Reason != "" {
		b.WriteString("\t\t\t\tif !(")
		b.WriteString(fd.Fair.Expr)
		b.WriteString(") && ")
		b.WriteString(q(fd.Fair.Reason))
		b.WriteString(" != \"\" { reasons = append(reasons, ")
		b.WriteString(q(fd.Fair.Reason))
		b.WriteString(") }\n")
	}

	// check: reason when validation fails
	if fd.Check.Expr != "" && fd.Check.Reason != "" {
		b.WriteString("\t\t\t\tif !(")
		b.WriteString(fd.Check.Expr)
		b.WriteString(") && ")
		b.WriteString(q(fd.Check.Reason))
		b.WriteString(" != \"\" { reasons = append(reasons, ")
		b.WriteString(q(fd.Check.Reason))
		b.WriteString(") }\n")
	}

	// oneOf branch conflict: reason when this branch is not active
	for _, group := range g.oneOfGroups {
		for _, branch := range group.Branches {
			if branch == fd.GoName {
				b.WriteString("\t\t\t\tif ")
				b.WriteString(group.Name)
				b.WriteString("Active != ")
				b.WriteString(fd.GoName)
				b.WriteString(" { reasons = append(reasons, \"conflicts with \")\n")
				b.WriteString("\t\t\t\t\treasons = append(reasons, strings.ToLower(")
				b.WriteString(q(group.Name))
				b.WriteString("))\n")
				b.WriteString("\t\t\t\t}\n")
			}
		}
	}
}

// q wraps a string in double quotes for Go source output.
func q(s string) string {
	return fmt.Sprintf("%q", s)
}
