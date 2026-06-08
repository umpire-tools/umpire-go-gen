package cli

import (
	"strings"
	"unicode"
)

// toPascalCase converts a string to PascalCase by splitting on non-alphanumeric
// characters, uppercasing the first letter of each segment, and joining them.
//
// Examples:
//
//	"checkout"         → "Checkout"
//	"user_profile"     → "UserProfile"
//	"my-schema"        → "MySchema"
//	"A.B_C"            → "ABC"
//	"123abc"           → "123abc"
func toPascalCase(s string) string {
	var segments []string
	start := 0

	for i := 0; i < len(s); i++ {
		r := rune(s[i])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			if i > start {
				segments = append(segments, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		segments = append(segments, s[start:])
	}

	for i, seg := range segments {
		if seg == "" {
			continue
		}
		runes := []rune(seg)
		runes[0] = unicode.ToUpper(runes[0])
		segments[i] = string(runes)
	}

	return strings.Join(segments, "")
}

// baseName extracts the base name from a file path and converts it to PascalCase.
// E.g. "checkout.umpire.json" -> "Checkout"
//
//	"/path/to/my-schema.umpire.json" -> "MySchema"
//
// Suffix stripping is case-insensitive.
func baseName(inputPath string) string {
	// Get the filename portion
	base := inputPath
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' {
			base = base[i+1:]
			break
		}
	}

	// Strip known suffixes (case-insensitive)
	lower := strings.ToLower(base)
	for _, suffix := range []string{".umpire.json", ".umpire", ".json"} {
		if strings.HasSuffix(lower, suffix) {
			base = base[:len(base)-len(suffix)]
			lower = lower[:len(lower)-len(suffix)]
			break
		}
	}

	return toPascalCase(base)
}

// fieldsDefault derives the default struct name for fields from the input filename.
// E.g. "checkout.umpire.json" -> "CheckoutFields"
func fieldsDefault(inputPath string) string {
	return baseName(inputPath) + "Fields"
}

// conditionsDefault derives the default struct name for conditions from the input filename.
// E.g. "checkout.umpire.json" -> "CheckoutConditions"
func conditionsDefault(inputPath string) string {
	return baseName(inputPath) + "Conditions"
}
