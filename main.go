package main

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"

	"github.com/umpire-tools/umpire-gen/internal/cli"
	"github.com/umpire-tools/umpire-gen/pkg/umpiregen"
)

func main() {
	cfg, err := cli.ParseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "usage: umpire-gen -i schema.umpire.json -pkg name [-o ./out] [-fields FieldsName] [-conditions ConditionsName]\n")
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

	// Run generated source through gofmt for canonical formatting.
	formatted, err := format.Source([]byte(source))
	if err != nil {
		// If gofmt fails, write the unformatted source so the user can
		// inspect the syntax error rather than getting nothing.
		fmt.Fprintf(os.Stderr, "warning: gofmt failed (%v); writing unformatted source\n", err)
		formatted = []byte(source)
	}

	// Write generated code
	if cfg.InputPath == "-" {
		if _, err := os.Stdout.Write(formatted); err != nil {
			fmt.Fprintf(os.Stderr, "error writing stdout: %v\n", err)
			os.Exit(1)
		}
	} else {
		var outFile string
		if cfg.OutputFile != "" {
			outFile = cfg.OutputFile
			if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
				fmt.Fprintf(os.Stderr, "error creating output directory: %v\n", err)
				os.Exit(1)
			}
		} else {
			outFile = filepath.Join(cfg.OutputDir, cli.DefaultOutputPath(cfg.InputPath))
			if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "error creating output directory: %v\n", err)
				os.Exit(1)
			}
		}
		if err := os.WriteFile(outFile, formatted, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing file: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "generated %s\n", outFile)
	}
}

func loadSchemaJSON(path string) ([]byte, error) {
	if path == "-" {
		return os.ReadFile("/dev/stdin")
	}
	return os.ReadFile(path)
}
