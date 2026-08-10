package umpiregen

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ProfileSchemaURI is the required $schema value for a JSON Schema Composition Profile v1.
const ProfileSchemaURI = "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json"

// Profile wraps the parsed fields of a JSON Schema Composition Profile v1.
type Profile struct {
	UmpireJSON      []byte
	ValueSchemaJSON []byte
}

// DefinitionIssue describes a single profile definition problem.
type DefinitionIssue struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

func (d DefinitionIssue) Error() string {
	return fmt.Sprintf("%s at %s", d.Code, d.Path)
}

// ProfileResult holds the result of profile parsing.
type ProfileResult struct {
	Profile *Profile
	Issues  []DefinitionIssue
}

// ParseProfile parses a canonical inline profile document.
func ParseProfile(data []byte) (*ProfileResult, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if err := requireKeys(raw, "$schema", "profileVersion", "valueSchema", "umpire"); err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}

	var schemaURI string
	if err := json.Unmarshal(raw["$schema"], &schemaURI); err != nil || schemaURI != ProfileSchemaURI {
		return nil, fmt.Errorf("profile: $schema must be %q", ProfileSchemaURI)
	}

	var pv float64
	if err := json.Unmarshal(raw["profileVersion"], &pv); err != nil || pv != 1 {
		return nil, fmt.Errorf("profile: profileVersion must be 1")
	}

	valueSchemaJSON := raw["valueSchema"]
	umpireJSON := raw["umpire"]

	var issues []DefinitionIssue
	issues = append(issues, validateVSMeta(valueSchemaJSON)...)

	var umpireMeta struct {
		Version float64 `json:"version"`
	}
	if err := json.Unmarshal(umpireJSON, &umpireMeta); err != nil || umpireMeta.Version != 1 {
		return nil, fmt.Errorf("profile: umpire must be a valid v1 document")
	}

	issues = append(issues, validateValueSchema(valueSchemaJSON)...)
	issues = append(issues, validateCrossFields(valueSchemaJSON, umpireJSON)...)

	return &ProfileResult{
		Profile: &Profile{UmpireJSON: umpireJSON, ValueSchemaJSON: valueSchemaJSON},
		Issues:  issues,
	}, nil
}

// ParseComposed parses separately supplied Umpire and value-schema bytes.
func ParseComposed(umpireJSON, valueSchemaJSON []byte) (*ProfileResult, error) {
	if len(umpireJSON) == 0 {
		return nil, fmt.Errorf("umpire document is required")
	}
	if len(valueSchemaJSON) == 0 {
		return nil, fmt.Errorf("value-schema is required for composed profile")
	}

	var issues []DefinitionIssue

	var umpireMeta struct {
		Version float64 `json:"version"`
	}
	if err := json.Unmarshal(umpireJSON, &umpireMeta); err != nil || umpireMeta.Version != 1 {
		return nil, fmt.Errorf("profile: umpire must be a valid v1 document")
	}

	issues = append(issues, validateVSMeta(valueSchemaJSON)...)
	issues = append(issues, validateValueSchema(valueSchemaJSON)...)
	issues = append(issues, validateCrossFields(valueSchemaJSON, umpireJSON)...)

	return &ProfileResult{
		Profile: &Profile{UmpireJSON: umpireJSON, ValueSchemaJSON: valueSchemaJSON},
		Issues:  issues,
	}, nil
}

// validateVSMeta checks the valueSchema root-level $schema and type.
func validateVSMeta(raw json.RawMessage) []DefinitionIssue {
	var vsMeta struct {
		Schema string `json:"$schema"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal(raw, &vsMeta); err != nil {
		return []DefinitionIssue{{Code: "invalidProfile", Path: "/valueSchema"}}
	}

	var issues []DefinitionIssue
	if vsMeta.Schema != "https://json-schema.org/draft/2020-12/schema" {
		issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: "/valueSchema"})
	}
	if vsMeta.Type != "object" {
		issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: "/valueSchema"})
	}
	return issues
}

func requireKeys(raw map[string]json.RawMessage, keys ...string) error {
	for _, k := range keys {
		if _, ok := raw[k]; !ok {
			return fmt.Errorf("missing required key %q", k)
		}
	}
	return nil
}

// validateValueSchema walks a valueSchema and enforces profile-level rules.
func validateValueSchema(raw json.RawMessage) []DefinitionIssue {
	var issues []DefinitionIssue
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return issues
	}

	issues = append(issues, checkExcludedKeywords("", doc)...)

	if propsRaw, ok := doc["properties"]; ok {
		var props map[string]json.RawMessage
		if json.Unmarshal(propsRaw, &props) == nil {
			for name, propRaw := range props {
				issues = append(issues, validatePropertySchema("/properties/"+name, propRaw)...)
			}
		}
	}

	if defsRaw, ok := doc["$defs"]; ok {
		var defs map[string]json.RawMessage
		if json.Unmarshal(defsRaw, &defs) == nil {
			refGraph := buildRefGraph(defs)
			for _, p := range detectCycles(refGraph) {
				issues = append(issues, DefinitionIssue{
					Code: "referenceCycle",
					Path: fmt.Sprintf("/valueSchema/$defs/%s", p),
				})
			}
			for name, defRaw := range defs {
				issues = append(issues, validatePropertySchema("/$defs/"+name, defRaw)...)
			}
		}
	}
	return issues
}

var excludedKeywords = map[string]bool{
	"allOf": true, "anyOf": true, "not": true,
	"uniqueItems": true, "pattern": true, "format": true,
}

func checkExcludedKeywords(prefix string, obj map[string]json.RawMessage) []DefinitionIssue {
	var issues []DefinitionIssue
	for key := range obj {
		if excludedKeywords[key] {
			issues = append(issues, DefinitionIssue{
				Code: "unsupportedKeyword",
				Path: fmt.Sprintf("/valueSchema%s/%s", prefix, key),
			})
		}
	}
	return issues
}

func validatePropertySchema(prefix string, raw json.RawMessage) []DefinitionIssue {
	var issues []DefinitionIssue
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}

	issues = append(issues, checkExcludedKeywords(prefix, obj)...)

	if refRaw, ok := obj["$ref"]; ok {
		var ref string
		if json.Unmarshal(refRaw, &ref) == nil && !strings.HasPrefix(ref, "#/$defs/") {
			issues = append(issues, DefinitionIssue{
				Code: "invalidReference",
				Path: fmt.Sprintf("/valueSchema%s/$ref", prefix),
			})
		}
	}

	if oneOfRaw, ok := obj["oneOf"]; ok {
		issues = append(issues, validateOneOf(prefix, oneOfRaw)...)
	}

	// Recurse into schema-containing keywords.
	recurseKeys := []string{"items", "contains", "unevaluatedItems", "if", "then", "else"}
	for _, key := range recurseKeys {
		if subRaw, ok := obj[key]; ok {
			issues = append(issues, validatePropertySchema(prefix+"/"+key, subRaw)...)
		}
	}

	if piRaw, ok := obj["prefixItems"]; ok {
		var pi []json.RawMessage
		if json.Unmarshal(piRaw, &pi) == nil {
			for i, item := range pi {
				issues = append(issues, validatePropertySchema(fmt.Sprintf("%s/prefixItems/%d", prefix, i), item)...)
			}
		}
	}

	if propsRaw, ok := obj["properties"]; ok {
		var props map[string]json.RawMessage
		if json.Unmarshal(propsRaw, &props) == nil {
			for name, propRaw := range props {
				issues = append(issues, validatePropertySchema(prefix+"/properties/"+name, propRaw)...)
			}
		}
	}

	if defsRaw, ok := obj["$defs"]; ok {
		var defs map[string]json.RawMessage
		if json.Unmarshal(defsRaw, &defs) == nil {
			for name, defRaw := range defs {
				issues = append(issues, validatePropertySchema(prefix+"/$defs/"+name, defRaw)...)
			}
		}
	}

	return issues
}

// validateOneOf checks oneOf branches for tagged union rules.
func validateOneOf(prefix string, raw json.RawMessage) []DefinitionIssue {
	var oneOf []json.RawMessage
	if err := json.Unmarshal(raw, &oneOf); err != nil || len(oneOf) == 0 {
		return nil
	}

	var commonDiscriminator string
	var branchIssues []DefinitionIssue

	for i, branchRaw := range oneOf {
		var branch map[string]json.RawMessage
		if err := json.Unmarshal(branchRaw, &branch); err != nil {
			continue
		}

		propsRaw, hasProps := branch["properties"]
		if !hasProps {
			branchIssues = append(branchIssues, DefinitionIssue{
				Code: "invalidDiscriminator",
				Path: fmt.Sprintf("/valueSchema%s/oneOf", prefix),
			})
			continue
		}
		var props map[string]json.RawMessage
		if err := json.Unmarshal(propsRaw, &props); err != nil {
			branchIssues = append(branchIssues, DefinitionIssue{
				Code: "invalidDiscriminator",
				Path: fmt.Sprintf("/valueSchema%s/oneOf", prefix),
			})
			continue
		}

		foundDiscriminator := ""
		hasInvalidConst := false
		for propName, propRaw := range props {
			var propObj map[string]json.RawMessage
			if json.Unmarshal(propRaw, &propObj) != nil {
				continue
			}
			if constRaw, ok := propObj["const"]; ok {
				var constVal string
				if json.Unmarshal(constRaw, &constVal) != nil {
					hasInvalidConst = true
					continue
				}
				if reqRaw, ok := branch["required"]; ok {
					var req []string
					if json.Unmarshal(reqRaw, &req) == nil {
						for _, r := range req {
							if r == propName {
								foundDiscriminator = propName
								break
							}
						}
					}
				}
				if foundDiscriminator != "" {
					break
				}
			}
		}

		if hasInvalidConst {
			branchIssues = append(branchIssues, DefinitionIssue{
				Code: "invalidDiscriminator",
				Path: fmt.Sprintf("/valueSchema%s/oneOf", prefix),
			})
			continue
		}

		if i == 0 {
			commonDiscriminator = foundDiscriminator
		} else if foundDiscriminator != commonDiscriminator {
			branchIssues = append(branchIssues, DefinitionIssue{
				Code: "invalidDiscriminator",
				Path: fmt.Sprintf("/valueSchema%s/oneOf", prefix),
			})
		}
	}

	if len(branchIssues) > 0 {
		return branchIssues
	}
	if commonDiscriminator == "" && len(oneOf) > 0 {
		return []DefinitionIssue{{Code: "invalidDiscriminator", Path: fmt.Sprintf("/valueSchema%s/oneOf", prefix)}}
	}
	return nil
}

func buildRefGraph(defs map[string]json.RawMessage) map[string][]string {
	graph := make(map[string][]string)
	for name, defRaw := range defs {
		graph[name] = extractRefTargets(defRaw)
	}
	return graph
}

func extractRefTargets(raw json.RawMessage) []string {
	var targets []string
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		for _, elem := range arr {
			targets = append(targets, extractRefTargets(elem)...)
		}
		return targets
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}

	if refRaw, ok := obj["$ref"]; ok {
		var ref string
		if json.Unmarshal(refRaw, &ref) == nil && strings.HasPrefix(ref, "#/$defs/") {
			targets = append(targets, strings.TrimPrefix(ref, "#/$defs/"))
		}
	}

	// Recurse into items.
	if itemsRaw, ok := obj["items"]; ok {
		targets = append(targets, extractRefTargets(itemsRaw)...)
	}

	// Recurse into properties -- iterate over each property value.
	if propsRaw, ok := obj["properties"]; ok {
		var props map[string]json.RawMessage
		if json.Unmarshal(propsRaw, &props) == nil {
			for _, propRaw := range props {
				targets = append(targets, extractRefTargets(propRaw)...)
			}
		}
	}

	// Recurse into contains, unevaluatedItems, if, then, else.
	for _, key := range []string{"contains", "unevaluatedItems", "if", "then", "else"} {
		if subRaw, ok := obj[key]; ok {
			targets = append(targets, extractRefTargets(subRaw)...)
		}
	}

	for _, listKey := range []string{"anyOf", "oneOf", "allOf", "prefixItems"} {
		if arrRaw, ok := obj[listKey]; ok {
			targets = append(targets, extractRefTargets(arrRaw)...)
		}
	}

	return targets
}

func detectCycles(graph map[string][]string) []string {
	type color int
	const (
		white color = iota
		gray
		black
	)
	colors := make(map[string]color)
	for name := range graph {
		colors[name] = white
	}
	var cycles []string
	seen := make(map[string]bool)

	var dfs func(string, []string)
	dfs = func(node string, path []string) {
		if colors[node] == gray {
			if len(path) > 0 && !seen[path[len(path)-1]] {
				seen[path[len(path)-1]] = true
				cycles = append(cycles, path[len(path)-1])
			}
			if !seen[node] {
				seen[node] = true
				cycles = append(cycles, node)
			}
			return
		}
		if colors[node] == black {
			return
		}
		colors[node] = gray
		for _, neighbor := range graph[node] {
			dfs(neighbor, append(path, node))
		}
		colors[node] = black
	}
	for name := range graph {
		if colors[name] == white {
			dfs(name, nil)
		}
	}
	return cycles
}

type vsSchemaProp struct {
	Type    string   `json:"type"`
	Minimum *float64 `json:"minimum,omitempty"`
	Maximum *float64 `json:"maximum,omitempty"`
	MinLen  *float64 `json:"minLength,omitempty"`
	MaxLen  *float64 `json:"maxLength,omitempty"`
}

func validateCrossFields(valueSchemaRaw, umpireRaw json.RawMessage) []DefinitionIssue {
	var issues []DefinitionIssue
	var vs struct {
		Properties map[string]vsSchemaProp `json:"properties"`
	}
	if err := json.Unmarshal(valueSchemaRaw, &vs); err != nil {
		return issues
	}

	var umpire struct {
		Fields map[string]struct {
			IsEmpty string          `json:"isEmpty,omitempty"`
			Default json.RawMessage `json:"default,omitempty"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(umpireRaw, &umpire); err != nil {
		return issues
	}

	for fieldName, umpDef := range umpire.Fields {
		vsProp, exists := vs.Properties[fieldName]
		if !exists {
			issues = append(issues, DefinitionIssue{Code: "fieldMismatch", Path: "/valueSchema"})
			continue
		}
		if umpDef.IsEmpty != "" {
			if issue := checkIsEmptyCompatibility(fieldName, umpDef.IsEmpty, vsProp.Type); issue != nil {
				issues = append(issues, *issue)
			}
		}
		if len(umpDef.Default) > 0 {
			if issue := checkDefaultCompatibility(fieldName, umpDef.Default, vsProp); issue != nil {
				issues = append(issues, *issue)
			}
		}
	}
	return issues
}

func checkIsEmptyCompatibility(fieldName, isEmpty, schemaType string) *DefinitionIssue {
	switch isEmpty {
	case "string":
		if schemaType != "string" {
			return &DefinitionIssue{Code: "incompatibleIsEmpty", Path: fmt.Sprintf("/umpire/fields/%s", fieldName)}
		}
	case "number":
		if schemaType != "number" && schemaType != "integer" {
			return &DefinitionIssue{Code: "incompatibleIsEmpty", Path: fmt.Sprintf("/umpire/fields/%s", fieldName)}
		}
	case "boolean":
		if schemaType != "boolean" {
			return &DefinitionIssue{Code: "incompatibleIsEmpty", Path: fmt.Sprintf("/umpire/fields/%s", fieldName)}
		}
	case "array":
		if schemaType != "array" {
			return &DefinitionIssue{Code: "incompatibleIsEmpty", Path: fmt.Sprintf("/umpire/fields/%s", fieldName)}
		}
	case "object":
		if schemaType != "object" {
			return &DefinitionIssue{Code: "incompatibleIsEmpty", Path: fmt.Sprintf("/umpire/fields/%s", fieldName)}
		}
	case "present":
	}
	return nil
}

func checkDefaultCompatibility(fieldName string, defaultRaw json.RawMessage, prop vsSchemaProp) *DefinitionIssue {
	var val any
	if err := json.Unmarshal(defaultRaw, &val); err != nil {
		return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
	}
	switch prop.Type {
	case "string":
		v, ok := val.(string)
		if !ok {
			return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
		}
		if prop.MinLen != nil && float64(len(v)) < *prop.MinLen {
			return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
		}
		if prop.MaxLen != nil && float64(len(v)) > *prop.MaxLen {
			return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
		}
	case "integer":
		v, ok := val.(float64)
		if !ok {
			return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
		}
		if v != float64(int64(v)) {
			return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
		}
		if prop.Minimum != nil && v < *prop.Minimum {
			return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
		}
		if prop.Maximum != nil && v > *prop.Maximum {
			return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
		}
	case "number":
		v, ok := val.(float64)
		if !ok {
			return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
		}
		if prop.Minimum != nil && v < *prop.Minimum {
			return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
		}
		if prop.Maximum != nil && v > *prop.Maximum {
			return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
		}
	}
	return nil
}

func (pr *ProfileResult) IssuesError() error {
	if len(pr.Issues) == 0 {
		return nil
	}
	var msgs []string
	for _, iss := range pr.Issues {
		msgs = append(msgs, iss.Error())
	}
	return fmt.Errorf("profile definition issues: %s", strings.Join(msgs, "; "))
}
