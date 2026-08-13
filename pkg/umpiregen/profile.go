package umpiregen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"unicode/utf8"
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
		Issues:  dedupeDefinitionIssues(issues),
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
		Issues:  dedupeDefinitionIssues(issues),
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

	var defs map[string]json.RawMessage
	if defsRaw, ok := doc["$defs"]; ok {
		_ = json.Unmarshal(defsRaw, &defs)
	}

	if propsRaw, ok := doc["properties"]; ok {
		var props map[string]json.RawMessage
		if json.Unmarshal(propsRaw, &props) == nil {
			for name, propRaw := range props {
				issues = append(issues, validatePropertySchema("/properties/"+name, propRaw, defs)...)
			}
		}
	}

	if defs != nil {
		refGraph := buildRefGraph(defs)
		for _, p := range detectCycles(refGraph) {
			issues = append(issues, DefinitionIssue{
				Code: "referenceCycle",
				Path: fmt.Sprintf("/valueSchema/$defs/%s", p),
			})
		}
		for name, defRaw := range defs {
			issues = append(issues, validatePropertySchema("/$defs/"+name, defRaw, defs)...)
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

func validatePropertySchema(prefix string, raw json.RawMessage, rootDefs map[string]json.RawMessage) []DefinitionIssue {
	var issues []DefinitionIssue
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}

	issues = append(issues, checkExcludedKeywords(prefix, obj)...)

	if refRaw, ok := obj["$ref"]; ok {
		var ref string
		targetExists := false
		if json.Unmarshal(refRaw, &ref) == nil && strings.HasPrefix(ref, "#/$defs/") {
			target := strings.TrimPrefix(ref, "#/$defs/")
			_, targetExists = rootDefs[target]
		}
		if !targetExists {
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
	recurseKeys := []string{"items", "contains", "unevaluatedItems", "if", "then", "else", "unevaluatedProperties"}
	for _, key := range recurseKeys {
		if subRaw, ok := obj[key]; ok {
			issues = append(issues, validatePropertySchema(prefix+"/"+key, subRaw, rootDefs)...)
		}
	}

	if piRaw, ok := obj["prefixItems"]; ok {
		var pi []json.RawMessage
		if json.Unmarshal(piRaw, &pi) == nil {
			for i, item := range pi {
				issues = append(issues, validatePropertySchema(fmt.Sprintf("%s/prefixItems/%d", prefix, i), item, rootDefs)...)
			}
		}
	}

	if propsRaw, ok := obj["properties"]; ok {
		var props map[string]json.RawMessage
		if json.Unmarshal(propsRaw, &props) == nil {
			for name, propRaw := range props {
				issues = append(issues, validatePropertySchema(prefix+"/properties/"+name, propRaw, rootDefs)...)
			}
		}
	}

	if defsRaw, ok := obj["$defs"]; ok {
		var defs map[string]json.RawMessage
		if json.Unmarshal(defsRaw, &defs) == nil {
			for name, defRaw := range defs {
				issues = append(issues, validatePropertySchema(prefix+"/$defs/"+name, defRaw, rootDefs)...)
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

		var required []string
		_ = json.Unmarshal(branch["required"], &required)
		requiredSet := make(map[string]bool, len(required))
		for _, r := range required {
			requiredSet[r] = true
		}

		hasInvalidConst := false
		var discriminators []string
		for propName, propRaw := range props {
			var propObj map[string]json.RawMessage
			if json.Unmarshal(propRaw, &propObj) != nil {
				continue
			}
			constRaw, hasConst := propObj["const"]
			if !hasConst {
				continue
			}
			var constVal string
			if json.Unmarshal(constRaw, &constVal) != nil {
				hasInvalidConst = true
				continue
			}
			// Track every required property carrying a string const (not just the
			// first) so discriminator detection is deterministic regardless of map
			// iteration order. A tagged union requires exactly one discriminator.
			if requiredSet[propName] {
				discriminators = append(discriminators, propName)
			}
		}
		sort.Strings(discriminators)

		if hasInvalidConst || len(discriminators) != 1 {
			branchIssues = append(branchIssues, DefinitionIssue{
				Code: "invalidDiscriminator",
				Path: fmt.Sprintf("/valueSchema%s/oneOf", prefix),
			})
			continue
		}

		if i == 0 {
			commonDiscriminator = discriminators[0]
		} else if discriminators[0] != commonDiscriminator {
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

	// Recurse into contains, unevaluatedItems, if, then, else, unevaluatedProperties.
	for _, key := range []string{"contains", "unevaluatedItems", "if", "then", "else", "unevaluatedProperties"} {
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
	colors := make(map[string]color, len(graph))
	names := make([]string, 0, len(graph))
	for name := range graph {
		colors[name] = white
		names = append(names, name)
	}
	sort.Strings(names)

	// Report the definition containing the reference that closes a cycle. This
	// is deterministic and pinpoints the offending $ref without reporting every
	// definition already on the active traversal stack.
	var cycles []string
	seen := make(map[string]bool)
	var dfs func(string)
	dfs = func(node string) {
		colors[node] = gray
		neighbors := append([]string(nil), graph[node]...)
		sort.Strings(neighbors)
		for _, neighbor := range neighbors {
			switch colors[neighbor] {
			case gray:
				if !seen[node] {
					seen[node] = true
					cycles = append(cycles, node)
				}
			case white:
				dfs(neighbor)
			}
		}
		colors[node] = black
	}
	for _, name := range names {
		if colors[name] == white {
			dfs(name)
		}
	}
	return cycles
}

func validateCrossFields(valueSchemaRaw, umpireRaw json.RawMessage) []DefinitionIssue {
	var issues []DefinitionIssue
	var vs struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Defs       map[string]json.RawMessage `json:"$defs"`
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
		propRaw, exists := vs.Properties[fieldName]
		if !exists {
			issues = append(issues, DefinitionIssue{Code: "fieldMismatch", Path: "/valueSchema"})
			continue
		}

		if umpDef.IsEmpty != "" {
			if schemaType := resolveLocalSchemaType(propRaw, vs.Defs); schemaType != "" {
				if issue := checkIsEmptyCompatibility(fieldName, umpDef.IsEmpty, schemaType); issue != nil {
					issues = append(issues, *issue)
				}
			}
		}
		if len(umpDef.Default) > 0 {
			if issue := checkDefaultCompatibility(fieldName, umpDef.Default, propRaw, vs.Defs); issue != nil {
				issues = append(issues, *issue)
			}
		}
	}
	for fieldName := range vs.Properties {
		if _, exists := umpire.Fields[fieldName]; !exists {
			issues = append(issues, DefinitionIssue{Code: "fieldMismatch", Path: "/valueSchema"})
		}
	}
	return issues
}

// resolveLocalSchemaType follows local root $defs references until it finds an
// explicit type. Missing, external, malformed, and cyclic references have no
// usable type for cross-field compatibility.
func resolveLocalSchemaType(raw json.RawMessage, defs map[string]json.RawMessage) string {
	seen := make(map[string]bool)
	for {
		var schema struct {
			Type string `json:"type"`
			Ref  string `json:"$ref"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil || schema.Type != "" {
			return schema.Type
		}
		if !strings.HasPrefix(schema.Ref, "#/$defs/") {
			return ""
		}

		name := strings.TrimPrefix(schema.Ref, "#/$defs/")
		if name == "" || seen[name] {
			return ""
		}
		seen[name] = true
		var ok bool
		raw, ok = defs[name]
		if !ok {
			return ""
		}
	}
}

func dedupeDefinitionIssues(issues []DefinitionIssue) []DefinitionIssue {
	seen := make(map[DefinitionIssue]bool, len(issues))
	out := make([]DefinitionIssue, 0, len(issues))
	for _, issue := range issues {
		if !seen[issue] {
			seen[issue] = true
			out = append(out, issue)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Code < out[j].Code
	})
	return out
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
		// "present" is the portable no-emptiness strategy (spec rule 5 only
		// constrains non-present strategies), so it is compatible with any type.
	}
	return nil
}

func checkDefaultCompatibility(fieldName string, defaultRaw, propRaw json.RawMessage, defs map[string]json.RawMessage) *DefinitionIssue {
	invalid := func() *DefinitionIssue {
		return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", fieldName)}
	}

	value, err := decodeJSONNumber(defaultRaw)
	if err != nil {
		return invalid()
	}
	// Base Umpire v1 permits only primitive defaults. Keep that contract explicit
	// here even if an object or array property schema would otherwise accept one.
	switch value.(type) {
	case map[string]any, []any:
		return invalid()
	}

	schemas, ok := resolveLocalSchemaChain(propRaw, defs)
	if !ok {
		// Reference validation reports the malformed, missing, or cyclic target.
		// Avoid manufacturing a second default issue without a usable schema.
		return nil
	}
	for _, schema := range schemas {
		if !defaultMatchesSchema(value, schema) {
			return invalid()
		}
	}
	return nil
}

// resolveLocalSchemaChain returns the property schema followed by each root-$defs
// target. JSON Schema 2020-12 applies $ref siblings too, so default validation
// checks every schema object in the chain rather than only the terminal target.
func resolveLocalSchemaChain(raw json.RawMessage, defs map[string]json.RawMessage) ([]map[string]json.RawMessage, bool) {
	var schemas []map[string]json.RawMessage
	seen := make(map[string]bool)
	for {
		var schema map[string]json.RawMessage
		if err := json.Unmarshal(raw, &schema); err != nil {
			return nil, false
		}
		schemas = append(schemas, schema)

		refRaw, hasRef := schema["$ref"]
		if !hasRef {
			return schemas, true
		}
		var ref string
		if json.Unmarshal(refRaw, &ref) != nil || !strings.HasPrefix(ref, "#/$defs/") {
			return nil, false
		}
		target := strings.TrimPrefix(ref, "#/$defs/")
		if target == "" || seen[target] {
			return nil, false
		}
		seen[target] = true
		var exists bool
		raw, exists = defs[target]
		if !exists {
			return nil, false
		}
	}
}

func defaultMatchesSchema(value any, schema map[string]json.RawMessage) bool {
	if !isPrimitiveDefaultSchema(schema) {
		return false
	}

	if typeRaw, ok := schema["type"]; ok {
		var schemaType string
		if json.Unmarshal(typeRaw, &schemaType) != nil || !defaultMatchesType(value, schemaType) {
			return false
		}
	}

	if enumRaw, ok := schema["enum"]; ok {
		var candidates []json.RawMessage
		if json.Unmarshal(enumRaw, &candidates) != nil {
			return false
		}
		matched := false
		for _, candidateRaw := range candidates {
			candidate, err := decodeJSONNumber(candidateRaw)
			if err == nil && equalJSONPrimitive(value, candidate) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if constRaw, ok := schema["const"]; ok {
		constant, err := decodeJSONNumber(constRaw)
		if err != nil || !equalJSONPrimitive(value, constant) {
			return false
		}
	}

	if str, ok := value.(string); ok {
		length := float64(utf8.RuneCountInString(str))
		if !meetsLowerBound(length, schema["minLength"], true) || !meetsUpperBound(length, schema["maxLength"], true) {
			return false
		}
	}

	if number, ok := value.(json.Number); ok {
		v, err := number.Float64()
		if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return false
		}
		if !meetsLowerBound(v, schema["minimum"], true) ||
			!meetsUpperBound(v, schema["maximum"], true) ||
			!meetsLowerBound(v, schema["exclusiveMinimum"], false) ||
			!meetsUpperBound(v, schema["exclusiveMaximum"], false) {
			return false
		}
	}
	return true
}

func isPrimitiveDefaultSchema(schema map[string]json.RawMessage) bool {
	if typeRaw, ok := schema["type"]; ok {
		var schemaType string
		if json.Unmarshal(typeRaw, &schemaType) == nil {
			if schemaType == "object" || schemaType == "array" {
				return false
			}
		}
	}
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		if _, ok := schema[key]; ok {
			return false
		}
	}
	return true
}

func defaultMatchesType(value any, schemaType string) bool {
	switch schemaType {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		n, ok := value.(json.Number)
		if !ok {
			return false
		}
		v, err := n.Float64()
		return err == nil && !math.IsInf(v, 0) && !math.IsNaN(v)
	case "integer":
		n, ok := value.(json.Number)
		if !ok {
			return false
		}
		v, ok := new(big.Rat).SetString(n.String())
		if !ok || !v.IsInt() {
			return false
		}
		const maxSafe = int64(9007199254740991)
		limit := big.NewRat(maxSafe, 1)
		return v.Cmp(limit) <= 0 && v.Cmp(new(big.Rat).Neg(limit)) >= 0
	case "object", "array":
		// Object and array defaults are forbidden by the base Umpire v1 contract.
		return false
	default:
		return false
	}
}

func decodeJSONNumber(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func equalJSONPrimitive(a, b any) bool {
	an, aNumber := a.(json.Number)
	bn, bNumber := b.(json.Number)
	if aNumber || bNumber {
		if !aNumber || !bNumber {
			return false
		}
		ar, aOK := new(big.Rat).SetString(an.String())
		br, bOK := new(big.Rat).SetString(bn.String())
		return aOK && bOK && ar.Cmp(br) == 0
	}
	switch av := a.(type) {
	case nil:
		return b == nil
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	default:
		return false
	}
}

func meetsLowerBound(value float64, raw json.RawMessage, inclusive bool) bool {
	if len(raw) == 0 {
		return true
	}
	var bound float64
	if json.Unmarshal(raw, &bound) != nil || math.IsInf(bound, 0) || math.IsNaN(bound) {
		return false
	}
	if inclusive {
		return value >= bound
	}
	return value > bound
}

func meetsUpperBound(value float64, raw json.RawMessage, inclusive bool) bool {
	if len(raw) == 0 {
		return true
	}
	var bound float64
	if json.Unmarshal(raw, &bound) != nil || math.IsInf(bound, 0) || math.IsNaN(bound) {
		return false
	}
	if inclusive {
		return value <= bound
	}
	return value < bound
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
