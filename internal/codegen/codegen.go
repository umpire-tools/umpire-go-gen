package codegen

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/umpire-tools/umpire-gen/internal/schema"
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
	hasStrings       bool
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
		hasStrings:       len(inferred.Branches) > 0,
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

	// Compile rules and generate Check/Challenge functions
	var helper, checkBody, challengeOutput string
	if len(g.allRules) > 0 {
		fieldTypes := make(map[string]GoType)
		for _, ft := range g.Inferred.Fields {
			fieldTypes[ft.Name] = ft.GoType
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
		ruleData := rc.CompileRules(g.allRules)

		// Build oneOf groups for branch disabling logic
		var oneOfGroups []OneOfGroup
		for groupName, branches := range branchGroups {
			var branchNames []string
			for _, b := range branches {
				branchNames = append(branchNames, b.Branch)
			}
			oneOfGroups = append(oneOfGroups, OneOfGroup{
				Name:     groupName,
				Branches: branchNames,
			})
		}
		g.hasStrings = len(oneOfGroups) > 0

		checkGen := NewCheckGenerator(g.AvailabilityName, g.FieldsName, g.ConditionsName, g.Inferred.Fields, ruleData, oneOfGroups)
		helper, checkBody = checkGen.Generate()

		challengeGen := NewChallengeGenerator(g.AvailabilityName, g.FieldsName, g.ConditionsName, g.Inferred.Fields, ruleData)
		challengeOutput = challengeGen.Generate()
	}

	data := generationTemplateData{
		PkgName:          g.PkgName,
		SchemaName:       g.SchemaName,
		FieldsName:       g.FieldsName,
		ConditionsName:   g.ConditionsName,
		AvailabilityName: g.AvailabilityName,
		Fields:           g.Inferred.Fields,
		Conditions:       g.Inferred.Conditions,
		Branches:         g.Inferred.Branches,
		BranchGroups:     branchGroups,
		HasStrings:       g.hasStrings,
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
{{- if .HasStrings }}
	"strings"
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
// Valid and Error are only populated when a named validator is attached
// to the field and the field is currently enabled and satisfied.
type FieldStatus struct {
	Enabled   bool
	Required  bool
	Satisfied bool
	Fair      bool
	Reason    *string  // nil when enabled; first blocking reason otherwise
	Reasons   []string // all blocking reasons; empty slice when enabled
	Valid     *bool    // nil = no validator; non-nil = validation result
	Error     string   // non-empty only when Valid != nil && !*Valid
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
