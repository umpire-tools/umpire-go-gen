package cli

import (
	"flag"
	"fmt"
)

// Mode indicates which generation mode is active.
type Mode int

const (
	ModeStandard Mode = iota // -i (existing behavior)
	ModeProfile              // -profile <file>
	ModeComposed             // -i <umpire-file> -value-schema <file>
)

// Config holds all parsed CLI arguments.
type Config struct {
	Mode              Mode   // generation mode
	InputPath         string // -i: input .umpire.json path
	ProfilePath       string // -profile: profile document path
	ValueSchemaPath   string // -value-schema: separate value-schema file path
	OutputDir         string // -o: output directory (default: ".")
	OutputFile        string // -output-file: exact output file path (overrides -o + derived name)
	PkgName           string // -pkg: Go package name (required)
	SchemaName        string // derived schema name used for generated type prefixes
	FieldsName        string // -fields: override for Fields struct name
	ConditionsName    string // -conditions: override for Conditions struct name
}

// ParseFlags parses command-line flags and returns a Config.
// Returns an error if required flags are missing or if flag combinations are ambiguous.
func ParseFlags(args []string) (*Config, error) {
	fs := flag.NewFlagSet("umpire-gen", flag.ContinueOnError)

	inputPath := fs.String("i", "", "input .umpire.json path (or umpire doc for composed mode)")
	profilePath := fs.String("profile", "", "profile document path (inline valueSchema + umpire)")
	valueSchemaPath := fs.String("value-schema", "", "value-schema file path (used with -i for composed mode)")
	outputDir := fs.String("o", ".", "output directory")
	outputFile := fs.String("output-file", "", "exact output file path (overrides -o + derived name)")
	pkgName := fs.String("pkg", "", "Go package name")
	fieldsName := fs.String("fields", "", "override for Fields struct name")
	conditionsName := fs.String("conditions", "", "override for Conditions struct name")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("parsing flags: %w", err)
	}

	if *pkgName == "" {
		return nil, fmt.Errorf("missing required flag: -pkg")
	}

	mode := ModeStandard
	pathForDefaults := *inputPath

	// Determine mode from flag combinations.
	hasInput := *inputPath != ""
	hasProfile := *profilePath != ""
	hasValueSchema := *valueSchemaPath != ""

	if hasProfile && hasInput {
		return nil, fmt.Errorf("ambiguous: -profile and -i cannot be used together")
	}
	if hasProfile && hasValueSchema {
		return nil, fmt.Errorf("ambiguous: -profile already includes valueSchema, cannot use -value-schema")
	}

	if hasProfile {
		mode = ModeProfile
		pathForDefaults = *profilePath
	} else if hasValueSchema {
		if !hasInput {
			return nil, fmt.Errorf("-value-schema requires -i for the umpire document")
		}
		mode = ModeComposed
	} else if !hasInput {
		return nil, fmt.Errorf("missing input: use -i, -profile, or -i with -value-schema")
	}

	defaultFields := fieldsDefault(pathForDefaults)
	defaultConditions := conditionsDefault(pathForDefaults)

	if *fieldsName == "" {
		*fieldsName = defaultFields
	}
	if *conditionsName == "" {
		*conditionsName = defaultConditions
	}

	return &Config{
		Mode:            mode,
		InputPath:       *inputPath,
		ProfilePath:     *profilePath,
		ValueSchemaPath: *valueSchemaPath,
		OutputDir:       *outputDir,
		OutputFile:      *outputFile,
		PkgName:         *pkgName,
		SchemaName:      baseName(pathForDefaults),
		FieldsName:      *fieldsName,
		ConditionsName:  *conditionsName,
	}, nil
}
