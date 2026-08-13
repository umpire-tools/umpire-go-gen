package structgen

import (
	"fmt"
	"strings"

	"github.com/umpire-tools/umpire-go-gen/pkg/codegen"
)

// isJSONNull returns a Go expression that is truthy when the raw map value is JSON null.
func isJSONNullExpr(r string) string {
	return "len(" + r + ") == 4 && string(" + r + ") == \"null\""
}

// emitStrictDecode renders an UnmarshalJSON for an object type that rejects
// unknown properties, JSON null for any known field, and missing required
// fields. Trailing JSON after the value is rejected by encoding/json.
func emitStrictDecode(b *strings.Builder, td TypeDef, schemaPrefix string) {
	fmt.Fprintf(b, "func (v *%s) UnmarshalJSON(data []byte) error {\n", td.Name)
	b.WriteString("\tvar raw map[string]json.RawMessage\n")
	b.WriteString("\tif err := json.Unmarshal(data, &raw); err != nil {\n\t\treturn err\n\t}\n")
	b.WriteString("\tif raw == nil { return fmt.Errorf(\"expected object, got null\") }\n")
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
	b.WriteString("\tvar next alias\n")
	custom := make([]FieldDef, 0)
	for _, f := range td.Fields {
		if needsIntegralDecode(f.Type) {
			custom = append(custom, f)
		}
	}
	if len(custom) == 0 {
		b.WriteString("\tif err := json.Unmarshal(data, &next); err != nil { return err }\n")
	} else {
		b.WriteString("\tremaining := make(map[string]json.RawMessage, len(raw))\n")
		b.WriteString("\tfor key, value := range raw { remaining[key] = value }\n")
		for _, f := range custom {
			fmt.Fprintf(b, "\tdelete(remaining, %q)\n", f.JSONTag)
		}
		b.WriteString("\trest, err := json.Marshal(remaining)\n\tif err != nil { return err }\n")
		b.WriteString("\tif err := json.Unmarshal(rest, &next); err != nil { return err }\n")
		counter := 0
		for _, f := range custom {
			fmt.Fprintf(b, "\tif encoded, ok := raw[%q]; ok {\n", f.JSONTag)
			target := "next." + f.GoName
			if f.Required {
				emitIntegralDecode(b, f.Type, "encoded", target, "\t\t", schemaPrefix, &counter)
			} else {
				local := fmt.Sprintf("decoded%d", counter)
				counter++
				fmt.Fprintf(b, "\t\tvar %s %s\n", local, baseGoType(f.Type))
				emitIntegralDecode(b, f.Type, "encoded", local, "\t\t", schemaPrefix, &counter)
				fmt.Fprintf(b, "\t\tnext.%s = &%s\n", f.GoName, local)
			}
			b.WriteString("\t}\n")
		}
	}
	fmt.Fprintf(b, "\t*v = %s(next)\n", td.Name)
	b.WriteString("\treturn nil\n")
	b.WriteString("}\n\n")
}

func needsIntegralDecode(ft FieldType) bool {
	if (ft.Kind == KindScalar || ft.Kind == KindEnum) && ft.Scalar == ScalarInt {
		return true
	}
	return ft.Kind == KindArray && ft.Elem != nil && needsIntegralDecode(*ft.Elem)
}

func emitIntegralDecode(b *strings.Builder, ft FieldType, raw, target, indent, schemaPrefix string, counter *int) {
	if ft.Kind == KindScalar || ft.Kind == KindEnum {
		value := fmt.Sprintf("integer%d", *counter)
		*counter++
		fmt.Fprintf(b, "%s%s, integral, safe := %sStructuralIntParts(%s)\n", indent, value, schemaPrefix, raw)
		fmt.Fprintf(b, "%sif !integral || !safe { return fmt.Errorf(\"integer value is not a safe mathematical integer\") }\n", indent)
		fmt.Fprintf(b, "%s%s = %s(%s)\n", indent, target, baseGoType(ft), value)
		return
	}
	if ft.Kind != KindArray || ft.Elem == nil {
		fmt.Fprintf(b, "%sif err := json.Unmarshal(%s, &%s); err != nil { return err }\n", indent, raw, target)
		return
	}
	items := fmt.Sprintf("items%d", *counter)
	*counter++
	fmt.Fprintf(b, "%svar %s []json.RawMessage\n", indent, items)
	fmt.Fprintf(b, "%sif err := json.Unmarshal(%s, &%s); err != nil { return err }\n", indent, raw, items)
	itemRaw := fmt.Sprintf("itemRaw%d", *counter)
	item := fmt.Sprintf("item%d", *counter)
	*counter++
	fmt.Fprintf(b, "%sfor _, %s := range %s {\n", indent, itemRaw, items)
	fmt.Fprintf(b, "%s\tvar %s %s\n", indent, item, baseGoType(*ft.Elem))
	emitIntegralDecode(b, *ft.Elem, itemRaw, item, indent+"\t", schemaPrefix, counter)
	fmt.Fprintf(b, "%s\t%s = append(%s, %s)\n", indent, target, target, item)
	fmt.Fprintf(b, "%s}\n", indent)
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
		switch td.Kind {
		case KindObject:
			emitValidateInto(b, spec, td.Name, td.Fields)
		case KindUnion:
			emitUnionValidateInto(b, spec, td)
			for _, branch := range td.Branches {
				emitValidateInto(b, spec, td.Name+"Value"+codegen.GoFieldName(branch.Wire), branch.Fields)
			}
		}
	}
}

func emitUnionValidateInto(b *strings.Builder, _ *Spec, td TypeDef) {
	fmt.Fprintf(b, "func (v %s) validate(path string, issues *[]Issue) {\n", td.Name)
	b.WriteString("\tswitch branch := v.Value.(type) {\n")
	for _, branch := range td.Branches {
		variant := td.Name + "Value" + codegen.GoFieldName(branch.Wire)
		fmt.Fprintf(b, "\tcase *%s:\n\t\tbranch.validate(path, issues)\n", variant)
	}
	b.WriteString("\tcase nil:\n\t\t*issues = append(*issues, Issue{Code: \"required\", Path: path})\n")
	b.WriteString("\t}\n}\n\n")
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
	if f.Type.Ref != "" && f.Type.Scalar == ScalarString {
		val = "string(" + val + ")"
	}
	if !alwaysPresent(f) {
		fmt.Fprintf(b, "\tif %s {\n", present)
	}
	emitScalarConstraints(b, f, val, tagPath)
	if !alwaysPresent(f) {
		b.WriteString("\t}\n")
	}
}

func emitArrayValidate(b *strings.Builder, spec *Spec, f FieldDef, gn, tagPath string) {
	array := gn
	if !f.Required {
		fmt.Fprintf(b, "\tif %s != nil {\n", gn)
		array = "*" + gn
	}
	if f.MinItems != nil {
		fmt.Fprintf(b, "\tif len(%s) < %d {\n", array, *f.MinItems)
		emitIssue(b, "minItems", tagPath)
		b.WriteString("\t}\n")
	}
	if f.MaxItems != nil {
		fmt.Fprintf(b, "\tif len(%s) > %d {\n", array, *f.MaxItems)
		emitIssue(b, "maxItems", tagPath)
		b.WriteString("\t}\n")
	}

	if f.Type.Elem != nil && needsTypedElementValidation(*f.Type.Elem) {
		fmt.Fprintf(b, "\tfor i, it := range %s {\n", array)
		emitArrayElementValidate(b, spec, *f.Type.Elem, "it", tagPath+` + "/" + strconv.Itoa(i)`)
		b.WriteString("\t}\n")
	}
	if !f.Required {
		b.WriteString("\t}\n")
	}
}

func needsTypedElementValidation(elem FieldType) bool {
	if elem.Kind == KindObject || elem.Kind == KindUnion || elem.Kind == KindEnum {
		return true
	}
	c := elem.Constraints
	if c.MinLength != nil || c.MaxLength != nil || c.Minimum != nil || c.Maximum != nil || c.ExclusiveMinimum != nil || c.ExclusiveMaximum != nil || c.MinItems != nil || c.MaxItems != nil || c.HasConst {
		return true
	}
	return elem.Kind == KindArray && elem.Elem != nil && needsTypedElementValidation(*elem.Elem)
}

func emitArrayElementValidate(b *strings.Builder, spec *Spec, elem FieldType, value, elemPath string) {
	switch elem.Kind {
	case KindObject, KindUnion:
		fmt.Fprintf(b, "\t\t%s.validate(%s, issues)\n", value, elemPath)
	case KindEnum:
		enum := spec.Lookup(elem.Ref)
		if enum == nil {
			return
		}
		fmt.Fprintf(b, "\t\tswitch %s {\n", value)
		for _, allowed := range enum.Values {
			fmt.Fprintf(b, "\t\tcase %s%s:\n", enum.Name, allowed.Name)
		}
		b.WriteString("\t\tdefault:\n")
		fmt.Fprintf(b, "\t\t\t*issues = append(*issues, Issue{Code: \"enum\", Path: %s})\n", elemPath)
		b.WriteString("\t\t}\n")
		constraint := FieldDef{Type: FieldType{Kind: KindScalar, Scalar: enum.Scalar}, Constraints: elem.Constraints}
		scalarValue := value
		if enum.Scalar == ScalarString {
			scalarValue = "string(" + value + ")"
		}
		emitScalarConstraints(b, constraint, scalarValue, elemPath)
	case KindScalar:
		emitScalarConstraints(b, FieldDef{Type: elem, Constraints: elem.Constraints}, value, elemPath)
	case KindArray:
		if elem.Constraints.MinItems != nil {
			fmt.Fprintf(b, "\t\tif len(%s) < %d { *issues = append(*issues, Issue{Code: \"minItems\", Path: %s}) }\n", value, *elem.Constraints.MinItems, elemPath)
		}
		if elem.Constraints.MaxItems != nil {
			fmt.Fprintf(b, "\t\tif len(%s) > %d { *issues = append(*issues, Issue{Code: \"maxItems\", Path: %s}) }\n", value, *elem.Constraints.MaxItems, elemPath)
		}
		if elem.Elem != nil {
			b.WriteString("\t\tfor j, nested := range " + value + " {\n")
			emitArrayElementValidate(b, spec, *elem.Elem, "nested", elemPath+` + "/" + strconv.Itoa(j)`)
			b.WriteString("\t\t}\n")
		}
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

// emitEnumValidate checks both membership and constraints attached to an
// enum-typed field. Enum fields retain a named Go type, so use the enum's
// underlying scalar solely to select the applicable constraint checks.
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
	constraintField := f
	constraintField.Type = FieldType{Kind: KindScalar, Scalar: enum.Scalar}
	constraintValue := val
	if enum.Scalar == ScalarString {
		constraintValue = "string(" + val + ")"
	}
	emitScalarConstraints(b, constraintField, constraintValue, tagPath)
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
		if f.ExclusiveMinimum != nil {
			fmt.Fprintf(b, "\tif float64(%s) <= %v {\n", val, *f.ExclusiveMinimum)
			emitIssue(b, "exclusiveMinimum", tagPath)
			b.WriteString("\t}\n")
		}
		if f.ExclusiveMaximum != nil {
			fmt.Fprintf(b, "\tif float64(%s) >= %v {\n", val, *f.ExclusiveMaximum)
			emitIssue(b, "exclusiveMaximum", tagPath)
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
		if f.ExclusiveMinimum != nil {
			fmt.Fprintf(b, "\tif %s <= %v {\n", val, *f.ExclusiveMinimum)
			emitIssue(b, "exclusiveMinimum", tagPath)
			b.WriteString("\t}\n")
		}
		if f.ExclusiveMaximum != nil {
			fmt.Fprintf(b, "\tif %s >= %v {\n", val, *f.ExclusiveMaximum)
			emitIssue(b, "exclusiveMaximum", tagPath)
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
