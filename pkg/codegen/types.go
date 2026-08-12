package codegen

import "unicode"

// GoType represents a Go type used in generated code.
type GoType string

const (
	GoString       GoType = "string"
	GoBool         GoType = "bool"
	GoInt          GoType = "int"
	GoFloat64      GoType = "float64"
	GoStringPtr    GoType = "*string"
	GoBoolPtr      GoType = "*bool"
	GoIntPtr       GoType = "*int"
	GoFloat64Ptr   GoType = "*float64"
	GoStringSlice  GoType = "[]string"
	GoFloat64Slice GoType = "[]float64"
	GoMap          GoType = "map[string]any"
)

// Nullable reports whether this GoType is a pointer type. Named structural pointer
// types (e.g. *WorkflowType, *Action) are recognized via the leading "*" prefix.
func (t GoType) Nullable() bool {
	if t == GoStringPtr || t == GoBoolPtr || t == GoIntPtr || t == GoFloat64Ptr {
		return true
	}
	return len(t) > 0 && t[0] == '*'
}

// Base returns the non-pointer base type.
func (t GoType) Base() GoType {
	switch t {
	case GoStringPtr:
		return GoString
	case GoBoolPtr:
		return GoBool
	case GoIntPtr:
		return GoInt
	case GoFloat64Ptr:
		return GoFloat64
	default:
		if len(t) > 0 && t[0] == '*' {
			return t[1:]
		}
		return t
	}
}

// GoFieldName converts a JSON field name (camelCase) to a Go field name (PascalCase).
func GoFieldName(name string) string {
	if name == "" {
		return ""
	}
	runes := []rune(name)
	// Upper-case first rune
	runes[0] = rune(unicode.ToUpper(runes[0]))
	// Scan for underscores or any non-alphanumeric to trigger upper-casing next letter
	var result []rune
	skipNext := false
	for i, r := range runes {
		if i == 0 {
			result = append(result, runes[0])
			continue
		}
		if skipNext {
			result = append(result, rune(unicode.ToUpper(r)))
			skipNext = false
			continue
		}
		if r == '_' {
			skipNext = true
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			skipNext = true
			continue
		}
		result = append(result, r)
	}
	return string(result)
}

// GoTypeName maps a JSON type string from the schema to a GoType.
func GoTypeName(jsonType string) GoType {
	switch jsonType {
	case "boolean":
		return GoBool
	case "string":
		return GoString
	case "number":
		return GoFloat64
	case "string[]":
		return GoStringSlice
	case "number[]":
		return GoFloat64Slice
	case "array":
		return GoStringSlice
	case "object":
		return GoMap
	default:
		return GoString
	}
}

// GoTypeForField determines the GoType for a schema field.
// If nullable is true, returns the pointer variant.
func GoTypeForField(t GoType, nullable bool) GoType {
	if !nullable {
		return t
	}
	switch t {
	case GoString:
		return GoStringPtr
	case GoBool:
		return GoBoolPtr
	case GoInt:
		return GoIntPtr
	case GoFloat64:
		return GoFloat64Ptr
	case GoStringSlice, GoFloat64Slice, GoMap:
		// Slices and maps are not made nullable - they're already reference types
		return t
	default:
		return GoStringPtr
	}
}
