package codegen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func isValidEmail(s string) bool {
	return emailRegex.MatchString(s)
}

func isValidRegexPattern(s, pattern string) bool {
	_, err := regexp.Compile(pattern)
	return err == nil
}

// OneOfGroup represents a oneOf/eitherOf group with its branches.
type OneOfGroup struct {
	Name     string         // e.g. "BullpenCallBranch"
	Branches []OneOfBranch  // branches with their activation conditions
	IsOneOf  bool           // true for oneOf (mutually exclusive), false for eitherOf
}

// CheckGenerator produces the Check function and helper for evaluating FieldStatus.
type CheckGenerator struct {
	availName     string
	fieldsName    string
	condName      string
	fields        []FieldTypeInfo
	fieldTypes    map[string]GoType
	fieldRuleMap  map[string]*FieldRuleData
	oneOfGroups   []OneOfGroup
	exprCompile   func(*schema.Expr) (string, error)
}

// NewCheckGenerator creates a CheckGenerator.
func NewCheckGenerator(availName, fieldsName, condName string, fields []FieldTypeInfo, frd map[string]*FieldRuleData, oneOfGroups []OneOfGroup) *CheckGenerator {
	fieldTypes := make(map[string]GoType, len(fields))
	for _, ft := range fields {
		fieldTypes[ft.Name] = ft.GoType
	}
	return &CheckGenerator{
		availName:     availName,
		fieldsName:    fieldsName,
		condName:      condName,
		fields:        fields,
		fieldTypes:    fieldTypes,
		fieldRuleMap:  frd,
		oneOfGroups:   oneOfGroups,
		exprCompile:   nil, // will be set via WithExprCompiler
	}
}

// WithExprCompiler sets the expression compiler function on this CheckGenerator.
func (g *CheckGenerator) WithExprCompiler(c *ExprCompiler) *CheckGenerator {
	g.exprCompile = c.Compile
	return g
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
			case GoMap:
				b.WriteString("		return len(f.")
				b.WriteString(gn)
				b.WriteString(") > 0\n")
			default:
				b.WriteString("		return true\n")
			}
		}
	}
	b.WriteString("\tdefault:\n\t\treturn true\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")

	// Add validation helpers
	b.WriteString("\nvar emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\\-]+@[a-zA-Z0-9.\\-]+\\.[a-zA-Z]{2,}$`)\n")
	b.WriteString("\n// isValidEmail checks if a string is a valid email address.\n")
	b.WriteString("func isValidEmail(s string) bool {\n")
	b.WriteString("\treturn emailRegex.MatchString(s)\n")
	b.WriteString("}\n")

	b.WriteString("\n// isValidRegexPattern checks if a string is a valid regex pattern.\n")
	b.WriteString("func isValidRegexPattern(s, pattern string) bool {\n")
	b.WriteString("\t_, err := regexp.Compile(pattern)\n")
	b.WriteString("\treturn err == nil\n")
	b.WriteString("}\n")

	b.WriteString("\n// isValidRegexMatch checks if a string matches a regex pattern.\n")
	b.WriteString("func isValidRegexMatch(s, pattern string) bool {\n")
	b.WriteString("\tre, err := regexp.Compile(pattern)\n")
	b.WriteString("\tif err != nil { return false }\n")
	b.WriteString("\treturn re.MatchString(s)\n")
	b.WriteString("}\n")

	b.WriteString("\nvar urlRegex = regexp.MustCompile(`^https?://[^\\s/$.?#].[^\\s]*$`)\n")
	b.WriteString("\n// isValidURL checks if a string is a valid URL.\n")
	b.WriteString("func isValidURL(s string) bool {\n")
	b.WriteString("\treturn urlRegex.MatchString(s)\n")
	b.WriteString("}\n")

	b.WriteString("\nvar integerRegex = regexp.MustCompile(`^-?\\d+$`)\n")
	b.WriteString("\n// isValidInteger checks if a string represents a valid integer.\n")
	b.WriteString("func isValidInteger(s string) bool {\n")
	b.WriteString("\treturn integerRegex.MatchString(s)\n")
	b.WriteString("}\n")

	b.WriteString("\nvar numberRegex = regexp.MustCompile(`^-?\\d+(\\.\\d+)?$`)\n")
	b.WriteString("\n// isValidNumber checks if a string represents a valid number.\n")
	b.WriteString("func isValidNumber(s string) bool {\n")
	b.WriteString("\treturn numberRegex.MatchString(s)\n")
	b.WriteString("}\n")

	b.WriteString("\n// parseFloat converts a string to a float64, returning 0 if invalid.\n")
	b.WriteString("func parseFloat(s string) float64 {\n")
	b.WriteString("\tv, _ := strconv.ParseFloat(s, 64)\n")
	b.WriteString("\treturn v\n")
	b.WriteString("}\n")

	b.WriteString("\n// isInRange checks if a string number is within a range spec like \"[1,10]\".\n")
	b.WriteString("func isInRange(s, rangeSpec string) bool {\n")
	b.WriteString("\tv, err := strconv.ParseFloat(s, 64)\n")
	b.WriteString("\tif err != nil { return false }\n")
	b.WriteString("\tr := strings.TrimSpace(rangeSpec)\n")
	b.WriteString("\tif !strings.HasPrefix(r, \"[\") && !strings.HasPrefix(r, \"(\") { return false }\n")
	b.WriteString("\topenInclusive := strings.HasPrefix(r, \"[\")\n")
	b.WriteString("\tr = strings.TrimPrefix(r, \"[\")\n")
	b.WriteString("\tr = strings.TrimPrefix(r, \"(\")\n")
	b.WriteString("\tcloseInclusive := strings.HasSuffix(r, \"]\")\n")
	b.WriteString("\tr = strings.TrimSuffix(r, \"]\")\n")
	b.WriteString("\tr = strings.TrimSuffix(r, \")\")\n")
	b.WriteString("\tparts := strings.Split(r, \",\")\n")
	b.WriteString("\tif len(parts) != 2 { return false }\n")
	b.WriteString("\tlo, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)\n")
	b.WriteString("\tif err != nil { return false }\n")
	b.WriteString("\thi, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)\n")
	b.WriteString("\tif err != nil { return false }\n")
	b.WriteString("\tif openInclusive { if v < lo { return false } } else { if v <= lo { return false } }\n")
	b.WriteString("\tif closeInclusive { if v > hi { return false } } else { if v >= hi { return false } }\n")
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
			if branch.Expression != nil && g.exprCompile != nil {
				exprStr, err := g.exprCompile(branch.Expression)
				if err == nil && exprStr != "" {
					b.WriteString("\tif ")
					b.WriteString(exprStr)
					b.WriteString(" {\n")
				} else {
					b.WriteString("\tif depSatisfied(f, ")
					b.WriteString(q(branch.Branch))
					b.WriteString(") {\n")
				}
			} else {
				b.WriteString("\tif depSatisfied(f, ")
				b.WriteString(q(branch.Branch))
				b.WriteString(") {\n")
			}
			b.WriteString("\t\t")
			b.WriteString(group.Name)
			b.WriteString("Active = ")
			b.WriteString(branch.Branch)
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
	g.emitSatisfied(b, gn, goType, base, isPtr, fd.IsEmpty)
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
	// Handle eitherOf with enabledWhen branches: enabled = OR of branch expressions
	if fd.IsEitherOf && !fd.IsFairOf && len(fd.Enabled.BranchExprs) > 0 {
		var parts []string
		for _, branchExpr := range fd.Enabled.BranchExprs {
			parts = append(parts, branchExpr)
		}
		b.WriteString("(" + strings.Join(parts, " || ") + ")")
		return
	}

	// Original logic for non-eitherOf fields
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
		// requires: enabled only if all dependencies are enabled AND satisfied AND fair
		var reqParts []string
		for _, req := range fd.Requires {
			reqParts = append(reqParts, g.depFullExpr(req.Field))
		}
		parts = append(parts, strings.Join(reqParts, " && "))
	}

	// oneOf branch disabling: if this field is in a oneOf group and not the active branch, disable it
	for _, group := range g.oneOfGroups {
		if !group.IsOneOf {
			continue
		}
		for _, branch := range group.Branches {
			if branch.Branch == fd.GoName {
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

func (g *CheckGenerator) emitSatisfied(b *strings.Builder, gn string, goType GoType, base GoType, isPtr bool, hasIsEmpty bool) {
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
	// Non-pointer types
	if !hasIsEmpty {
		b.WriteString("true")
		return
	}
	switch goType {
	case GoStringSlice, GoFloat64Slice:
		b.WriteString("len(f.")
		b.WriteString(gn)
		b.WriteString(") > 0")
	case GoMap:
		b.WriteString("len(f.")
		b.WriteString(gn)
		b.WriteString(") > 0")
	case GoString:
		b.WriteString("f.")
		b.WriteString(gn)
		b.WriteString(" != \"\"")
	case GoBool:
		b.WriteString("true")
	case GoInt, GoFloat64:
		b.WriteString("true")
	default:
		b.WriteString("true")
	}
}

func (g *CheckGenerator) emitFair(b *strings.Builder, fd *FieldRuleData) {
	// Handle eitherOf with fairWhen branches: fair = OR of branch expressions
	if fd.IsEitherOf && fd.IsFairOf && len(fd.Enabled.BranchExprs) > 0 {
		var parts []string
		for _, branchExpr := range fd.Enabled.BranchExprs {
			parts = append(parts, branchExpr)
		}
		b.WriteString("(" + strings.Join(parts, " || ") + ")")
		return
	}
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
	// Handle eitherOf: reasons come from failing branches
	// For the FIRST branch: find the first failing sub-condition's reason (primary Reason)
	// For ALL branches: collect the first reason of each failing branch (Reasons)
	if fd.IsEitherOf && len(fd.Enabled.BranchExprs) > 0 {		// For each branch, if the branch expression fails, add the first reason
		for i, branchExpr := range fd.Enabled.BranchExprs {
			// Collect ALL reasons for this branch (per the test expectations)
			// The branch's sub-conditions all need to be considered
			if i < len(fd.Enabled.BranchSubExprs) {
				subExprs := fd.Enabled.BranchSubExprs[i]
				subReasons := []string{}
				if i < len(fd.Enabled.BranchSubReasons) {
					subReasons = fd.Enabled.BranchSubReasons[i]
				}
				// For each sub-condition, if it fails, add its reason
				for j, subExpr := range subExprs {
					var subReason string
					if j < len(subReasons) {
						subReason = subReasons[j]
					}
					if subReason != "" {
						b.WriteString("\t\t\t\tif !(")
						b.WriteString(subExpr)
						b.WriteString(") && ")
						b.WriteString(q(subReason))
						b.WriteString(" != \"\" { reasons = append(reasons, ")
						b.WriteString(q(subReason))
						b.WriteString(") }\n")
					}
				}
			} else {
				// Fallback: use the first reason
				reason := ""
				if i < len(fd.Enabled.BranchReasons) {
					reason = fd.Enabled.BranchReasons[i]
				}
				if reason != "" {
					b.WriteString("\t\t\t\tif !(")
					b.WriteString(branchExpr)
					b.WriteString(") && ")
					b.WriteString(q(reason))
					b.WriteString(" != \"\" { reasons = append(reasons, ")
					b.WriteString(q(reason))
					b.WriteString(") }\n")
				}
			}
		}
		return
	}

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
		fullExpr := g.depFullExpr(req.Field)
		if strings.ContainsAny(fullExpr, "||") && !strings.HasPrefix(fullExpr, "(") {
			fullExpr = "(" + fullExpr + ")"
		}
		b.WriteString("\t\t\t\tif !(")
		b.WriteString(fullExpr)
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

	// oneOf branch conflict: this field is a branch of a oneOf group and is not the active branch
	for _, group := range g.oneOfGroups {
		if !group.IsOneOf {
			continue
		}
		isBranch := false
		for _, branch := range group.Branches {
			if branch.Branch == fd.GoName {
				isBranch = true
				break
			}
		}
		if !isBranch {
			continue
		}
		// Generate: switch <groupName>Active { case <otherBranch>: reasons = append(reasons, "conflicts with <otherOriginal> strategy"); }
		b.WriteString("\t\t\t\tswitch ")
		b.WriteString(group.Name)
		b.WriteString("Active {\n")
		for _, branch := range group.Branches {
			if branch.Branch == fd.GoName {
				continue
			}
			original := branch.OriginalName
			if original == "" {
				original = branch.Branch
			}
			b.WriteString("\t\t\t\tcase ")
			b.WriteString(branch.Branch)
			b.WriteString(":\n")
			b.WriteString("\t\t\t\t\treasons = append(reasons, ")
			b.WriteString(q(fmt.Sprintf("conflicts with %s strategy", original)))
			b.WriteString(")\n")
		}
		b.WriteString("\t\t\t\t}\n")
	}


}

// q wraps a string in double quotes for Go source output.
func q(s string) string {
	return fmt.Sprintf("%q", s)
}

// fieldEnabledExpr returns the Go boolean expression that evaluates whether the
// named dependency field is currently enabled. The expression is inlined from
// the dependency's own enabled/disables/oneOf-branch logic.
func (g *CheckGenerator) fieldEnabledExpr(goName string) string {
	depFD, ok := g.fieldRuleMap[goName]
	if !ok {
		return "true"
	}
	depFT, hasFT := g.findFieldTypeByGoName(goName)

	// oneOf branch disabling: only the active branch is enabled.
	for _, group := range g.oneOfGroups {
		if !group.IsOneOf {
			continue
		}
		for _, branch := range group.Branches {
			if branch.Branch == goName {
				return fmt.Sprintf("%sActive == %s", group.Name, goName)
			}
		}
	}

	if depFD.IsEitherOf && len(depFD.Enabled.BranchExprs) > 0 {
		return "(" + strings.Join(depFD.Enabled.BranchExprs, " || ") + ")"
	}

	var parts []string
	if depFD.Enabled.Expr != "" {
		parts = append(parts, depFD.Enabled.Expr)
	}
	if depFD.Disabled.Expr != "" {
		parts = append(parts, "!("+depFD.Disabled.Expr+")")
	}
	// A dependency may itself have requires; inline them too.
	if len(depFD.Requires) > 0 {
		var reqParts []string
		for _, req := range depFD.Requires {
			reqParts = append(reqParts, g.depFullExpr(req.Field))
		}
		parts = append(parts, strings.Join(reqParts, " && "))
	}
	_ = hasFT
	_ = depFT

	if len(parts) == 0 {
		return "true"
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " && ") + ")"
}

// fieldSatisfiedExpr returns the Go boolean expression that evaluates whether
// the named dependency field is currently satisfied. The expression is inlined
// using the dependency's GoType and isEmpty metadata.
func (g *CheckGenerator) fieldSatisfiedExpr(goName string) string {
	ft, ok := g.findFieldTypeByGoName(goName)
	if !ok {
		return "true"
	}
	depFD, fdOK := g.fieldRuleMap[goName]
	var hasIsEmpty bool
	if fdOK {
		hasIsEmpty = depFD.IsEmpty
	}
	isPtr := ft.GoType.Nullable()
	base := ft.GoType.Base()
	if isPtr {
		switch base {
		case GoString:
			return fmt.Sprintf("(func() bool { v := f.%s; return v != nil && *v != \"\" })()", goName)
		default:
			return fmt.Sprintf("f.%s != nil", goName)
		}
	}
	// Non-pointer with explicit isEmpty: empty values are unsatisfied.
	if hasIsEmpty {
		switch ft.GoType {
		case GoStringSlice, GoFloat64Slice:
			return fmt.Sprintf("len(f.%s) > 0", goName)
		case GoMap:
			return fmt.Sprintf("len(f.%s) > 0", goName)
		case GoString:
			return fmt.Sprintf("f.%s != \"\"", goName)
		default:
			return "true"
		}
	}
	// Non-pointer without isEmpty: zero value counts as present (per Issue C).
	return "true"
}

// fieldFairExpr returns the Go boolean expression that evaluates whether the
// named dependency field is currently fair. The expression is inlined using
// the dependency's fairWhen/check rule.
func (g *CheckGenerator) fieldFairExpr(goName string) string {
	depFD, ok := g.fieldRuleMap[goName]
	if !ok {
		return "true"
	}
	if depFD.Check.Expr != "" {
		return depFD.Check.Expr
	}
	if depFD.Fair.Expr != "" {
		return depFD.Fair.Expr
	}
	return "true"
}

// depFullExpr returns a Go boolean expression representing "dependency is
// enabled AND satisfied AND fair" — the full preconditions for a requires rule.
// Trivially-true and duplicate components are elided to keep the generated Go
// readable and avoid vet "redundant and" diagnostics.
func (g *CheckGenerator) depFullExpr(goName string) string {
	enabled := g.fieldEnabledExpr(goName)
	satisfied := g.fieldSatisfiedExpr(goName)
	fair := g.fieldFairExpr(goName)
	parts := make([]string, 0, 3)
	seen := make(map[string]bool, 3)
	add := func(expr string) {
		if expr == "true" || seen[expr] {
			return
		}
		seen[expr] = true
		parts = append(parts, expr)
	}
	add(enabled)
	add(satisfied)
	add(fair)
	if len(parts) == 0 {
		return "true"
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " && ") + ")"
}

// findFieldTypeByGoName returns the FieldTypeInfo for a given Go field name.
func (g *CheckGenerator) findFieldTypeByGoName(goName string) (FieldTypeInfo, bool) {
	for _, ft := range g.fields {
		if GoFieldName(ft.Name) == goName {
			return ft, true
		}
	}
	return FieldTypeInfo{}, false
}
