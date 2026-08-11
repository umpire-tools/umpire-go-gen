package structgen

import (
	"fmt"
	"strings"
)

// isJSONNull returns a Go expression that is truthy when the raw map value is JSON null.
func isJSONNullExpr(r string) string {
	return "len(" + r + ") == 4 && string(" + r + ") == \"null\""
}

// emitStrictDecode renders an UnmarshalJSON for an object type that rejects
// unknown properties, JSON null for any known field, and missing required
// fields. Trailing JSON after the value is rejected by encoding/json.
func emitStrictDecode(b *strings.Builder, td TypeDef) {
	fmt.Fprintf(b, "func (v *%s) UnmarshalJSON(data []byte) error {\n", td.Name)
	b.WriteString("\tvar raw map[string]json.RawMessage\n")
	b.WriteString("\tif err := json.Unmarshal(data, &raw); err != nil {\n\t\treturn err\n\t}\n")
	b.WriteString("\tfor key := range raw {\n\t\tswitch key {\n")
	for _, f := range td.Fields {
		fmt.Fprintf(b, "\t\tcase %q:\n", f.JSONTag)
	}
	b.WriteString("\t\tdefault:\n\t\t\treturn fmt.Errorf(\"unknown field %q\", key)\n\t\t}\n\t}\n")
	// No field type in this subset accepts JSON null, so reject null for every
	// known field, not just required ones.
	for _, f := range td.Fields {
		fmt.Fprintf(b, "\tif r, ok := raw[%q]; ok {\n", f.JSONTag)
		b.WriteString("\t\tif " + isJSONNullExpr("r") + " {\n")
		fmt.Fprintf(b, "\t\t\treturn fmt.Errorf(%q)\n", "field \""+f.JSONTag+"\" must not be null")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
	}
	for _, f := range td.Fields {
		if f.Required {
			fmt.Fprintf(b, "\tif _, ok := raw[%q]; !ok {\n", f.JSONTag)
			fmt.Fprintf(b, "\t\treturn fmt.Errorf(%q)\n", "missing required field \""+f.JSONTag+"\"")
			b.WriteString("\t}\n")
		}
	}
	fmt.Fprintf(b, "\ttype alias %s\n", td.Name)
	b.WriteString("\treturn json.Unmarshal(data, (*alias)(v))\n")
	b.WriteString("}\n\n")
}

// emitValidation renders Validate() (root), validateInto() (every object/union),
// the Issue type, and the RFC 6901 escapePtr helper. Constraint checks use
// normalized codes and RFC 6901 JSON Pointer paths.
func emitValidation(b *strings.Builder, spec *Spec) {
	b.WriteString("// Issue describes one validation problem with an RFC 6901 JSON Pointer path.\n")
	b.WriteString("type Issue struct {\n\tCode string `json:\"code\"`\n\tPath string `json:\"path\"`\n}\n\n")
	b.WriteString("// escapePtr percent-escapes a JSON Pointer token per RFC 6901.\n")
	b.WriteString("func escapePtr(s string) string {\n")
	b.WriteString("\ts = strings.ReplaceAll(s, \"~\", \"~0\")\n")
	b.WriteString("\treturn strings.ReplaceAll(s, \"/\", \"~1\")\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "func (v %s) Validate() []Issue {\n", spec.RootName)
	b.WriteString("\tvar issues []Issue\n")
	fmt.Fprintf(b, "\tv.validate(\"\", &issues)\n")
	b.WriteString("\treturn issues\n")
	b.WriteString("}\n\n")

	emitValidateInto(b, spec, spec.RootName, spec.Root)
	for _, td := range spec.Types {
		if td.Kind == KindObject || td.Kind == KindUnion {
			emitValidateInto(b, spec, td.Name, td.Fields)
		}
	}
}

func emitValidateInto(b *strings.Builder, spec *Spec, typeName string, fields []FieldDef) {
	fmt.Fprintf(b, "func (v %s) validate(path string, issues *[]Issue) {\n", typeName)
	for _, f := range fields {
		emitFieldValidate(b, spec, f)
	}
	b.WriteString("}\n\n")
}

func emitFieldValidate(b *strings.Builder, spec *Spec, f FieldDef) {
	gn := "v." + f.GoName
	tagPath := `path + "/" + escapePtr(` + strconvQ(f.JSONTag) + `)`

	switch f.Type.Kind {
	case KindObject, KindUnion:
		if f.Required {
			fmt.Fprintf(b, "\t%s.validate(%s, issues)\n", gn, tagPath)
		} else {
			fmt.Fprintf(b, "\tif %s != nil {\n\t\t%s.validate(%s, issues)\n\t}\n", gn, gn, tagPath)
		}
		return
	case KindArray:
		emitArrayValidate(b, spec, f, gn, tagPath)
		return
	case KindEnum:
		emitEnumValidate(b, spec, f, gn, tagPath)
		return
	}

	// Scalar constraints, guarded by presence for optional fields.
	present, val := presentAndValue(f)
	if !alwaysPresent(f) {
		fmt.Fprintf(b, "\tif %s {\n", present)
	}
	emitScalarConstraints(b, f, val, tagPath)
	if !alwaysPresent(f) {
		b.WriteString("\t}\n")
	}
}

func emitArrayValidate(b *strings.Builder, spec *Spec, f FieldDef, gn, tagPath string) {
	// min/max items (nil slice present guard handled by len() == 0 below; a nil
	// slice reports 0 items so no separate guard is required).
	if f.MinItems != nil {
		fmt.Fprintf(b, "\tif len(%s) < %d {\n", gn, *f.MinItems)
		emitIssue(b, "minItems", tagPath)
		b.WriteString("\t}\n")
	}
	if f.MaxItems != nil {
		fmt.Fprintf(b, "\tif len(%s) > %d {\n", gn, *f.MaxItems)
		emitIssue(b, "maxItems", tagPath)
		b.WriteString("\t}\n")
	}

	elem := f.Type.Elem
	switch {
	case elem.IsReference() && (elem.Kind == KindObject || elem.Kind == KindUnion):
		fmt.Fprintf(b, "\tfor i, it := range %s {\n", gn)
		fmt.Fprintf(b, "\t\tit.validate(path+\"/%s/\"+strconv.Itoa(i), issues)\n", f.JSONTag)
		b.WriteString("\t}\n")
	case elem.Kind == KindEnum:
		emitArrayEnumValidate(b, spec, f, gn, tagPath)
	}
}

func emitArrayEnumValidate(b *strings.Builder, spec *Spec, f FieldDef, gn, tagPath string) {
	enum := spec.Lookup(f.Type.Elem.Ref)
	if enum == nil {
		return
	}
	fmt.Fprintf(b, "\tfor i, e := range %s {\n", gn)
	fmt.Fprintf(b, "\t\tswitch e {\n")
	for _, v := range enum.Values {
		fmt.Fprintf(b, "\t\tcase %s%s:\n", enum.Name, v.Name)
	}
	fmt.Fprintf(b, "\t\tdefault:\n")
	fmt.Fprintf(b, "\t\t\t*issues = append(*issues, Issue{Code: %q, Path: path+\"/%s/\"+strconv.Itoa(i)})\n", "enum", f.JSONTag)
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n")
}

// emitEnumValidate checks an enum-typed field's value is one of its declared
// constants (direct field, not array).
func emitEnumValidate(b *strings.Builder, spec *Spec, f FieldDef, gn, tagPath string) {
	enum := spec.Lookup(f.Type.Ref)
	if enum == nil {
		return
	}
	present, val := presentAndValue(f)
	if !alwaysPresent(f) {
		fmt.Fprintf(b, "\tif %s {\n", present)
	}
	fmt.Fprintf(b, "\tswitch %s {\n", val)
	for _, v := range enum.Values {
		fmt.Fprintf(b, "\tcase %s%s:\n", enum.Name, v.Name)
	}
	fmt.Fprintf(b, "\tdefault:\n")
	emitIssue(b, "enum", tagPath)
	b.WriteString("\t}\n")
	if !alwaysPresent(f) {
		b.WriteString("\t}\n")
	}
}

func emitScalarConstraints(b *strings.Builder, f FieldDef, val, tagPath string) {
	switch f.Type.Scalar {
	case ScalarString:
		if f.MinLength != nil {
			fmt.Fprintf(b, "\tif utf8.RuneCountInString(%s) < %d {\n", val, *f.MinLength)
			emitIssue(b, "minLength", tagPath)
			b.WriteString("\t}\n")
		}
		if f.MaxLength != nil {
			fmt.Fprintf(b, "\tif utf8.RuneCountInString(%s) > %d {\n", val, *f.MaxLength)
			emitIssue(b, "maxLength", tagPath)
			b.WriteString("\t}\n")
		}
	case ScalarInt:
		// Schema bounds can be fractional, for example minimum 0.5.
		// Cast the integer to float64 before comparison.
		if f.Minimum != nil {
			fmt.Fprintf(b, "\tif float64(%s) < %v {\n", val, *f.Minimum)
			emitIssue(b, "minimum", tagPath)
			b.WriteString("\t}\n")
		}
		if f.Maximum != nil {
			fmt.Fprintf(b, "\tif float64(%s) > %v {\n", val, *f.Maximum)
			emitIssue(b, "maximum", tagPath)
			b.WriteString("\t}\n")
		}
	case ScalarNumber:
		if f.Minimum != nil {
			fmt.Fprintf(b, "\tif %s < %v {\n", val, *f.Minimum)
			emitIssue(b, "minimum", tagPath)
			b.WriteString("\t}\n")
		}
		if f.Maximum != nil {
			fmt.Fprintf(b, "\tif %s > %v {\n", val, *f.Maximum)
			emitIssue(b, "maximum", tagPath)
			b.WriteString("\t}\n")
		}
	}
	// const only applies to scalar fields; structural consts are not emitted.
	if f.HasConst && f.Type.Kind == KindScalar {
		fmt.Fprintf(b, "\tif %s != %s {\n", val, constLiteral(f))
		emitIssue(b, "const", tagPath)
		b.WriteString("\t}\n")
	}
}

func emitIssue(b *strings.Builder, code, tagPath string) {
	fmt.Fprintf(b, "\t*issues = append(*issues, Issue{Code: %q, Path: %s})\n", code, tagPath)
}

// presentAndValue returns (guardExpr, valueExpr). valueExpr is safe to deref only
// when guardExpr is true.
func presentAndValue(f FieldDef) (present, val string) {
	gn := "v." + f.GoName
	if alwaysPresent(f) {
		return "true", gn
	}
	switch f.Type.Kind {
	case KindArray:
		return gn + " != nil", gn
	default:
		return gn + " != nil", "*" + gn
	}
}

func alwaysPresent(f FieldDef) bool {
	return f.Required
}

// constLiteral renders a typed Go literal for a scalar const value.
func constLiteral(f FieldDef) string {
	switch f.Type.Scalar {
	case ScalarBool:
		if b, ok := f.Const.(bool); ok {
			return fmt.Sprintf("%v", b)
		}
	case ScalarInt, ScalarNumber:
		if n, ok := f.Const.(float64); ok {
			return fmt.Sprintf("%v", n)
		}
	case ScalarString:
		if s, ok := f.Const.(string); ok {
			return strconvQ(s)
		}
	}
	return `""`
}

func strconvQ(s string) string {
	return fmt.Sprintf("%q", s)
}
