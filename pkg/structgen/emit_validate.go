package structgen

import (
	"fmt"
	"strings"
)

// emitStrictDecode renders an UnmarshalJSON for an object type that rejects
// unknown properties, missing required fields, and explicit null on required
// fields. Trailing JSON after the value is rejected by encoding/json's decoder.
func emitStrictDecode(b *strings.Builder, td TypeDef) {
	fmt.Fprintf(b, "func (v *%s) UnmarshalJSON(data []byte) error {\n", td.Name)
	b.WriteString("\tvar raw map[string]json.RawMessage\n")
	b.WriteString("\tif err := json.Unmarshal(data, &raw); err != nil {\n\t\treturn err\n\t}\n")
	b.WriteString("\tfor key := range raw {\n\t\tswitch key {\n")
	for _, f := range td.Fields {
		fmt.Fprintf(b, "\t\tcase %q:\n", f.JSONTag)
	}
	b.WriteString("\t\tdefault:\n\t\t\treturn fmt.Errorf(\"unknown field %q\", key)\n\t\t}\n\t}\n")
	for _, f := range td.Fields {
		if f.Required {
			emitRequiredCheck(b, f)
		}
	}
	fmt.Fprintf(b, "\ttype alias %s\n", td.Name)
	b.WriteString("\treturn json.Unmarshal(data, (*alias)(v))\n")
	b.WriteString("}\n\n")
}

func emitRequiredCheck(b *strings.Builder, f FieldDef) {
	fmt.Fprintf(b, "\tif r, ok := raw[%q]; ok {\n", f.JSONTag)
	b.WriteString("\t\tif len(r) == 4 && string(r) == \"null\" {\n")
	fmt.Fprintf(b, "\t\t\treturn fmt.Errorf(%q)\n", "required field \""+f.JSONTag+"\" is null")
	b.WriteString("\t\t}\n")
	b.WriteString("\t} else {\n")
	fmt.Fprintf(b, "\t\treturn fmt.Errorf(%q)\n", "missing required field \""+f.JSONTag+"\"")
	b.WriteString("\t}\n")
}

// emitValidation renders Validate() (root) and validateInto() (every object/union),
// applying constraints with RFC 6901 JSON Pointer paths and normalized codes.
func emitValidation(b *strings.Builder, spec *Spec) {
	b.WriteString("// Issue describes one validation problem with an RFC 6901 JSON Pointer path.\n")
	b.WriteString("type Issue struct {\n\tCode string `json:\"code\"`\n\tPath string `json:\"path\"`\n}\n\n")

	// References/union index paths need strconv; rune length needs utf8; handled
	// by import selection in Emit (see specNeedsImports).

	// Validate method on the root.
	fmt.Fprintf(b, "func (v %s) Validate() []Issue {\n", spec.RootName)
	b.WriteString("\tvar issues []Issue\n")
	fmt.Fprintf(b, "\tv.validate(\"\", &issues)\n")
	b.WriteString("\treturn issues\n")
	b.WriteString("}\n\n")

	emitValidateInto(b, spec.RootName, spec.Root)
	for _, td := range spec.Types {
		if td.Kind == KindObject || td.Kind == KindUnion {
			emitValidateInto(b, td.Name, td.Fields)
		}
	}
}

func emitValidateInto(b *strings.Builder, typeName string, fields []FieldDef) {
	fmt.Fprintf(b, "func (v %s) validate(path string, issues *[]Issue) {\n", typeName)
	for _, f := range fields {
		emitFieldValidate(b, f)
	}
	b.WriteString("}\n\n")
}

func emitFieldValidate(b *strings.Builder, f FieldDef) {
	gn := "v." + f.GoName
	tagPath := `path + "/" + ` + strconvQ(f.JSONTag)

	// Recursion for reference and array types.
	switch f.Type.Kind {
	case KindObject, KindUnion:
		if f.Required {
			fmt.Fprintf(b, "\t%s.validate(%s, issues)\n", gn, tagPath)
		} else {
			fmt.Fprintf(b, "\tif %s != nil {\n\t\t%s.validate(%s, issues)\n\t}\n", gn, gn, tagPath)
		}
		return
	case KindArray:
		if f.Type.Elem.IsReference() {
			fmt.Fprintf(b, "\tfor i, it := range %s {\n\t\tit.validate(path+\"/%s/\"+strconv.Itoa(i), issues)\n\t}\n", gn, f.JSONTag)
		}
	}

	// Presence guard for non-array scalar/reference-optional handled above.
	present, val := presentAndValue(f)
	if !alwaysPresent(f) {
		fmt.Fprintf(b, "\tif %s {\n", present)
	}
	emitConstraints(b, f, val, tagPath)
	if !alwaysPresent(f) {
		b.WriteString("\t}\n")
	}
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

func emitConstraints(b *strings.Builder, f FieldDef, val, tagPath string) {
	issue := func(code string) {
		fmt.Fprintf(b, "\t*issues = append(*issues, Issue{Code: %q, Path: %s})\n", code, tagPath)
	}
	switch f.Type.Kind {
	case KindArray:
		if f.MinItems != nil {
			fmt.Fprintf(b, "\tif len(%s) < %d {\n", val, *f.MinItems)
			issue("minItems")
			b.WriteString("\t}\n")
		}
		if f.MaxItems != nil {
			fmt.Fprintf(b, "\tif len(%s) > %d {\n", val, *f.MaxItems)
			issue("maxItems")
			b.WriteString("\t}\n")
		}
	case KindScalar:
		switch f.Type.Scalar {
		case ScalarString:
			if f.MinLength != nil {
				fmt.Fprintf(b, "\tif utf8.RuneCountInString(%s) < %d {\n", val, *f.MinLength)
				issue("minLength")
				b.WriteString("\t}\n")
			}
			if f.MaxLength != nil {
				fmt.Fprintf(b, "\tif utf8.RuneCountInString(%s) > %d {\n", val, *f.MaxLength)
				issue("maxLength")
				b.WriteString("\t}\n")
			}
		case ScalarInt, ScalarNumber:
			if f.Minimum != nil {
				fmt.Fprintf(b, "\tif %s < %v {\n", val, *f.Minimum)
				issue("minimum")
				b.WriteString("\t}\n")
			}
			if f.Maximum != nil {
				fmt.Fprintf(b, "\tif %s > %v {\n", val, *f.Maximum)
				issue("maximum")
				b.WriteString("\t}\n")
			}
		}
	}
	if f.HasConst {
		fmt.Fprintf(b, "\tif %s != %s {\n", val, constLiteral(f))
		issue("const")
		b.WriteString("\t}\n")
	}
}

// constLiteral renders a typed Go literal for a const value.
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
	}
	// fall back to a quoted string (handles non-scalar consts leniently)
	if s, ok := f.Const.(string); ok {
		return strconvQ(s)
	}
	return `""`
}

func strconvQ(s string) string {
	return fmt.Sprintf("%q", s)
}
