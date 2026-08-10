package umpiregen

import (
	"fmt"

	"github.com/umpire-tools/umpire-go-gen/pkg/codegen"
	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
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
	return generateFromBytes(schemaJSON, cfg)
}

// GenerateProfile reads a canonical profile document (valueSchema + umpire inline)
// and emits a Go source file from the embedded umpire availability document.
// Definition issues (excluded keywords, field mismatches, etc.) are returned
// alongside the generated source and do not prevent generation.
func GenerateProfile(profileJSON []byte, cfg Config) (source string, issues []DefinitionIssue, err error) {
	result, err := ParseProfile(profileJSON)
	if err != nil {
		return "", nil, fmt.Errorf("parse profile: %w", err)
	}

	source, err = generateFromBytes(result.Profile.UmpireJSON, cfg)
	if err != nil {
		return "", result.Issues, fmt.Errorf("generate from profile umpire: %w", err)
	}

	return source, result.Issues, nil
}

// GenerateComposed reads separately supplied umpire and value-schema documents
// and emits a Go source file from the umpire availability document.
// Definition issues are validated across both documents.
func GenerateComposed(umpireJSON, valueSchemaJSON []byte, cfg Config) (source string, issues []DefinitionIssue, err error) {
	result, err := ParseComposed(umpireJSON, valueSchemaJSON)
	if err != nil {
		return "", nil, fmt.Errorf("parse composed profile: %w", err)
	}

	source, err = generateFromBytes(result.Profile.UmpireJSON, cfg)
	if err != nil {
		return "", result.Issues, fmt.Errorf("generate from composed umpire: %w", err)
	}

	return source, result.Issues, nil
}

// generateFromBytes is the shared implementation for all generation paths.
func generateFromBytes(schemaJSON []byte, cfg Config) (string, error) {
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
