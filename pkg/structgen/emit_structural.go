package structgen

import (
	"fmt"
	"strings"
)

// rawGen carries the schema symbol prefix and spec needed while rendering the
// dependency-free raw structural validator.
type rawGen struct {
	S    string
	spec *Spec
}

func (g rawGen) fn(name string) string { return "svalidate" + name }

// enumWires returns the wire values for a direct enum field or array-of-enum elem.
func (g rawGen) enumWires(ft FieldType) []string {
	var ref string
	switch ft.Kind {
	case KindEnum:
		ref = ft.Ref
	case KindArray:
		if ft.Elem != nil && ft.Elem.Kind == KindEnum {
			ref = ft.Elem.Ref
		}
	}
	if ref == "" {
		return nil
	}
	td := g.spec.Lookup(ref)
	if td == nil {
		return nil
	}
	out := make([]string, 0, len(td.Values))
	for _, v := range td.Values {
		out = append(out, v.Wire)
	}
	return out
}

// emitStructural emits the profile's raw structural validation API.
// It emits structural issues, structural errors, validation, and decoding.
// Raw values keep omitted optional properties distinct from explicit null.
// Well-formed structural failures return sorted, deduplicated issues.
func emitStructural(b *strings.Builder, spec *Spec, opts EmitOptions) {
	S := opts.SchemaName
	if S == "" {
		S = spec.RootName
	}
	rootType := opts.RootTypeName
	if rootType == "" {
		rootType = spec.RootName
	}
	g := rawGen{S: S, spec: spec}

	emitStructuralTypes(b, S)
	emitValidateJSON(b, g, spec)
	emitDecode(b, S, rootType)

	// Raw walker for the root object, then every named object/union type.
	g.emitRawObject(b, spec.RootName, spec.Root)
	for _, td := range spec.Types {
		switch td.Kind {
		case KindObject:
			g.emitRawObject(b, td.Name, td.Fields)
		case KindUnion:
			g.emitRawUnion(b, td)
		}
	}
}

func emitStructuralTypes(b *strings.Builder, S string) {
	fmt.Fprintf(b, "// %sStructuralIssue describes one structural validation problem.\n", S)
	fmt.Fprintf(b, "type %sStructuralIssue struct {\n", S)
	b.WriteString("\tSource     string\n")
	b.WriteString("\tCode       string\n")
	b.WriteString("\tPath       string\n")
	b.WriteString("\tSchemaPath string\n")
	b.WriteString("\tMessage    string\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "// %sStructuralError carries normalized structural issues from Decode.\n", S)
	fmt.Fprintf(b, "type %sStructuralError struct {\n\tIssues []%sStructuralIssue\n}\n\n", S, S)
	fmt.Fprintf(b, "func (e *%sStructuralError) Error() string {\n", S)
	b.WriteString("\tif e == nil {\n\t\treturn \"<nil>\"\n\t}\n")
	b.WriteString("\tparts := make([]string, 0, len(e.Issues))\n")
	b.WriteString("\tfor _, i := range e.Issues {\n\t\tparts = append(parts, i.Code+\" at \"+i.Path)\n\t}\n")
	b.WriteString("\treturn strings.Join(parts, \"; \")\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "// %sStructuralIssueAt builds a normalized json-schema issue.\n", S)
	fmt.Fprintf(b, "func %sStructuralIssueAt(code, path, schemaPath string) %sStructuralIssue {\n", S, S)
	fmt.Fprintf(b, "\treturn %sStructuralIssue{Source: \"json-schema\", Code: code, Path: path, SchemaPath: schemaPath, Message: code}\n", S)
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "// %sStructuralKind reports the JSON value kind of a raw token.\n", S)
	fmt.Fprintf(b, "func %sStructuralKind(raw json.RawMessage) string {\n", S)
	b.WriteString("\tfor _, c := range raw {\n")
	b.WriteString("\t\tswitch c {\n")
	b.WriteString("\t\tcase ' ', '\\t', '\\n', '\\r':\n\t\t\tcontinue\n")
	b.WriteString("\t\tcase '{':\n\t\t\treturn \"object\"\n")
	b.WriteString("\t\tcase '[':\n\t\t\treturn \"array\"\n")
	b.WriteString("\t\tcase '\"':\n\t\t\treturn \"string\"\n")
	b.WriteString("\t\tcase 't', 'f':\n\t\t\treturn \"boolean\"\n")
	b.WriteString("\t\tcase 'n':\n\t\t\treturn \"null\"\n")
	b.WriteString("\t\tdefault:\n\t\t\treturn \"number\"\n")
	b.WriteString("\t\t}\n\t}\n")
	b.WriteString("\treturn \"number\"\n}\n\n")

	fmt.Fprintf(b, "// %sStructuralIntParts splits a JSON number literal into an integer\n", S)
	fmt.Fprintf(b, "// value, whether it was an integer literal, and whether it lies within\n")
	fmt.Fprintf(b, "// JavaScript's safe-integer range (|v| <= 2^53-1).\n")
	fmt.Fprintf(b, "func %sStructuralIntParts(raw json.RawMessage) (val int64, isInt, safe bool) {\n", S)
	b.WriteString("\ts := strings.TrimSpace(string(raw))\n")
	b.WriteString("\tif s == \"\" {\n\t\treturn 0, false, false\n\t}\n")
	b.WriteString("\tif strings.ContainsAny(s, \".eE\") {\n\t\treturn 0, false, false\n\t}\n")
	b.WriteString("\tbody := s\n")
	b.WriteString("\tif strings.HasPrefix(body, \"-\") {\n\t\tbody = body[1:]\n\t}\n")
	b.WriteString("\tfor i := 0; i < len(body); i++ {\n")
	b.WriteString("\t\tif body[i] < '0' || body[i] > '9' {\n\t\t\treturn 0, false, false\n\t\t}\n\t}\n")
	b.WriteString("\tn, err := strconv.ParseInt(s, 10, 64)\n")
	b.WriteString("\tif err != nil {\n\t\treturn 0, true, false\n\t} // integral literal overflowing int64 is unsafe\n")
	b.WriteString("\tconst maxSafe = 9007199254740991 // 2^53 - 1\n")
	b.WriteString("\tif n < -maxSafe || n > maxSafe {\n\t\treturn n, true, false\n\t}\n")
	b.WriteString("\treturn n, true, true\n}\n\n")

	fmt.Fprintf(b, "// %sStructuralSort dedupes issues by (source, code, path) and sorts by path, then code.\n", S)
	fmt.Fprintf(b, "func %sStructuralSort(issues []%sStructuralIssue) []%sStructuralIssue {\n", S, S, S)
	b.WriteString("\tseen := make(map[string]bool, len(issues))\n")
	fmt.Fprintf(b, "\tout := make([]%sStructuralIssue, 0, len(issues))\n", S)
	b.WriteString("\tfor _, i := range issues {\n")
	b.WriteString("\t\tk := i.Source + \"\\x00\" + i.Code + \"\\x00\" + i.Path\n")
	b.WriteString("\t\tif seen[k] {\n\t\t\tcontinue\n\t\t}\n")
	b.WriteString("\t\tseen[k] = true\n\t\tout = append(out, i)\n\t}\n")
	b.WriteString("\tsort.Slice(out, func(a, b int) bool {\n")
	b.WriteString("\t\tif out[a].Path != out[b].Path {\n\t\t\treturn out[a].Path < out[b].Path\n\t\t}\n")
	b.WriteString("\t\treturn out[a].Code < out[b].Code\n\t})\n")
	b.WriteString("\treturn out\n}\n\n")
}

// emitValidateJSON renders the Validate<S>JSON entrypoint.
func emitValidateJSON(b *strings.Builder, g rawGen, spec *Spec) {
	S := g.S
	fmt.Fprintf(b, "// Validate%[1]sJSON validates raw JSON and returns normalized structural\n", S)
	fmt.Fprintf(b, "// issues. It returns a non-nil error only for malformed JSON or trailing\n")
	fmt.Fprintf(b, "// JSON values; well-formed but structurally invalid input yields issues.\n")
	fmt.Fprintf(b, "func Validate%[1]sJSON(data []byte) ([]%[1]sStructuralIssue, error) {\n", S)
	b.WriteString("\tif !json.Valid(data) {\n\t\treturn nil, fmt.Errorf(\"invalid JSON: malformed or trailing data\")\n\t}\n")
	b.WriteString("\tvar raw json.RawMessage\n")
	b.WriteString("\tif err := json.Unmarshal(data, &raw); err != nil {\n\t\treturn nil, err\n\t}\n")
	fmt.Fprintf(b, "\tissues := []%sStructuralIssue{}\n", S)
	fmt.Fprintf(b, "\t%s(raw, \"\", \"\", &issues)\n", g.fn(spec.RootName))
	fmt.Fprintf(b, "\treturn %sStructuralSort(issues), nil\n", S)
	b.WriteString("}\n\n")
}

// emitDecode renders the Decode<S> entrypoint.
func emitDecode(b *strings.Builder, S, rootType string) {
	fmt.Fprintf(b, "// Decode%[1]s validates raw JSON structurally, then decodes it into %[2]s.\n", S, rootType)
	fmt.Fprintf(b, "// If raw validation finds issues it returns a *%[1]sStructuralError from\n", S)
	fmt.Fprintf(b, "// which callers recover normalized issues via errors.As.\n")
	fmt.Fprintf(b, "func Decode%[1]s(data []byte) (%[2]s, error) {\n", S, rootType)
	fmt.Fprintf(b, "\tissues, err := Validate%[1]sJSON(data)\n", S)
	b.WriteString("\tif err != nil {\n")
	fmt.Fprintf(b, "\t\tvar zero %s\n", rootType)
	b.WriteString("\t\treturn zero, err\n\t}\n")
	fmt.Fprintf(b, "\tif len(issues) > 0 {\n")
	fmt.Fprintf(b, "\t\tvar zero %s\n", rootType)
	fmt.Fprintf(b, "\t\treturn zero, &%sStructuralError{Issues: issues}\n\t}\n", S)
	fmt.Fprintf(b, "\tvar out %s\n", rootType)
	b.WriteString("\tif err := json.Unmarshal(data, &out); err != nil {\n\t\treturn out, err\n\t}\n")
	b.WriteString("\treturn out, nil\n}\n\n")
}

// emitRawObject renders an object validator for a named object type.
func (g rawGen) emitRawObject(b *strings.Builder, typeName string, fields []FieldDef) {
	S := g.S
	fmt.Fprintf(b, "func %s(raw json.RawMessage, path, schemaPath string, issues *[]%sStructuralIssue) {\n", g.fn(typeName), S)
	b.WriteString("\tif " + S + `StructuralKind(raw) != "object" {` + "\n")
	fmt.Fprintf(b, "\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", path, schemaPath))\n", S)
	b.WriteString("\t\treturn\n\t}\n")
	b.WriteString("\tvar m map[string]json.RawMessage\n")
	b.WriteString("\t_ = json.Unmarshal(raw, &m)\n")
	for _, f := range fields {
		if f.Required {
			g.emitRawRequired(b, f)
		}
	}
	g.emitRawUnknown(b, fields)
	for _, f := range fields {
		g.emitRawField(b, f)
	}
	b.WriteString("}\n\n")
}

func (g rawGen) emitRawRequired(b *strings.Builder, f FieldDef) {
	S := g.S
	fmt.Fprintf(b, "\tif _, ok := m[%q]; !ok {\n", f.JSONTag)
	fmt.Fprintf(b, "\t\t*issues = append(*issues, %sStructuralIssueAt(\"required\", path+\"/\"+escapePtr(%q), schemaPath))\n", S, f.JSONTag)
	b.WriteString("\t}\n")
}

func (g rawGen) emitRawUnknown(b *strings.Builder, fields []FieldDef) {
	S := g.S
	b.WriteString("\tallowed := map[string]bool{\n")
	for _, f := range fields {
		fmt.Fprintf(b, "\t\t%q: true,\n", f.JSONTag)
	}
	b.WriteString("\t}\n")
	b.WriteString("\tfor key := range m {\n")
	b.WriteString("\t\tif allowed[key] {\n\t\t\tcontinue\n\t\t}\n")
	fmt.Fprintf(b, "\t\t*issues = append(*issues, %sStructuralIssueAt(\"additionalProperties\", path+\"/\"+escapePtr(key), schemaPath))\n", S)
	b.WriteString("\t}\n")
}

func (g rawGen) emitRawField(b *strings.Builder, f FieldDef) {
	fmt.Fprintf(b, "\tif r, ok := m[%q]; ok {\n", f.JSONTag)
	fmt.Fprintf(b, "\t\tfpath := path + \"/\" + escapePtr(%q)\n", f.JSONTag)
	fmt.Fprintf(b, "\t\tfspath := schemaPath + \"/properties/\" + escapePtr(%q)\n", f.JSONTag)
	g.emitRawFieldBody(b, f)
	b.WriteString("\t}\n")
}

// emitRawFieldBody renders type-specific validation for a present field. It relies
// on identifiers r (json.RawMessage), fpath, fspath being in scope.
func (g rawGen) emitRawFieldBody(b *strings.Builder, f FieldDef) {
	switch f.Type.Kind {
	case KindObject, KindUnion:
		S := g.S
		fmt.Fprintf(b, "\t\tif %sStructuralKind(r) == \"object\" {\n", S)
		fmt.Fprintf(b, "\t\t\t%s(r, fpath, fspath, issues)\n", g.fn(f.Type.Ref))
		b.WriteString("\t\t} else {\n")
		fmt.Fprintf(b, "\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", fpath, fspath))\n", S)
		b.WriteString("\t\t}\n")
	case KindArray:
		g.emitRawArrayBody(b, f)
	case KindEnum:
		g.emitRawEnumBody(b, f)
	case KindScalar:
		g.emitRawScalarBody(b, f)
	}
}

func (g rawGen) emitRawArrayBody(b *strings.Builder, f FieldDef) {
	S := g.S
	fmt.Fprintf(b, "\t\tif %sStructuralKind(r) != \"array\" {\n", S)
	fmt.Fprintf(b, "\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", fpath, fspath))\n", S)
	b.WriteString("\t\t} else {\n")
	b.WriteString("\t\t\tvar arr []json.RawMessage\n")
	b.WriteString("\t\t\t_ = json.Unmarshal(r, &arr)\n")
	if f.MinItems != nil {
		fmt.Fprintf(b, "\t\t\tif len(arr) < %d {\n", *f.MinItems)
		fmt.Fprintf(b, "\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"minItems\", fpath, fspath))\n", S)
		b.WriteString("\t\t\t}\n")
	}
	if f.MaxItems != nil {
		fmt.Fprintf(b, "\t\t\tif len(arr) > %d {\n", *f.MaxItems)
		fmt.Fprintf(b, "\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"maxItems\", fpath, fspath))\n", S)
		b.WriteString("\t\t\t}\n")
	}
	b.WriteString("\t\t\tfor i, el := range arr {\n")
	b.WriteString("\t\t\t\tepath := fpath + \"/\" + strconv.Itoa(i)\n")
	b.WriteString("\t\t\t\tespath := fspath + \"/items\"\n")
	g.emitRawArrayElemBody(b, f.Type.Elem)
	b.WriteString("\t\t\t}\n")
	b.WriteString("\t\t}\n")
}

func (g rawGen) emitRawArrayElemBody(b *strings.Builder, elem *FieldType) {
	S := g.S
	switch elem.Kind {
	case KindObject, KindUnion:
		fmt.Fprintf(b, "\t\t\t\t%s(el, epath, espath, issues)\n", g.fn(elem.Ref))
	case KindEnum:
		wires := g.enumWires(*elem)
		fmt.Fprintf(b, "\t\t\t\tif %sStructuralKind(el) == \"string\" {\n", S)
		b.WriteString("\t\t\t\t\tvar ev string\n")
		b.WriteString("\t\t\t\t\t_ = json.Unmarshal(el, &ev)\n")
		b.WriteString("\t\t\t\t\tswitch ev {\n")
		for _, v := range wires {
			fmt.Fprintf(b, "\t\t\t\t\tcase %q:\n", v)
		}
		fmt.Fprintf(b, "\t\t\t\t\tdefault:\n\t\t\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"enum\", epath, espath))\n", S)
		b.WriteString("\t\t\t\t\t}\n")
		b.WriteString("\t\t\t\t} else {\n")
		fmt.Fprintf(b, "\t\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", epath, espath))\n", S)
		b.WriteString("\t\t\t\t}\n")
	case KindScalar:
		switch elem.Scalar {
		case ScalarString, ScalarBool, ScalarNumber:
			want := "string"
			if elem.Scalar == ScalarBool {
				want = "boolean"
			} else if elem.Scalar == ScalarNumber {
				want = "number"
			}
			fmt.Fprintf(b, "\t\t\t\tif %sStructuralKind(el) != %q {\n", S, want)
			fmt.Fprintf(b, "\t\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", epath, espath))\n", S)
			b.WriteString("\t\t\t\t}\n")
		case ScalarInt:
			fmt.Fprintf(b, "\t\t\t\t_, isInt, isSafe := %sStructuralIntParts(el)\n", S)
			b.WriteString("\t\t\t\tif !isInt {\n")
			fmt.Fprintf(b, "\t\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", epath, espath))\n", S)
			b.WriteString("\t\t\t\t} else if !isSafe {\n")
			fmt.Fprintf(b, "\t\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"safeInteger\", epath, espath))\n", S)
			b.WriteString("\t\t\t\t}\n")
		}
	}
}

func (g rawGen) emitRawEnumBody(b *strings.Builder, f FieldDef) {
	S := g.S
	wires := g.enumWires(f.Type)
	fmt.Fprintf(b, "\t\tif %sStructuralKind(r) == \"string\" {\n", S)
	b.WriteString("\t\t\tvar ev string\n")
	b.WriteString("\t\t\t_ = json.Unmarshal(r, &ev)\n")
	b.WriteString("\t\t\tswitch ev {\n")
	for _, v := range wires {
		fmt.Fprintf(b, "\t\t\tcase %q:\n", v)
	}
	fmt.Fprintf(b, "\t\t\tdefault:\n\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"enum\", fpath, fspath))\n", S)
	b.WriteString("\t\t\t}\n")
	b.WriteString("\t\t} else {\n")
	fmt.Fprintf(b, "\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", fpath, fspath))\n", S)
	b.WriteString("\t\t}\n")
}

func (g rawGen) emitRawScalarBody(b *strings.Builder, f FieldDef) {
	S := g.S
	switch f.Type.Scalar {
	case ScalarString:
		b.WriteString("\t\tswitch " + S + `StructuralKind(r) {` + "\n")
		b.WriteString("\t\tcase \"string\":\n")
		b.WriteString("\t\t\tvar sv string\n")
		b.WriteString("\t\t\t_ = json.Unmarshal(r, &sv)\n")
		if f.MinLength != nil {
			fmt.Fprintf(b, "\t\t\tif utf8.RuneCountInString(sv) < %d {\n", *f.MinLength)
			fmt.Fprintf(b, "\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"minLength\", fpath, fspath))\n", S)
			b.WriteString("\t\t\t}\n")
		}
		if f.MaxLength != nil {
			fmt.Fprintf(b, "\t\t\tif utf8.RuneCountInString(sv) > %d {\n", *f.MaxLength)
			fmt.Fprintf(b, "\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"maxLength\", fpath, fspath))\n", S)
			b.WriteString("\t\t\t}\n")
		}
		if f.HasConst {
			if s, ok := f.Const.(string); ok {
				fmt.Fprintf(b, "\t\t\tif sv != %q {\n", s)
				fmt.Fprintf(b, "\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"const\", fpath, fspath))\n", S)
				b.WriteString("\t\t\t}\n")
			}
		}
		b.WriteString("\t\tdefault:\n")
		fmt.Fprintf(b, "\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", fpath, fspath))\n", S)
		b.WriteString("\t\t}\n")
	case ScalarBool:
		b.WriteString("\t\tswitch " + S + `StructuralKind(r) {` + "\n")
		b.WriteString("\t\tcase \"boolean\":\n")
		if f.HasConst {
			if bv, ok := f.Const.(bool); ok {
				b.WriteString("\t\t\tvar bvx bool\n")
				b.WriteString("\t\t\t_ = json.Unmarshal(r, &bvx)\n")
				fmt.Fprintf(b, "\t\t\tif bvx != %v {\n", bv)
				fmt.Fprintf(b, "\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"const\", fpath, fspath))\n", S)
				b.WriteString("\t\t\t}\n")
			}
		}
		b.WriteString("\t\tdefault:\n")
		fmt.Fprintf(b, "\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", fpath, fspath))\n", S)
		b.WriteString("\t\t}\n")
	case ScalarNumber:
		b.WriteString("\t\tswitch " + S + `StructuralKind(r) {` + "\n")
		b.WriteString("\t\tcase \"number\":\n")
		b.WriteString("\t\t\tvar nv float64\n")
		b.WriteString("\t\t\t_ = json.Unmarshal(r, &nv)\n")
		if f.Minimum != nil {
			fmt.Fprintf(b, "\t\t\tif nv < %v {\n", *f.Minimum)
			fmt.Fprintf(b, "\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"minimum\", fpath, fspath))\n", S)
			b.WriteString("\t\t\t}\n")
		}
		if f.Maximum != nil {
			fmt.Fprintf(b, "\t\t\tif nv > %v {\n", *f.Maximum)
			fmt.Fprintf(b, "\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"maximum\", fpath, fspath))\n", S)
			b.WriteString("\t\t\t}\n")
		}
		if f.HasConst {
			if cv, ok := f.Const.(float64); ok {
				fmt.Fprintf(b, "\t\t\tif nv != %v {\n", cv)
				fmt.Fprintf(b, "\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"const\", fpath, fspath))\n", S)
				b.WriteString("\t\t\t}\n")
			}
		}
		b.WriteString("\t\tdefault:\n")
		fmt.Fprintf(b, "\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", fpath, fspath))\n", S)
		b.WriteString("\t\t}\n")
	case ScalarInt:
		b.WriteString("\t\tswitch " + S + `StructuralKind(r) {` + "\n")
		b.WriteString("\t\tcase \"number\":\n")
		fmt.Fprintf(b, "\t\t\tival, isInt, isSafe := %sStructuralIntParts(r)\n", S)
		b.WriteString("\t\t\t_ = ival\n")
		b.WriteString("\t\t\tif !isInt {\n")
		fmt.Fprintf(b, "\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", fpath, fspath))\n", S)
		b.WriteString("\t\t\t} else if !isSafe {\n")
		fmt.Fprintf(b, "\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"safeInteger\", fpath, fspath))\n", S)
		b.WriteString("\t\t\t} else {\n")
		if f.Minimum != nil {
			fmt.Fprintf(b, "\t\t\t\tif float64(ival) < %v {\n", *f.Minimum)
			fmt.Fprintf(b, "\t\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"minimum\", fpath, fspath))\n", S)
			b.WriteString("\t\t\t\t}\n")
		}
		if f.Maximum != nil {
			fmt.Fprintf(b, "\t\t\t\tif float64(ival) > %v {\n", *f.Maximum)
			fmt.Fprintf(b, "\t\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"maximum\", fpath, fspath))\n", S)
			b.WriteString("\t\t\t\t}\n")
		}
		if f.HasConst {
			if cv, ok := f.Const.(float64); ok {
				fmt.Fprintf(b, "\t\t\t\tif ival != int64(%v) {\n", cv)
				fmt.Fprintf(b, "\t\t\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"const\", fpath, fspath))\n", S)
				b.WriteString("\t\t\t\t}\n")
			}
		}
		b.WriteString("\t\t\t}\n")
		b.WriteString("\t\tdefault:\n")
		fmt.Fprintf(b, "\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", fpath, fspath))\n", S)
		b.WriteString("\t\t}\n")
	}
}

// emitRawUnion renders the discriminator-based validator for a tagged union.
func (g rawGen) emitRawUnion(b *strings.Builder, td TypeDef) {
	S := g.S
	disc := td.Discriminator
	fmt.Fprintf(b, "func %s(raw json.RawMessage, path, schemaPath string, issues *[]%sStructuralIssue) {\n", g.fn(td.Name), S)
	b.WriteString("\tif " + S + `StructuralKind(raw) != "object" {` + "\n")
	fmt.Fprintf(b, "\t\t*issues = append(*issues, %sStructuralIssueAt(\"type\", path, schemaPath))\n", S)
	b.WriteString("\t\treturn\n\t}\n")
	b.WriteString("\tvar m map[string]json.RawMessage\n")
	b.WriteString("\t_ = json.Unmarshal(raw, &m)\n")
	fmt.Fprintf(b, "\tdiscPath := path + \"/\" + escapePtr(%q)\n", disc)
	fmt.Fprintf(b, "\tdiscSPath := schemaPath + \"/properties/\" + escapePtr(%q)\n", disc)
	fmt.Fprintf(b, "\tdvRaw, ok := m[%q]\n", disc)
	b.WriteString("\tif !ok {\n")
	fmt.Fprintf(b, "\t\t*issues = append(*issues, %sStructuralIssueAt(\"required\", discPath, discSPath))\n", S)
	b.WriteString("\t\treturn\n\t}\n")
	fmt.Fprintf(b, "\tif %sStructuralKind(dvRaw) != \"string\" {\n", S)
	fmt.Fprintf(b, "\t\t*issues = append(*issues, %sStructuralIssueAt(\"discriminator\", discPath, discSPath))\n", S)
	b.WriteString("\t\treturn\n\t}\n")
	b.WriteString("\tvar dv string\n")
	b.WriteString("\t_ = json.Unmarshal(dvRaw, &dv)\n")
	b.WriteString("\tswitch dv {\n")
	for _, br := range td.Branches {
		fmt.Fprintf(b, "\tcase %q:\n", br.Wire)
		g.emitRawUnionBranch(b, disc, br)
	}
	b.WriteString("\tdefault:\n")
	fmt.Fprintf(b, "\t\t*issues = append(*issues, %sStructuralIssueAt(\"discriminator\", discPath, discSPath))\n", S)
	b.WriteString("\t}\n")
	b.WriteString("}\n\n")
}

// emitRawUnionBranch renders validation for one selected branch: unknown-property,
// branch-required, and per-field checks (discriminator excluded).
func (g rawGen) emitRawUnionBranch(b *strings.Builder, disc string, br UnionBranch) {
	S := g.S
	b.WriteString("\t\tallowed := map[string]bool{\n")
	for _, f := range br.Fields {
		fmt.Fprintf(b, "\t\t\t%q: true,\n", f.JSONTag)
	}
	b.WriteString("\t\t}\n")
	b.WriteString("\t\tfor key := range m {\n")
	b.WriteString("\t\t\tif allowed[key] {\n\t\t\t\tcontinue\n\t\t\t}\n")
	fmt.Fprintf(b, "\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"additionalProperties\", path+\"/\"+escapePtr(key), schemaPath))\n", S)
	b.WriteString("\t\t}\n")
	for _, f := range br.Fields {
		if !f.Required || f.Name == disc {
			continue
		}
		fmt.Fprintf(b, "\t\tif _, ok := m[%q]; !ok {\n", f.Name)
		fmt.Fprintf(b, "\t\t\t*issues = append(*issues, %sStructuralIssueAt(\"required\", path+\"/\"+escapePtr(%q), schemaPath))\n", S, f.Name)
		b.WriteString("\t\t}\n")
	}
	// Per-field raw validation (discriminator handled by the branch dispatch above).
	for _, f := range br.Fields {
		if f.Name == disc {
			continue
		}
		fmt.Fprintf(b, "\t\tif r, ok := m[%q]; ok {\n", f.Name)
		fmt.Fprintf(b, "\t\t\tfpath := path + \"/\" + escapePtr(%q)\n", f.Name)
		fmt.Fprintf(b, "\t\t\tfspath := schemaPath + \"/properties/\" + escapePtr(%q)\n", f.Name)
		g.emitRawFieldBody(b, f)
		b.WriteString("\t\t}\n")
	}
}
