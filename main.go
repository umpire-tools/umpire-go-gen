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
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	schemaJSON, err := loadSchemaJSON(cfg.InputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	source, err := umpiregen.Generate(schemaJSON, umpiregen.Config{
		PkgName:        cfg.PkgName,
		SchemaName:     cfg.SchemaName,
		FieldsName:     cfg.FieldsName,
		ConditionsName: cfg.ConditionsName,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating code: %v\n", err)
		os.Exit(1)
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
// stdout when InputPath is "-", an explicit file when OutputFile is set, or
// the derived filename inside OutputDir otherwise.
func writeOutput(formatted []byte, cfg *cli.Config) error {
	if cfg.InputPath == "-" {
		if _, err := os.Stdout.Write(formatted); err != nil {
			return fmt.Errorf("writing stdout: %w", err)
		}
		return nil
	}

	var outFile string
	if cfg.OutputFile != "" {
		outFile = cfg.OutputFile
		if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
	} else {
		outFile = filepath.Join(cfg.OutputDir, cli.DefaultOutputPath(cfg.InputPath))
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

func loadSchemaJSON(path string) ([]byte, error) {
	if path == "-" {
		return os.ReadFile("/dev/stdin")
	}
	return os.ReadFile(path)
}
