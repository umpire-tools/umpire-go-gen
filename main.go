package main

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"

	"github.com/umpire-tools/umpire-go-gen/internal/cli"
	"github.com/umpire-tools/umpire-go-gen/pkg/umpiregen"
)

func main() {
	cfg, err := cli.ParseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "usage: umpire-gen -i schema.umpire.json -pkg name [-o ./out] [-output-file path] [-fields FieldsName] [-conditions ConditionsName]\n")
		fmt.Fprintf(os.Stderr, "       umpire-gen -profile profile.json -pkg name [-o ./out] [-output-file path]\n")
		fmt.Fprintf(os.Stderr, "       umpire-gen -i umpire.json -value-schema schema.json -pkg name [-o ./out]\n")
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	cfgGen := umpiregen.Config{
		PkgName:        cfg.PkgName,
		SchemaName:     cfg.SchemaName,
		FieldsName:     cfg.FieldsName,
		ConditionsName: cfg.ConditionsName,
	}

	var source string
	var issues []umpiregen.DefinitionIssue

	switch cfg.Mode {
	case cli.ModeProfile:
		profileJSON, err := loadFile(cfg.ProfilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		source, issues, err = umpiregen.GenerateProfile(profileJSON, cfgGen)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error generating code: %v\n", err)
			os.Exit(1)
		}

	case cli.ModeComposed:
		umpireJSON, err := loadFile(cfg.InputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		valueSchemaJSON, err := loadFile(cfg.ValueSchemaPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		source, issues, err = umpiregen.GenerateComposed(umpireJSON, valueSchemaJSON, cfgGen)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error generating code: %v\n", err)
			os.Exit(1)
		}

	default:
		schemaJSON, err := loadSchemaJSON(cfg.InputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		source, err = umpiregen.Generate(schemaJSON, cfgGen)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error generating code: %v\n", err)
			os.Exit(1)
		}
	}

	// Warn about definition issues so users know the profile has structural concerns.
	for _, iss := range issues {
		fmt.Fprintf(os.Stderr, "profile definition issue: %s at %s\n", iss.Code, iss.Path)
	}

	formatted := formatSource(source)

	if err := writeOutput(formatted, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// formatSource runs go/format on the generated source. If formatting fails
// (e.g. the template produced invalid Go), it falls back to the unformatted
// source and prints a warning so the user can inspect the syntax error.
func formatSource(source string) []byte {
	formatted, err := format.Source([]byte(source))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: gofmt failed (%v); writing unformatted source\n", err)
		return []byte(source)
	}
	return formatted
}

// writeOutput writes the formatted generated code to the configured output:
// stdout when any input path is "-", an explicit file when OutputFile is set, or
// the derived filename inside OutputDir otherwise.
func writeOutput(formatted []byte, cfg *cli.Config) error {
	// Determine whether to write to stdout: if any input path is "-".
	writeStdout := cfg.InputPath == "-" || cfg.ProfilePath == "-" || cfg.ValueSchemaPath == "-"
	if writeStdout {
		if _, err := os.Stdout.Write(formatted); err != nil {
			return fmt.Errorf("writing stdout: %w", err)
		}
		return nil
	}

	// Pick the path to derive the default output filename from.
	pathForDerivation := cfg.InputPath
	if pathForDerivation == "" {
		pathForDerivation = cfg.ProfilePath
	}

	var outFile string
	if cfg.OutputFile != "" {
		outFile = cfg.OutputFile
		if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
	} else {
		outFile = filepath.Join(cfg.OutputDir, cli.DefaultOutputPath(pathForDerivation))
		if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
	}
	if err := os.WriteFile(outFile, formatted, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "generated %s\n", outFile)
	return nil
}

func loadFile(path string) ([]byte, error) {
	if path == "-" {
		return os.ReadFile("/dev/stdin")
	}
	return os.ReadFile(path)
}

func loadSchemaJSON(path string) ([]byte, error) {
	return loadFile(path)
}
