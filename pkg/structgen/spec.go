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

// UnionBranch represents one tagged-union branch.
// It stores the discriminator wire value and the branch's property schemas.
// Field names and JSON tags retain their original wire values.
type UnionBranch struct {
	Wire     string     // discriminator const value, e.g. "manual"
	Fields   []FieldDef // this branch's declared properties (includes the discriminator)
	Required []string   // branch-required JSON property names
}

// TypeDef is a named type emitted by structgen. Exactly one family applies
// depending on Kind.
type TypeDef struct {
	Name          string        // deterministic generated Go type name
	Kind          Kind          // scalar | object | enum | union | array
	JSONName      string        // original JSON property name for inline types; "" for $defs/root
	Fields        []FieldDef    // object fields
	Values        []EnumValue   // enum values in declared order
	Scalar        Scalar        // scalar/enum underlying type
	Discriminator string        // union shared discriminator property
	Branches      []UnionBranch // branch-specific union definitions
	Elem          *FieldType    // array definition element type
	Constraints   Constraints   // scalar/array definition constraints
}

// EnumValue is one allowed value of an enum type.
type EnumValue struct {
	Name string // Go constant suffix, e.g. "Pipeline" or "Value1"
	Wire any    // original primitive wire value
}

// Constraints are the supported scalar and array validation keywords.
type Constraints struct {
	MinLength        *int
	MaxLength        *int
	Minimum          *float64
	Maximum          *float64
	ExclusiveMinimum *float64
	ExclusiveMaximum *float64
	MinItems         *int
	MaxItems         *int
	Const            any // const value when HasConst
	HasConst         bool
}

// FieldDef is one field of an object/union type, or of the root.
type FieldDef struct {
	Name     string    // original JSON property name, e.g. "workflowType"
	GoName   string    // PascalCase Go identifier, e.g. "WorkflowType"
	JSONTag  string    // exact JSON wire name
	Required bool      // whether the property appears in the object's required array
	Type     FieldType // structural type
	Constraints
}

// FieldType is the structural type of a field: either a primitive scalar, an
// array of another field type, or a reference to a named type.
type FieldType struct {
	Kind        Kind
	Scalar      Scalar     // Kind == KindScalar
	Ref         string     // non-empty when the schema uses a named generated type
	Elem        *FieldType // Kind == KindArray
	Constraints Constraints
}

// IsReference reports whether the field type points at a named generated type.
func (ft FieldType) IsReference() bool { return ft.Ref != "" }

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
