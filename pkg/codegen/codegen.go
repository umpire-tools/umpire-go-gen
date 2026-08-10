package codegen

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
)

// Generator produces Go source code from an inferred schema.
type Generator struct {
	PkgName          string
	SchemaName       string // e.g. "Checkout" from "checkout.umpire.json"
	FieldsName       string
	ConditionsName   string
	AvailabilityName string
	Inferred         *InferredSchema
	allRules         []schema.Rule
	allFields        []schema.FieldDef // original field defs for Required/IsEmpty
	allSchema        *schema.Schema    // full schema for accessing BranchExpressions
	hasStrings       bool
	hasStrconv       bool
	// Overrides, when set, replace the inferred GoType for matching field names
	// (keyed by original JSON field name). Used by the profile path to surface
	// structural valueSchema types in the availability Fields struct. Empty by
	// default so the availability-only path is byte-for-byte unchanged.
	Overrides map[string]GoType
}

// NewGenerator creates a Generator for the given schema and config.
func NewGenerator(schemaName, pkgName, fieldsName, conditionsName string, inferred *InferredSchema) *Generator {
	return &Generator{
		PkgName:          pkgName,
		SchemaName:       schemaName,
		FieldsName:       fieldsName,
		ConditionsName:   conditionsName,
		AvailabilityName: schemaName + "Availability",
		Inferred:         inferred,
		hasStrings:       true,
		hasStrconv:       true,
	}
}

// WithRules sets the rules for Check/Challenge generation.
func (g *Generator) WithRules(rules []schema.Rule) *Generator {
	g.allRules = rules
	return g
}

// WithFields sets the original field definitions (for Required/IsEmpty).
func (g *Generator) WithFields(fields []schema.FieldDef) *Generator {
	g.allFields = fields
	return g
}

// WithFieldTypeOverrides sets explicit GoTypes for named fields, overriding
// type inference. Provide nil/empty to keep inference for all fields.
func (g *Generator) WithFieldTypeOverrides(overrides map[string]GoType) *Generator {
	g.Overrides = overrides
	return g
}

// WithSchema sets the full schema (for accessing BranchExpressions and other metadata).
func (g *Generator) WithSchema(s *schema.Schema) *Generator {
	g.allSchema = s
	return g
}

// GenerateResult holds the generated Go source code.
type GenerateResult struct {
	Source string
}

// Generate produces the full Go source file.
func (g *Generator) Generate() (*GenerateResult, error) {
	// Group branches by group name
	branchGroups := make(map[string][]OneOfBranch)
	for _, b := range g.Inferred.Branches {
		branchGroups[b.GroupName] = append(branchGroups[b.GroupName], b)
	}

	// Detect if strconv is needed for any check operators
	for _, rule := range g.allRules {
		if rule.Check != nil {
			switch rule.Check.Op {
			case "max", "min", "range", "integer":
				g.hasStrconv = true
			}
		}
	}

	fieldTypes := make(map[string]GoType)
	for _, ft := range g.Inferred.Fields {
		fieldTypes[ft.Name] = ft.GoType
	}

	// Honor explicit structural type overrides for the availability Fields.
	fieldsTypeInfo := g.Inferred.Fields
	if len(g.Overrides) > 0 {
		fieldsTypeInfo = applyTypeOverrides(g.Inferred.Fields, g.Overrides)
		fieldTypes = make(map[string]GoType, len(fieldsTypeInfo))
		for _, ft := range fieldsTypeInfo {
			fieldTypes[ft.Name] = ft.GoType
		}
	}
	condTypes := make(map[string]GoType)
	for _, ct := range g.Inferred.Conditions {
		condTypes[ct.Name] = ct.GoType
	}

	fields := g.allFields
	if fields == nil {
		fields = []schema.FieldDef{}
	}

	rc := NewRuleCompiler(fieldTypes, condTypes, fields)
	if g.allSchema != nil {
		rc.WithSchema(g.allSchema)
	}
	ruleData := rc.CompileRules(g.allRules)

	// Build oneOf groups for branch disabling logic
	var oneOfGroups []OneOfGroup
	for groupName, branches := range branchGroups {
		isOneOf := false
		for _, b := range branches {
			if b.IsOneOf {
				isOneOf = true
				break
			}
		}
		oneOfGroups = append(oneOfGroups, OneOfGroup{
			Name:     groupName,
			Branches: branches,
			IsOneOf:  isOneOf,
		})
	}
	checkGen := NewCheckGenerator(g.AvailabilityName, g.FieldsName, g.ConditionsName, fieldsTypeInfo, ruleData, oneOfGroups)
	checkGen.WithExprCompiler(NewExprCompiler(fieldTypes, condTypes))
	helper, checkBody := checkGen.Generate()

	challengeGen := NewChallengeGenerator(g.AvailabilityName, g.FieldsName, g.ConditionsName, g.Inferred.Fields, ruleData)
	challengeOutput := challengeGen.Generate()

	data := generationTemplateData{
		PkgName:          g.PkgName,
		SchemaName:       g.SchemaName,
		FieldsName:       g.FieldsName,
		ConditionsName:   g.ConditionsName,
		AvailabilityName: g.AvailabilityName,
		Fields:           fieldsTypeInfo,
		Conditions:       g.Inferred.Conditions,
		Branches:         g.Inferred.Branches,
		BranchGroups:     branchGroups,
		HasStrings:       g.hasStrings,
		HasStrconv:       g.hasStrconv,
		Helper:           helper,
		CheckBody:        checkBody,
		ChallengeOutput:  challengeOutput,
	}

	tmpl := template.Must(template.New("umpire").Funcs(templateFuncMap).Parse(templateSrc))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}

	return &GenerateResult{Source: buf.String()}, nil
}

// applyTypeOverrides returns a copy of the field info slice with GoTypes for the
// given field names replaced by the overrides. Fields not in the map are unchanged.
func applyTypeOverrides(fields []FieldTypeInfo, overrides map[string]GoType) []FieldTypeInfo {
	out := make([]FieldTypeInfo, len(fields))
	copy(out, fields)
	for i := range out {
		if t, ok := overrides[out[i].Name]; ok {
			out[i].GoType = t
		}
	}
	return out
}

// generationTemplateData is the data passed to the Go template.
type generationTemplateData struct {
	PkgName          string
	SchemaName       string
	FieldsName       string
	ConditionsName   string
	AvailabilityName string
	Fields           []FieldTypeInfo
	Conditions       []ConditionTypeInfo
	Branches         []OneOfBranch
	// BranchGroups groups branches by their GroupName for template rendering.
	BranchGroups map[string][]OneOfBranch
	// HasStrings indicates whether the strings package should be imported.
	HasStrings bool
	// HasStrconv indicates whether the strconv package should be imported.
	HasStrconv bool
	// Pre-generated Go code sections (raw, not templated).
	Helper          string
	CheckBody       string
	ChallengeOutput string
}

// templateFuncMap provides helper functions for the template.
var templateFuncMap = template.FuncMap{
	"goType": func(ft FieldTypeInfo) string {
		return string(ft.GoType)
	},
	"condGoType": func(ct ConditionTypeInfo) string {
		return string(ct.GoType)
	},
	"goFieldName": func(name string) string {
		return GoFieldName(name)
	},
	"goFieldNameFT": func(ft FieldTypeInfo) string {
		return GoFieldName(ft.Name)
	},
	"activeFieldName": func(groupName string) string {
		return "Active" + GoFieldName(groupName)
	},
	"activeField": func(groupName string) string {
		return fmt.Sprintf("	%s %s `json:\"-\"`", "Active"+GoFieldName(groupName), groupName)
	},
}

// templateSrc is the Go source template.
const templateSrc = `// Code generated by umpire-gen; DO NOT EDIT.

package {{ .PkgName }}

import (
	"regexp"
	"strings"
{{- if .HasStrconv }}
	"strconv"
{{- end }}
)

// {{ .FieldsName }} holds the fields for {{ .SchemaName }} availability checks.
type {{ .FieldsName }} struct {
{{- range .Fields }}
	{{ goFieldNameFT . }} {{ goType . }} ` + "`json:\"{{ .JSONTag }},omitempty\"`" + `
{{- end }}
}

// {{ .ConditionsName }} holds the conditions for {{ .SchemaName }} availability checks.
type {{ .ConditionsName }} struct {
{{- range .Conditions }}
	{{ goFieldName .Name }} {{ condGoType . }} ` + "`json:\"{{ .JSONTag }}\"`" + `
{{- end }}
}

// {{ .AvailabilityName }} holds the availability status for each field.
type {{ .AvailabilityName }} struct {
{{- range .Fields }}
	{{ goFieldNameFT . }} FieldStatus
{{- end }}
{{- range $groupName, $branchList := .BranchGroups }}
	{{ activeFieldName $groupName }} {{ $groupName }} ` + "`json:\"-\"`" + `
{{- end }}
}

// FieldStatus mirrors the conformance expectedAvailability shape exactly.
// Valid and Error are omitted unless a named validator runs for an enabled,
// satisfied field.
type FieldStatus struct {
	Enabled   bool     ` + "`json:\"enabled\"`" + `
	Required  bool     ` + "`json:\"required\"`" + `
	Satisfied bool     ` + "`json:\"satisfied\"`" + `
	Fair      bool     ` + "`json:\"fair\"`" + `
	Reason    *string  ` + "`json:\"reason\"`" + `
	Reasons   []string ` + "`json:\"reasons\"`" + `
	Valid     *bool    ` + "`json:\"valid,omitempty\"`" + `
	Error     string   ` + "`json:\"error,omitempty\"`" + `
}

// contains reports whether a string slice contains a given value.
func contains(s []string, v string) bool {
	for _, ss := range s {
		if ss == v {
			return true
		}
	}
	return false
}

{{- range $groupName, $branchList := .BranchGroups }}
// {{ $groupName }} represents the active branch of the {{ $groupName }} oneOf/eitherOf group.
type {{ $groupName }} int

const (
	{{ $groupName }}None {{ $groupName }} = iota
{{- range $branchList }}
	{{ .Branch }} {{ $groupName }} = iota
{{- end }}
)
{{- end }}
{{ .Helper }}
{{ .CheckBody }}
{{ .ChallengeOutput }}
`
