package structgen

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
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

	rootType := codegen.GoFieldName(rootName)
	if rootType == "" {
		rootType = "Profile"
	}
	b := &builder{byName: make(map[string]int), defNames: make(map[string]string), rootName: rootType}

	// Pre-register all $defs names so forward references within an acyclic DAG resolve.
	if defsRaw, ok := root["$defs"]; ok {
		var defs map[string]json.RawMessage
		if err := json.Unmarshal(defsRaw, &defs); err == nil {
			for _, name := range sortedKeys(defs) {
				typed := rootType + codegen.GoFieldName(name)
				b.defNames[name] = typed
				b.register(TypeDef{Name: typed})
			}
			for _, name := range sortedKeys(defs) {
				typed := b.defNames[name]
				if targetWire, ok := refOnlyDefinition(defs[name]); ok {
					target := b.defNames[targetWire]
					if target == "" {
						return nil, fmt.Errorf("$defs/%s: unsupported $ref %q", name, "#/$defs/"+targetWire)
					}
					idx := b.byName[typed]
					b.types[idx] = TypeDef{Name: typed, AliasRef: target}
					continue
				}
				ft, err := b.resolve(defs[name], typed, "", true)
				if err != nil {
					return nil, fmt.Errorf("$defs/%s: %w", name, err)
				}
				if ft.Ref != typed {
					idx := b.byName[typed]
					b.types[idx] = TypeDef{Name: typed, Kind: ft.Kind, Scalar: ft.Scalar, Elem: ft.Elem, Constraints: ft.Constraints}
				}
			}
			if err := b.finalizeAliases(); err != nil {
				return nil, err
			}
		}
	}

	// Build the root object.
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
	types    []TypeDef
	byName   map[string]int
	defNames map[string]string
	rootName string
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

// finalizeAliases copies each ref-only definition's target shape while retaining
// a Go alias edge for emission. Profile validation rejects cycles, but Build also
// detects them so direct structgen callers never observe incomplete placeholders.
func (b *builder) finalizeAliases() error {
	state := make(map[string]uint8)
	var resolve func(string) error
	resolve = func(name string) error {
		td := b.at(name)
		if td == nil || td.AliasRef == "" {
			return nil
		}
		switch state[name] {
		case 1:
			return fmt.Errorf("$defs alias cycle at %s", name)
		case 2:
			return nil
		}
		state[name] = 1
		target := b.at(td.AliasRef)
		if target == nil {
			return fmt.Errorf("$defs alias %s has missing target %s", name, td.AliasRef)
		}
		if err := resolve(target.Name); err != nil {
			return err
		}
		target = b.at(td.AliasRef)
		if target.Kind == "" {
			return fmt.Errorf("$defs alias %s has unresolved target %s", name, td.AliasRef)
		}
		aliasRef := td.AliasRef
		copy := *target
		copy.Name = name
		copy.AliasRef = aliasRef
		*b.at(name) = copy
		state[name] = 2
		return nil
	}
	for _, td := range b.types {
		if err := resolve(td.Name); err != nil {
			return err
		}
	}
	return nil
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
		if ft.Ref != "" {
			if td := b.at(ft.Ref); td != nil {
				ft.Kind = actualKind(td.Kind)
				ft.Scalar = td.Scalar
				ft.Elem = td.Elem
				ft.Constraints = td.Constraints
			}
		}
		if ft.Kind == KindArray {
			fixFT(ft.Elem)
		}
	}
	for i := range b.types {
		fixFT(b.types[i].Elem)
		for j := range b.types[i].Fields {
			fixFT(&b.types[i].Fields[j].Type)
			if ref := b.types[i].Fields[j].Type.Ref; ref != "" {
				if target := b.at(ref); target != nil && (target.Kind == KindScalar || target.Kind == KindArray || target.Kind == KindEnum) {
					b.types[i].Fields[j].Constraints = target.Constraints
					b.types[i].Fields[j].Type.Constraints = target.Constraints
				}
			}
		}
		for j := range b.types[i].Branches {
			for k := range b.types[i].Branches[j].Fields {
				field := &b.types[i].Branches[j].Fields[k]
				fixFT(&field.Type)
				if target := b.at(field.Type.Ref); target != nil && (target.Kind == KindScalar || target.Kind == KindArray || target.Kind == KindEnum) {
					field.Constraints = target.Constraints
					field.Type.Constraints = target.Constraints
				}
			}
		}
	}
}

func actualKind(k Kind) Kind { return k }

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
		wireName := refWireName(ref)
		name := b.defNames[wireName]
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
		return withConstraints(constScalar(node["const"]), node), nil
	}

	// Tagged union: oneOf with a shared const discriminator.
	if hasNode(node, "oneOf") {
		return b.resolveUnion(node, hint)
	}

	// Primitive enums become named types with the matching scalar underlying type.
	for schemaType, scalar := range map[string]Scalar{
		"string": ScalarString, "boolean": ScalarBool, "integer": ScalarInt, "number": ScalarNumber,
	} {
		if isScalar(node, schemaType) {
			if hasNode(node, "enum") {
				if !isDef {
					hint += "Value"
				}
				return b.resolveEnum(node, hint, scalar)
			}
			return withConstraints(FieldType{Kind: KindScalar, Scalar: scalar}, node), nil
		}
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
		return withConstraints(FieldType{Kind: KindArray, Elem: &elem}, node), nil
	}

	// Object: declared object, object with properties, or an empty schema. All
	// become a named struct — never `any`.
	if isScalar(node, "object") || hasNode(node, "properties") || len(node) == 0 {
		b.register(TypeDef{Name: hint})
		if _, err := b.resolveObjectFields(node, hint, jsonName); err != nil {
			return FieldType{}, err
		}
		return withConstraints(FieldType{Kind: KindObject, Ref: hint}, node), nil
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
		ft, err := b.resolve(props[propName], name+codegen.GoFieldName(propName), propName, false)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", propName, err)
		}
		fields = append(fields, FieldDef{
			Name:        propName,
			GoName:      codegen.GoFieldName(propName),
			JSONTag:     propName,
			Required:    required[propName],
			Type:        ft,
			Constraints: ft.Constraints,
		})
	}
	b.types[idx].Fields = fields
	return fields, nil
}

// resolveEnum fills a named primitive enum TypeDef and returns its field type.
func (b *builder) resolveEnum(node map[string]json.RawMessage, hint string, scalar Scalar) (FieldType, error) {
	var rawValues []json.RawMessage
	if err := json.Unmarshal(node["enum"], &rawValues); err != nil || len(rawValues) == 0 {
		return FieldType{}, fmt.Errorf("enum is not a non-empty array")
	}
	var constraintField FieldDef
	attachConstraints(&constraintField, node)
	td := TypeDef{Name: hint, Kind: KindEnum, JSONName: hint, Scalar: scalar, Constraints: constraintField.Constraints}
	for i, raw := range rawValues {
		value, err := enumValue(raw, scalar)
		if err != nil {
			return FieldType{}, err
		}
		name := fmt.Sprintf("Value%d", i+1)
		if s, ok := value.(string); ok {
			name = codegen.GoFieldName(s)
		} else if v, ok := value.(bool); ok {
			if v {
				name = "True"
			} else {
				name = "False"
			}
		}
		td.Values = append(td.Values, EnumValue{Name: name, Wire: value})
	}
	idx := b.register(td)
	b.types[idx] = td
	return withConstraints(FieldType{Kind: KindEnum, Scalar: scalar, Ref: hint}, node), nil
}

func enumValue(raw json.RawMessage, scalar Scalar) (any, error) {
	switch scalar {
	case ScalarString:
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return nil, fmt.Errorf("enum value is not a string")
		}
		return value, nil
	case ScalarBool:
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return nil, fmt.Errorf("enum value is not a boolean")
		}
		return value, nil
	case ScalarInt:
		value, ok := parseSchemaRat(strings.TrimSpace(string(raw)))
		if !ok || !value.IsInt() || !value.Num().IsInt64() {
			return nil, fmt.Errorf("enum value is not an integer")
		}
		return value.Num().Int64(), nil
	case ScalarNumber:
		var value float64
		if json.Unmarshal(raw, &value) != nil {
			return nil, fmt.Errorf("enum value is not a number")
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported enum scalar %q", scalar)
	}
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

		// Identify and verify the shared required string discriminator before
		// resolving branch fields. The discriminator value owns every branch-local
		// generated type name, so same-wire fields may have incompatible schemas.
		branchDiscriminator := ""
		for _, propName := range sortedKeys(props) {
			if req[propName] && hasStringConst(props[propName]) {
				branchDiscriminator = propName
				break
			}
		}
		if discriminator == "" {
			discriminator = branchDiscriminator
		}
		if branchDiscriminator == "" || branchDiscriminator != discriminator {
			return FieldType{}, fmt.Errorf("oneOf union %q has no shared required string discriminator", hint)
		}
		d, ok := discConst(props, discriminator)
		if !ok {
			return FieldType{}, fmt.Errorf("oneOf union %q branch lacks discriminator const %q", hint, discriminator)
		}
		if _, dup := branchIdx[d]; dup {
			return FieldType{}, fmt.Errorf("oneOf union %q: duplicate discriminator value %q", hint, d)
		}
		branchIdx[d] = len(branchDefs)

		var brFields []FieldDef
		branchOwner := hint + codegen.GoFieldName(d)
		for _, propName := range sortedKeys(props) {
			ft, err := b.resolve(props[propName], branchOwner+codegen.GoFieldName(propName), propName, false)
			if err != nil {
				return FieldType{}, err
			}
			brFields = append(brFields, FieldDef{
				Name:        propName,
				GoName:      codegen.GoFieldName(propName),
				JSONTag:     propName,
				Required:    req[propName],
				Type:        ft,
				Constraints: ft.Constraints,
			})
		}
		branchDefs = append(branchDefs, UnionBranch{Wire: d, Fields: brFields})
		branchOrder = append(branchOrder, d)
	}
	if discriminator == "" {
		return FieldType{}, fmt.Errorf("oneOf union %q has no required string discriminator", hint)
	}

	// The discriminator becomes a named enum type (e.g. ActionKind) built from the
	// union's branch const values.
	enumName := hint + "Kind"
	enumT := TypeDef{Name: enumName, Kind: KindEnum, JSONName: hint, Scalar: ScalarString}
	for _, v := range uniqueSorted(branchOrder) {
		enumT.Values = append(enumT.Values, EnumValue{Name: codegen.GoFieldName(v), Wire: v})
	}
	enumIdx := b.register(enumT)
	b.types[enumIdx] = enumT
	// Per-branch Required lists (wire names), minus the discriminator key itself.
	for i := range branchDefs {
		var reqNames []string
		for j := range branchDefs[i].Fields {
			bf := &branchDefs[i].Fields[j]
			if bf.Name == discriminator {
				bf.Type = FieldType{Kind: KindEnum, Ref: enumName}
			}
			if bf.Required && bf.Name != discriminator {
				reqNames = append(reqNames, bf.Name)
			}
		}
		branchDefs[i].Required = reqNames
	}

	td := TypeDef{Name: hint, Kind: KindUnion, JSONName: hint, Discriminator: discriminator, Branches: branchDefs}
	idx := b.register(td)
	b.types[idx] = td
	return withConstraints(FieldType{Kind: KindUnion, Ref: hint}, node), nil
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

func withConstraints(ft FieldType, node map[string]json.RawMessage) FieldType {
	var fd FieldDef
	attachConstraints(&fd, node)
	ft.Constraints = fd.Constraints
	return ft
}

func attachConstraints(fd *FieldDef, node map[string]json.RawMessage) {
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
	if v, ok := numField(node, "exclusiveMinimum"); ok {
		fd.ExclusiveMinimum = &v
	}
	if v, ok := numField(node, "exclusiveMaximum"); ok {
		fd.ExclusiveMaximum = &v
	}
	if c, ok := node["const"]; ok {
		fd.HasConst = true
		_ = json.Unmarshal(c, &fd.Const)
	}
}

// ---- helpers ----

func refOnlyDefinition(raw json.RawMessage) (string, bool) {
	var node map[string]json.RawMessage
	if json.Unmarshal(raw, &node) != nil || len(node) != 1 {
		return "", false
	}
	var ref string
	if json.Unmarshal(node["$ref"], &ref) != nil {
		return "", false
	}
	wire := refWireName(ref)
	return wire, wire != ""
}

func refWireName(ref string) string {
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return ""
	}
	token := strings.TrimPrefix(ref, prefix)
	if token == "" || strings.Contains(token, "/") {
		return ""
	}
	var decoded strings.Builder
	for i := 0; i < len(token); i++ {
		if token[i] != '~' {
			decoded.WriteByte(token[i])
			continue
		}
		if i+1 >= len(token) {
			return ""
		}
		i++
		switch token[i] {
		case '0':
			decoded.WriteByte('~')
		case '1':
			decoded.WriteByte('/')
		default:
			return ""
		}
	}
	return decoded.String()
}

func kindOfRef(k Kind) Kind {
	if k == "" {
		return KindObject // corrected by finalize after forward target resolution
	}
	return k
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
	value, ok := parseSchemaRat(strings.TrimSpace(string(raw)))
	if !ok || !value.IsInt() || !value.Num().IsInt64() {
		return 0, false
	}
	integer := value.Num().Int64()
	if int64(int(integer)) != integer {
		return 0, false
	}
	return int(integer), true
}

func parseSchemaRat(text string) (*big.Rat, bool) {
	negative := strings.HasPrefix(text, "-")
	if negative {
		text = text[1:]
	}
	exponent := 0
	if at := strings.IndexAny(text, "eE"); at >= 0 {
		parsed, err := strconv.Atoi(text[at+1:])
		if err != nil || parsed > 4096 || parsed < -4096 {
			return nil, false
		}
		exponent = parsed
		text = text[:at]
	}
	fractionDigits := 0
	if dot := strings.IndexByte(text, '.'); dot >= 0 {
		fractionDigits = len(text) - dot - 1
		text = text[:dot] + text[dot+1:]
	}
	exponent -= fractionDigits
	integer := new(big.Int)
	if _, ok := integer.SetString(text, 10); !ok {
		return nil, false
	}
	if negative {
		integer.Neg(integer)
	}
	magnitude := exponent
	if magnitude < 0 {
		magnitude = -magnitude
	}
	power := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(magnitude)), nil)
	if exponent >= 0 {
		integer.Mul(integer, power)
		return new(big.Rat).SetInt(integer), true
	}
	return new(big.Rat).SetFrac(integer, power), true
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
