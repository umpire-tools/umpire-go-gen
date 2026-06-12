package umpiregen

import (
	"fmt"

	"github.com/umpire-tools/umpire-gen/pkg/codegen"
	"github.com/umpire-tools/umpire-gen/pkg/schema"
)

// Config controls how Go code is generated.
type Config struct {
	PkgName        string // Go package name for the generated file
	SchemaName     string // e.g. "Checkout" (used for struct/function prefixes)
	FieldsName     string // name of the generated Fields struct (default: SchemaName+"Fields")
	ConditionsName string // name of the generated Conditions struct (default: SchemaName+"Conditions")
}

// Generate reads a .umpire.json payload and emits a Go source file.
func Generate(schemaJSON []byte, cfg Config) (string, error) {
	s, err := schema.Parse(schemaJSON)
	if err != nil {
		return "", fmt.Errorf("parse schema: %w", err)
	}

	inferred, err := codegen.InferTypes(s)
	if err != nil {
		return "", fmt.Errorf("infer types: %w", err)
	}

	fieldsName := cfg.FieldsName
	if fieldsName == "" {
		fieldsName = cfg.SchemaName + "Fields"
	}
	conditionsName := cfg.ConditionsName
	if conditionsName == "" {
		conditionsName = cfg.SchemaName + "Conditions"
	}

	gen := codegen.NewGenerator(cfg.SchemaName, cfg.PkgName, fieldsName, conditionsName, inferred)
	gen.WithFields(s.Fields)
	gen.WithRules(s.Rules)
	gen.WithSchema(s)

	result, err := gen.Generate()
	if err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}

	return result.Source, nil
}
