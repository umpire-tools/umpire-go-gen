package structgen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/umpire-tools/umpire-go-gen/pkg/codegen"
)

// Build maps a validated JSON Schema 2020-12 valueSchema into a structural IR.
// rootName is the Go name for the root object (e.g. the schema name). Deterministic:
// $defs types are emitted first (definition order), then root and inline types in
// first-encounter order.
func Build(valueSchema json.RawMessage, rootName string) (*Spec, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(valueSchema, &root); err != nil {
		return nil, fmt.Errorf("valueSchema is not an object: %w", err)
	}

	b := &builder{byName: make(map[string]int)}

	// Pre-register all $defs names so forward references within an acyclic DAG resolve.
	if defsRaw, ok := root["$defs"]; ok {
		var defs map[string]json.RawMessage
		if err := json.Unmarshal(defsRaw, &defs); err == nil {
			for _, name := range sortedKeys(defs) {
				b.register(TypeDef{Name: codegen.GoFieldName(name)})
			}
			for _, name := range sortedKeys(defs) {
				typed := codegen.GoFieldName(name)
				if _, err := b.resolve(defs[name], typed, "", true); err != nil {
					return nil, fmt.Errorf("$defs/%s: %w", name, err)
				}
			}
		}
	}

	// Build the root object.
	rootType := codegen.GoFieldName(rootName)
	if rootType == "" {
		rootType = "Profile"
	}
	rootFields, err := b.resolveObjectFields(root, rootType, "")
	if err != nil {
		return nil, fmt.Errorf("valueSchema root: %w", err)
	}

	// All types are now fully built; correct any field reference kinds that were
	// forced to KindObject by forward $def references.
	b.finalize()

	// Emit the root as Root (not as a separate named type), then every other named type.
	var types []TypeDef
	for _, td := range b.types {
		if td.Name == rootType {
			continue
		}
		types = append(types, td)
	}

	return &Spec{RootName: rootType, Root: rootFields, Types: types}, nil
}

type builder struct {
	types  []TypeDef
	byName map[string]int
}

func (b *builder) register(t TypeDef) int {
	if idx, ok := b.byName[t.Name]; ok {
		return idx
	}
	b.byName[t.Name] = len(b.types)
	b.types = append(b.types, t)
	return len(b.types) - 1
}

func (b *builder) at(name string) *TypeDef {
	idx, ok := b.byName[name]
	if !ok {
		return nil
	}
	return &b.types[idx]
}

// finalize corrects field reference kinds now that every named type is fully
// built. Forward $def references were resolved against empty placeholders and
// forced to KindObject; this restores the true object/enum/union kind so
// emission and validation recursion see the right type family.
func (b *builder) finalize() {
	var fixFT func(ft *FieldType)
	fixFT = func(ft *FieldType) {
		if ft == nil {
			return
		}
		if ft.Kind == KindArray {
			fixFT(ft.Elem)
			return
		}
		if ft.Ref != "" {
			if td := b.at(ft.Ref); td != nil {
				ft.Kind = actualKind(td.Kind)
			}
		}
	}
	for i := range b.types {
		for j := range b.types[i].Fields {
			fixFT(&b.types[i].Fields[j].Type)
		}
	}
}

func actualKind(k Kind) Kind {
	switch k {
	case KindEnum:
		return KindEnum
	case KindUnion:
		return KindUnion
	default:
		return KindObject
	}
}

// resolve returns the FieldType for a schema node and, for object/union/enum
// nodes, fills/registers the corresponding named TypeDef.
func (b *builder) resolve(raw json.RawMessage, hint, jsonName string, isDef bool) (FieldType, error) {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(raw, &node); err != nil {
		return FieldType{}, fmt.Errorf("schema node is not an object")
	}

	// $ref: local reference to a $def (pre-registered as a placeholder in Build).
	if refRaw, ok := node["$ref"]; ok {
		var ref string
		if err := json.Unmarshal(refRaw, &ref); err != nil {
			return FieldType{}, fmt.Errorf("$ref is not a string")
		}
		name := refName(ref)
		if name == "" {
			return FieldType{}, fmt.Errorf("unsupported $ref %q", ref)
		}
		if b.at(name) == nil {
			b.register(TypeDef{Name: name})
		}
		return FieldType{Kind: kindOfRef(b.at(name).Kind), Ref: name}, nil
	}

	// const-only node (no type): infer the scalar kind from the const value.
	if hasNode(node, "const") && !hasNode(node, "type") {
		return constScalar(node["const"]), nil
	}

	// Tagged union: oneOf with a shared const discriminator.
	if hasNode(node, "oneOf") {
		return b.resolveUnion(node, hint)
	}

	// Named enum: string + enum list.
	if isScalar(node, "string") {
		if hasNode(node, "enum") {
			return b.resolveEnum(node, hint)
		}
		return FieldType{Kind: KindScalar, Scalar: ScalarString}, nil
	}
	if isScalar(node, "boolean") {
		return FieldType{Kind: KindScalar, Scalar: ScalarBool}, nil
	}
	if isScalar(node, "integer") {
		return FieldType{Kind: KindScalar, Scalar: ScalarInt}, nil
	}
	if isScalar(node, "number") {
		return FieldType{Kind: KindScalar, Scalar: ScalarNumber}, nil
	}

	// Array: homogeneous items → slice.
	if isScalar(node, "array") {
		elem := FieldType{Kind: KindScalar, Scalar: ScalarString}
		if itemsRaw, ok := node["items"]; ok {
			e, err := b.resolve(itemsRaw, hint+"Item", jsonName+"Item", false)
			if err != nil {
				return FieldType{}, err
			}
			elem = e
		}
		return FieldType{Kind: KindArray, Elem: &elem}, nil
	}

	// Object: declared object, object with properties, or an empty schema. All
	// become a named struct — never `any`.
	if isScalar(node, "object") || hasNode(node, "properties") || len(node) == 0 {
		b.register(TypeDef{Name: hint})
		if _, err := b.resolveObjectFields(node, hint, jsonName); err != nil {
			return FieldType{}, err
		}
		return FieldType{Kind: KindObject, Ref: hint}, nil
	}

	return FieldType{}, fmt.Errorf("unsupported schema shape at %q", hint)
}

// resolveObjectFields fills the named TypeDef for an object and returns its fields.
func (b *builder) resolveObjectFields(node map[string]json.RawMessage, name, jsonName string) ([]FieldDef, error) {
	var props map[string]json.RawMessage
	if p, ok := node["properties"]; ok {
		if err := json.Unmarshal(p, &props); err != nil {
			return nil, fmt.Errorf("properties is not an object")
		}
	}
	required := requiredSet(node)

	idx := b.register(TypeDef{Name: name})
	b.types[idx].Kind = KindObject
	b.types[idx].JSONName = jsonName

	fields := make([]FieldDef, 0, len(props))
	for _, propName := range sortedKeys(props) {
		ft, err := b.resolve(props[propName], codegen.GoFieldName(propName), propName, false)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", propName, err)
		}
		fd := FieldDef{
			Name:     propName,
			GoName:   codegen.GoFieldName(propName),
			JSONTag:  propName,
			Required: required[propName],
			Type:     ft,
		}
		attachConstraints(&fd, props[propName])
		fields = append(fields, fd)
	}
	b.types[idx].Fields = fields
	return fields, nil
}

// resolveEnum fills a named enum TypeDef and returns its field type.
func (b *builder) resolveEnum(node map[string]json.RawMessage, hint string) (FieldType, error) {
	var vals []string
	if err := json.Unmarshal(node["enum"], &vals); err != nil {
		return FieldType{}, fmt.Errorf("enum is not an array of strings")
	}
	td := TypeDef{Name: hint, Kind: KindEnum, JSONName: hint}
	for _, v := range vals {
		td.Values = append(td.Values, EnumValue{Name: codegen.GoFieldName(v), Wire: v})
	}
	idx := b.register(td)
	b.types[idx] = td
	return FieldType{Kind: KindEnum, Ref: hint}, nil
}

// resolveUnion fills a named union TypeDef and returns its field type. The union
// merges the discriminator (shared required string-const property) plus every
// branch field; non-discriminator fields are optional in the merged struct.
// Field and discriminator property names are keyed by their ORIGINAL JSON wire
// name so arbitrary profile-accepted identifiers survive round-tripping and
// strict decoding (including the discriminator key). Each branch additionally
// keeps its own property schemas (with constraints) for per-branch raw
// validation and branch-required strictness.
func (b *builder) resolveUnion(node map[string]json.RawMessage, hint string) (FieldType, error) {
	var oneOf []json.RawMessage
	if err := json.Unmarshal(node["oneOf"], &oneOf); err != nil {
		return FieldType{}, fmt.Errorf("oneOf is not an array")
	}

	fieldType := map[string]FieldType{} // keyed by original wire name
	var fieldOrder []string
	addField := func(n string, ft FieldType) {
		if _, ok := fieldType[n]; !ok {
			fieldOrder = append(fieldOrder, n)
		}
		fieldType[n] = ft
	}

	discriminator := ""
	var branchOrder []string
	var branchDefs []UnionBranch
	branchIdx := map[string]int{} // discriminator wire -> index in branchDefs
	for _, brRaw := range oneOf {
		var br map[string]json.RawMessage
		if err := json.Unmarshal(brRaw, &br); err != nil || !hasNode(br, "properties") {
			return FieldType{}, fmt.Errorf("oneOf branch is not an object with properties")
		}
		var props map[string]json.RawMessage
		if err := json.Unmarshal(br["properties"], &props); err != nil {
			return FieldType{}, fmt.Errorf("branch properties is not an object")
		}
		req := requiredSet(br)

		var brFields []FieldDef
		for _, propName := range sortedKeys(props) {
			ft, err := b.resolve(props[propName], codegen.GoFieldName(propName), propName, false)
			if err != nil {
				return FieldType{}, err
			}
			addField(propName, ft)
			bf := FieldDef{
				Name:     propName,
				GoName:   codegen.GoFieldName(propName),
				JSONTag:  propName,
				Required: req[propName],
				Type:     ft,
			}
			attachConstraints(&bf, props[propName])
			brFields = append(brFields, bf)
		}
		// Identify the discriminator: the first required property with a string const.
		if discriminator == "" {
			for _, propName := range sortedKeys(props) {
				if req[propName] && hasStringConst(props[propName]) {
					discriminator = propName
					break
				}
			}
		}

		// Record this branch (its const value and per-branch schemas/requirements).
		d, ok := discConst(props, discriminator)
		if !ok {
			return FieldType{}, fmt.Errorf("oneOf union %q branch lacks discriminator const %q", hint, discriminator)
		}
		if _, dup := branchIdx[d]; dup {
			return FieldType{}, fmt.Errorf("oneOf union %q: duplicate discriminator value %q", hint, d)
		}
		branchIdx[d] = len(branchDefs)
		branchDefs = append(branchDefs, UnionBranch{Wire: d, Fields: brFields})
		branchOrder = append(branchOrder, d)
	}
	if discriminator == "" {
		return FieldType{}, fmt.Errorf("oneOf union %q has no required string discriminator", hint)
	}

	// The discriminator becomes a named enum type (e.g. ActionKind) built from the
	// union's branch const values.
	enumName := hint + "Kind"
	enumT := TypeDef{Name: enumName, Kind: KindEnum, JSONName: hint}
	for _, v := range uniqueSorted(branchOrder) {
		enumT.Values = append(enumT.Values, EnumValue{Name: codegen.GoFieldName(v), Wire: v})
	}
	enumIdx := b.register(enumT)
	b.types[enumIdx] = enumT
	fieldType[discriminator] = FieldType{Kind: KindEnum, Ref: enumName}

	// Per-branch Required lists (wire names), minus the discriminator key itself.
	for i := range branchDefs {
		var reqNames []string
		for _, bf := range branchDefs[i].Fields {
			if bf.Required && bf.Name != discriminator {
				reqNames = append(reqNames, bf.Name)
			}
		}
		branchDefs[i].Required = reqNames
	}

	td := TypeDef{Name: hint, Kind: KindUnion, JSONName: hint, Discriminator: discriminator, Branches: branchDefs}
	for _, n := range fieldOrder {
		td.Fields = append(td.Fields, FieldDef{
			Name:     n,
			GoName:   codegen.GoFieldName(n),
			JSONTag:  n,
			Required: n == discriminator, // only the discriminator is required at the merged-struct level
			Type:     fieldType[n],
		})
	}
	idx := b.register(td)
	b.types[idx] = td
	return FieldType{Kind: KindUnion, Ref: hint}, nil
}

// discConst returns the string const value of the named discriminator property
// within a branch's properties, if present.
func discConst(props map[string]json.RawMessage, wire string) (string, bool) {
	raw, ok := props[wire]
	if !ok {
		return "", false
	}
	var node map[string]json.RawMessage
	if json.Unmarshal(raw, &node) != nil {
		return "", false
	}
	c, ok := node["const"]
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(c, &s) != nil {
		return "", false
	}
	return s, true
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// attachConstraints copies validation-relevant keywords from a property schema
// onto a FieldDef.
func constScalar(raw json.RawMessage) FieldType {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return FieldType{Kind: KindScalar, Scalar: ScalarString}
	}
	switch val := v.(type) {
	case bool:
		return FieldType{Kind: KindScalar, Scalar: ScalarBool}
	case float64:
		s := ScalarNumber
		if val == float64(int64(val)) {
			s = ScalarInt
		}
		return FieldType{Kind: KindScalar, Scalar: s}
	default:
		return FieldType{Kind: KindScalar, Scalar: ScalarString}
	}
}

func attachConstraints(fd *FieldDef, raw json.RawMessage) {
	var node map[string]json.RawMessage
	if json.Unmarshal(raw, &node) != nil {
		return
	}
	if v, ok := intField(node, "minLength"); ok {
		fd.MinLength = &v
	}
	if v, ok := intField(node, "maxLength"); ok {
		fd.MaxLength = &v
	}
	if v, ok := intField(node, "minItems"); ok {
		fd.MinItems = &v
	}
	if v, ok := intField(node, "maxItems"); ok {
		fd.MaxItems = &v
	}
	if v, ok := numField(node, "minimum"); ok {
		fd.Minimum = &v
	}
	if v, ok := numField(node, "maximum"); ok {
		fd.Maximum = &v
	}
	if c, ok := node["const"]; ok {
		fd.HasConst = true
		_ = json.Unmarshal(c, &fd.Const)
	}
}

// ---- helpers ----

func refName(ref string) string {
	const p = "#/$defs/"
	if strings.HasPrefix(ref, p) {
		return codegen.GoFieldName(strings.TrimPrefix(ref, p))
	}
	return ""
}

func kindOfRef(k Kind) Kind {
	if k == KindEnum || k == KindUnion {
		return k
	}
	return KindObject
}

func isScalar(node map[string]json.RawMessage, want string) bool {
	tRaw, ok := node["type"]
	if !ok {
		return false
	}
	var t string
	if json.Unmarshal(tRaw, &t) != nil {
		return false
	}
	return t == want
}

func hasNode(m map[string]json.RawMessage, key string) bool {
	_, ok := m[key]
	return ok
}

func requiredSet(node map[string]json.RawMessage) map[string]bool {
	out := map[string]bool{}
	if r, ok := node["required"]; ok {
		var req []string
		if json.Unmarshal(r, &req) == nil {
			for _, k := range req {
				out[k] = true
			}
		}
	}
	return out
}

func hasStringConst(raw json.RawMessage) bool {
	var node map[string]json.RawMessage
	if json.Unmarshal(raw, &node) != nil {
		return false
	}
	c, ok := node["const"]
	if !ok {
		return false
	}
	var s string
	return json.Unmarshal(c, &s) == nil
}

func intField(node map[string]json.RawMessage, key string) (int, bool) {
	raw, ok := node[key]
	if !ok {
		return 0, false
	}
	var f float64
	if json.Unmarshal(raw, &f) != nil {
		return 0, false
	}
	return int(f), true
}

func numField(node map[string]json.RawMessage, key string) (float64, bool) {
	raw, ok := node[key]
	if !ok {
		return 0, false
	}
	var f float64
	if json.Unmarshal(raw, &f) != nil {
		return 0, false
	}
	return f, true
}

func wireName(pascal string) string {
	if pascal == "" {
		return ""
	}
	r := []rune(pascal)
	r[0] = []rune(strings.ToLower(string(r[0])))[0]
	return string(r)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
