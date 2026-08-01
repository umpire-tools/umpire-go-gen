package cli

import (
	"flag"
	"fmt"
)

// Config holds all parsed CLI arguments.
type Config struct {
	InputPath      string // -i: input .umpire.json path (required)
	OutputDir      string // -o: output directory (default: ".")
	OutputFile     string // -output-file: exact output file path (overrides -o + derived name)
	PkgName        string // -pkg: Go package name (required)
	SchemaName     string // derived schema name used for generated type prefixes
	FieldsName     string // -fields: override for Fields struct name
	ConditionsName string // -conditions: override for Conditions struct name
}

// ParseFlags parses command-line flags and returns a Config.
// Returns an error if required flags are missing.
func ParseFlags(args []string) (*Config, error) {
	fs := flag.NewFlagSet("umpire-gen", flag.ContinueOnError)

	inputPath := fs.String("i", "", "input .umpire.json path")
	outputDir := fs.String("o", ".", "output directory")
	outputFile := fs.String("output-file", "", "exact output file path (overrides -o + derived name)")
	pkgName := fs.String("pkg", "", "Go package name")
	fieldsName := fs.String("fields", "", "override for Fields struct name")
	conditionsName := fs.String("conditions", "", "override for Conditions struct name")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("parsing flags: %w", err)
	}

	if *inputPath == "" {
		return nil, fmt.Errorf("missing required flag: -i")
	}
	if *pkgName == "" {
		return nil, fmt.Errorf("missing required flag: -pkg")
	}

	defaultFields := fieldsDefault(*inputPath)
	defaultConditions := conditionsDefault(*inputPath)

	if *fieldsName == "" {
		*fieldsName = defaultFields
	}
	if *conditionsName == "" {
		*conditionsName = defaultConditions
	}

	return &Config{
		InputPath:      *inputPath,
		OutputDir:      *outputDir,
		OutputFile:     *outputFile,
		PkgName:        *pkgName,
		SchemaName:     baseName(*inputPath),
		FieldsName:     *fieldsName,
		ConditionsName: *conditionsName,
	}, nil
}
