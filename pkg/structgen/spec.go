package structgen

// Kind classifies a named generated type or a field's structural type.
type Kind string

const (
	KindScalar Kind = "scalar" // primitive scalar (string/bool/int/number)
	KindObject Kind = "object" // object with declared properties → named struct
	KindEnum   Kind = "enum"   // string + enum → named type + constants
	KindUnion  Kind = "union"  // oneOf tagged union → discriminator-based decoding
	KindArray  Kind = "array"  // array with homogeneous items → slice
)

// Scalar is the underlying primitive type for a scalar field or enum.
type Scalar string

const (
	ScalarString Scalar = "string"
	ScalarBool   Scalar = "boolean"
	ScalarInt    Scalar = "integer"
	ScalarNumber Scalar = "number"
)

// TypeDef is a named type emitted by structgen. Exactly one family applies
// depending on Kind.
// UnionBranch captures one tagged-union branch: its discriminator wire value and
// the property names that branch requires (for branch-level strict decoding).
type UnionBranch struct {
	Wire     string   // discriminator const value, e.g. "manual"
	Required []string // branch-required JSON property names
}

// TypeDef is a named type emitted by structgen. Exactly one family applies
// depending on Kind.
type TypeDef struct {
	Name          string        // Go type name (PascalCase), e.g. "Node", "ActionKind"
	Kind          Kind          // object | enum | union | array-alias
	JSONName      string        // original JSON property name for inline types; "" for $defs/root
	Fields        []FieldDef    // object / union: declared fields (union includes all branches)
	Values        []EnumValue   // enum: allowed wire values in declared order
	Discriminator string        // union: the shared discriminator property (e.g. "kind")
	Branches      []UnionBranch // union: per-discriminator branch requirements
	Elem          *FieldType    // array-alias: the element type
}

// EnumValue is one allowed value of an enum type.
type EnumValue struct {
	Name string // Go constant name (PascalCase) e.g. "WorkflowPipeline"
	Wire string // original wire value, e.g. "pipeline"
}

// FieldDef is one field of an object/union type, or of the root.
type FieldDef struct {
	Name     string    // original JSON property name, e.g. "workflowType"
	GoName   string    // PascalCase Go identifier, e.g. "WorkflowType"
	JSONTag  string    // lowercase json tag text
	Required bool      // whether the property appears in the object's required array
	Type     FieldType // structural type
	// Constraints used by validation (chunk 5).
	MinLength *int
	MaxLength *int
	Minimum   *float64
	Maximum   *float64
	MinItems  *int
	MaxItems  *int
	Const     any // const value when HasConst
	HasConst  bool
}

// FieldType is the structural type of a field: either a primitive scalar, an
// array of another field type, or a reference to a named type.
type FieldType struct {
	Kind   Kind
	Scalar Scalar     // Kind == KindScalar
	Ref    string     // Kind in {object, enum, union} → named TypeDef.Name
	Elem   *FieldType // Kind == KindArray
}

// IsReference reports whether the field type points at a named generated type.
func (ft FieldType) IsReference() bool {
	return ft.Kind == KindObject || ft.Kind == KindEnum || ft.Kind == KindUnion
}

// Spec is the complete structural IR for one valueSchema.
type Spec struct {
	RootName string     // root struct name, e.g. "Workflow"
	Root     []FieldDef // root object's fields
	Types    []TypeDef  // named generated types in deterministic emission order
}

// Lookup returns the named type with the given Name, or nil.
func (s *Spec) Lookup(name string) *TypeDef {
	for i := range s.Types {
		if s.Types[i].Name == name {
			return &s.Types[i]
		}
	}
	return nil
}
