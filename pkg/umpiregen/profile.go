package umpiregen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/token"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/umpire-tools/umpire-go-gen/pkg/codegen"
)

// ProfileSchemaURI is the required $schema value for a JSON Schema Composition Profile v1.
const ProfileSchemaURI = "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json"

const maxProfileSafeInteger int64 = 9007199254740991

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

// DefinitionError rejects profile compilation while retaining every normalized
// definition issue returned separately by GenerateProfile or GenerateComposed.
type DefinitionError struct {
	Issues []DefinitionIssue
}

func (e *DefinitionError) Error() string {
	if e == nil {
		return "<nil>"
	}
	msgs := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		msgs = append(msgs, issue.Error())
	}
	return fmt.Sprintf("profile definition issues: %s", strings.Join(msgs, "; "))
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
	if err := requireExactKeys(raw, "$schema", "profileVersion", "valueSchema", "umpire"); err != nil {
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

func requireExactKeys(raw map[string]json.RawMessage, keys ...string) error {
	allowed := make(map[string]bool, len(keys))
	for _, k := range keys {
		allowed[k] = true
		if _, ok := raw[k]; !ok {
			return fmt.Errorf("missing required key %q", k)
		}
	}
	for key := range raw {
		if !allowed[key] {
			return fmt.Errorf("unexpected key %q", key)
		}
	}
	return nil
}

// validateValueSchema walks the closed Profile v1 schema vocabulary. Schema
// paths are relative to valueSchema until an issue is emitted.
func validateValueSchema(raw json.RawMessage) []DefinitionIssue {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		return []DefinitionIssue{{Code: "invalidProfile", Path: "/valueSchema"}}
	}

	var defs map[string]json.RawMessage
	if defsRaw, ok := doc["$defs"]; ok && rawJSONObject(defsRaw, &defs) {
		// Root definitions are made available to every recursive reference check.
	}

	issues := validateSchemaObject("", raw, defs, schemaValidationContext{root: true})
	issues = append(issues, validateGoSchemaNames(doc)...)
	if defs != nil {
		for _, name := range detectCycles(buildRefGraph(defs)) {
			issues = append(issues, DefinitionIssue{
				Code: "referenceCycle",
				Path: "/valueSchema/$defs/" + escapeProfilePointer(name),
			})
		}
	}
	return issues
}

// supportedSchemaKeywords is deliberately an allowlist. Profile v1 is closed:
// every JSON Schema keyword not listed here is rejected rather than ignored.
var supportedSchemaKeywords = map[string]bool{
	"type": true, "properties": true, "required": true, "additionalProperties": true,
	"items": true, "minItems": true, "maxItems": true,
	"minLength": true, "maxLength": true,
	"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true,
	"enum": true, "const": true, "$ref": true, "oneOf": true,
	"title": true, "description": true,
	// These two are accepted only at the value-schema root.
	"$schema": true, "$defs": true,
}

type schemaValidationContext struct {
	root              bool
	untypedConstNames map[string]bool
	allowUntypedConst bool
}

func validateSchemaObject(prefix string, raw json.RawMessage, rootDefs map[string]json.RawMessage, ctx schemaValidationContext) []DefinitionIssue {
	path := "/valueSchema" + prefix
	var obj map[string]json.RawMessage
	if !rawJSONObject(raw, &obj) {
		return []DefinitionIssue{{Code: "invalidProfile", Path: path}}
	}

	var issues []DefinitionIssue
	unsupported := false
	keys := sortedRawKeys(obj)
	for _, key := range keys {
		if !supportedSchemaKeywords[key] || (key == "$schema" && !ctx.root) || (key == "$defs" && !ctx.root) {
			unsupported = true
			issues = append(issues, DefinitionIssue{
				Code: "unsupportedKeyword",
				Path: path + "/" + escapeProfilePointer(key),
			})
		}
	}
	for _, key := range []string{"title", "description"} {
		if raw, ok := obj[key]; ok {
			var value string
			if unmarshalProfileKeyword(raw, &value) != nil {
				issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path + "/" + key})
			}
		}
	}

	if refRaw, ok := obj["$ref"]; ok {
		var ref string
		targetExists := false
		if json.Unmarshal(refRaw, &ref) == nil {
			if target, ok := localDefinitionReference(ref); ok {
				_, targetExists = rootDefs[target]
			}
		}
		// A Profile v1 reference is the entire schema object. Annotations and
		// validation siblings would change 2020-12 reference semantics and are
		// therefore rejected too.
		if !targetExists || len(obj) != 1 {
			issuePath := path + "/$ref"
			if targetExists && len(obj) != 1 {
				issuePath = path
			}
			issues = append(issues, DefinitionIssue{Code: "invalidReference", Path: issuePath})
		}
		return issues
	}

	if oneOfRaw, ok := obj["oneOf"]; ok {
		if ctx.root {
			issues = append(issues, DefinitionIssue{Code: "unsupportedKeyword", Path: path + "/oneOf"})
		}
		issues = append(issues, validateOneOf(prefix, oneOfRaw, rootDefs, !ctx.root)...)
		if !ctx.root {
			// A tagged union is a complete schema shape in Profile v1.
			for _, key := range keys {
				if key != "oneOf" && key != "title" && key != "description" && supportedSchemaKeywords[key] {
					issues = append(issues, DefinitionIssue{Code: "invalidDiscriminator", Path: path + "/oneOf"})
					break
				}
			}
			return issues
		}
	}

	// The only intentionally untyped schema is a tagged-union discriminator.
	if ctx.allowUntypedConst {
		if _, ok := obj["const"]; ok {
			complete := true
			for _, key := range keys {
				if key != "const" && key != "title" && key != "description" {
					complete = false
					break
				}
			}
			if complete {
				return issues
			}
		}
	}

	var schemaType string
	typeRaw, hasType := obj["type"]
	if !hasType {
		// An unsupported applicator often forms an intentionally untyped schema.
		// Report the unsupported keyword, not a secondary shape issue.
		if !unsupported {
			issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path})
		}
		return issues
	}
	if unmarshalProfileKeyword(typeRaw, &schemaType) != nil {
		issuePath := path + "/type"
		if ctx.root {
			issuePath = path
		}
		issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: issuePath})
		return issues
	}
	if schemaType != "string" && schemaType != "number" && schemaType != "integer" && schemaType != "boolean" && schemaType != "array" && schemaType != "object" {
		issuePath := path + "/type"
		if ctx.root {
			issuePath = path
		}
		issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: issuePath})
		return issues
	}
	if ctx.root && schemaType != "object" {
		// validateVSMeta emits this same root issue; deduplication keeps one tuple.
		issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path})
		return issues
	}

	issues = append(issues, validateKeywordPlacement(path, schemaType, obj)...)
	issues = append(issues, validateEnumAndConst(path, schemaType, obj)...)

	switch schemaType {
	case "object":
		var props map[string]json.RawMessage
		propsRaw, hasProps := obj["properties"]
		if !hasProps || !rawJSONObject(propsRaw, &props) {
			issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path})
			return issues
		}
		var closed bool
		if rawClosed, ok := obj["additionalProperties"]; !ok {
			issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path})
		} else if unmarshalProfileKeyword(rawClosed, &closed) != nil || closed {
			issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path + "/additionalProperties"})
		}
		if requiredRaw, ok := obj["required"]; ok {
			var required []string
			if unmarshalProfileKeyword(requiredRaw, &required) != nil {
				issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path + "/required"})
			} else {
				seen := make(map[string]bool, len(required))
				for _, name := range required {
					if seen[name] || props[name] == nil {
						issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path + "/required"})
					}
					seen[name] = true
				}
			}
		}
		for _, name := range sortedRawKeys(props) {
			childCtx := schemaValidationContext{allowUntypedConst: ctx.untypedConstNames[name]}
			issues = append(issues, validateSchemaObject(prefix+"/properties/"+escapeProfilePointer(name), props[name], rootDefs, childCtx)...)
		}
		if ctx.root {
			if defsRaw, ok := obj["$defs"]; ok {
				var defs map[string]json.RawMessage
				if !rawJSONObject(defsRaw, &defs) {
					issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path + "/$defs"})
				} else {
					for _, name := range sortedRawKeys(defs) {
						issues = append(issues, validateSchemaObject(prefix+"/$defs/"+escapeProfilePointer(name), defs[name], rootDefs, schemaValidationContext{})...)
					}
				}
			}
		}
	case "array":
		itemsRaw, ok := obj["items"]
		if !ok {
			issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path})
		} else {
			var item map[string]json.RawMessage
			if !rawJSONObject(itemsRaw, &item) {
				issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path + "/items"})
			} else {
				issues = append(issues, validateSchemaObject(prefix+"/items", itemsRaw, rootDefs, schemaValidationContext{})...)
			}
		}
	}
	return issues
}

func validateKeywordPlacement(path, schemaType string, obj map[string]json.RawMessage) []DefinitionIssue {
	allowed := map[string]bool{"type": true, "title": true, "description": true, "enum": true, "const": true}
	switch schemaType {
	case "object":
		allowed["properties"], allowed["required"], allowed["additionalProperties"] = true, true, true
	case "array":
		allowed["items"], allowed["minItems"], allowed["maxItems"] = true, true, true
	case "string":
		allowed["minLength"], allowed["maxLength"] = true, true
	case "number", "integer":
		allowed["minimum"], allowed["maximum"] = true, true
		allowed["exclusiveMinimum"], allowed["exclusiveMaximum"] = true, true
	}
	allowed["$schema"], allowed["$defs"] = true, true // root-only checks happen separately.

	var issues []DefinitionIssue
	for _, key := range sortedRawKeys(obj) {
		if supportedSchemaKeywords[key] && !allowed[key] && key != "oneOf" {
			issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path + "/" + escapeProfilePointer(key)})
		}
	}
	for _, key := range []string{"minItems", "maxItems", "minLength", "maxLength"} {
		if raw, ok := obj[key]; ok {
			valid, safe := isNonNegativeProfileCount(raw)
			switch {
			case !valid:
				issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path + "/" + key})
			case !safe:
				issues = append(issues, DefinitionIssue{Code: "unsafeNumber", Path: path + "/" + key})
			}
		}
	}
	for _, key := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"} {
		if raw, ok := obj[key]; ok {
			if !isFiniteJSONNumber(raw) || (schemaType == "integer" && !isProfileSafeNumber(raw)) {
				issues = append(issues, DefinitionIssue{Code: "unsafeNumber", Path: path + "/" + key})
			}
		}
	}
	return issues
}

func validateEnumAndConst(path, schemaType string, obj map[string]json.RawMessage) []DefinitionIssue {
	var issues []DefinitionIssue
	if enumRaw, ok := obj["enum"]; ok {
		var rawValues []json.RawMessage
		if json.Unmarshal(enumRaw, &rawValues) != nil || len(rawValues) == 0 {
			issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path + "/enum"})
		} else {
			var seen []any
			for _, valueRaw := range rawValues {
				value, err := decodeJSONNumber(valueRaw)
				matches, unsafe := profileLiteralMatchesType(value, schemaType)
				duplicate := false
				for _, previous := range seen {
					if equalProfileLiteral(value, previous, schemaType) {
						duplicate = true
						break
					}
				}
				if unsafe {
					issues = append(issues, DefinitionIssue{Code: "unsafeNumber", Path: path + "/enum"})
					break
				}
				if err != nil || duplicate || !matches {
					issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path + "/enum"})
					break
				}
				seen = append(seen, value)
			}
		}
	}
	if constRaw, ok := obj["const"]; ok {
		value, err := decodeJSONNumber(constRaw)
		matches, unsafe := profileLiteralMatchesType(value, schemaType)
		switch {
		case unsafe:
			issues = append(issues, DefinitionIssue{Code: "unsafeNumber", Path: path + "/const"})
		case err != nil || !matches:
			issues = append(issues, DefinitionIssue{Code: "invalidProfile", Path: path + "/const"})
		}
	}
	return issues
}

func equalProfileLiteral(a, b any, schemaType string) bool {
	if schemaType == "number" {
		an, aOK := a.(json.Number)
		bn, bOK := b.(json.Number)
		if !aOK || !bOK {
			return false
		}
		af, aErr := an.Float64()
		bf, bErr := bn.Float64()
		return aErr == nil && bErr == nil && af == bf
	}
	return equalJSONPrimitive(a, b)
}

func profileLiteralMatchesType(value any, schemaType string) (matches, unsafe bool) {
	switch schemaType {
	case "string":
		_, matches = value.(string)
	case "boolean":
		_, matches = value.(bool)
	case "number":
		n, ok := value.(json.Number)
		if !ok {
			return false, false
		}
		f, err := n.Float64()
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return false, true
		}
		return true, false
	case "integer":
		n, ok := value.(json.Number)
		if !ok {
			return false, false
		}
		rat, ok := parseJSONRat(n.String())
		if !ok || !rat.IsInt() {
			return false, false
		}
		limit := big.NewRat(maxProfileSafeInteger, 1)
		if rat.Cmp(limit) > 0 || rat.Cmp(new(big.Rat).Neg(limit)) < 0 {
			return false, true
		}
		return true, false
	default:
		return false, false
	}
	return matches, false
}

// validateOneOf validates both the tagged-union contract and every branch
// schema. Recursive issues retain the exact /oneOf/<index> branch path.
func validateOneOf(prefix string, raw json.RawMessage, rootDefs map[string]json.RawMessage, requireTagged bool) []DefinitionIssue {
	oneOfPath := "/valueSchema" + prefix + "/oneOf"
	var branches []json.RawMessage
	if err := json.Unmarshal(raw, &branches); err != nil || len(branches) == 0 {
		if requireTagged {
			return []DefinitionIssue{{Code: "invalidDiscriminator", Path: oneOfPath}}
		}
		return nil
	}

	commonDiscriminator := ""
	seenValues := make(map[string]bool)
	invalidDiscriminator := false
	branchObjects := make([]map[string]json.RawMessage, len(branches))
	branchConstNames := make([]map[string]bool, len(branches))
	for i, branchRaw := range branches {
		var branch map[string]json.RawMessage
		if !rawJSONObject(branchRaw, &branch) {
			invalidDiscriminator = true
			continue
		}
		branchObjects[i] = branch
		var props map[string]json.RawMessage
		var required []string
		if !rawJSONObject(branch["properties"], &props) || unmarshalProfileKeyword(branch["required"], &required) != nil {
			invalidDiscriminator = true
			continue
		}
		requiredSet := make(map[string]bool, len(required))
		for _, name := range required {
			requiredSet[name] = true
		}
		var candidates []string
		values := make(map[string]string)
		branchConstNames[i] = make(map[string]bool)
		for name, propRaw := range props {
			var prop map[string]json.RawMessage
			if !rawJSONObject(propRaw, &prop) || !requiredSet[name] {
				continue
			}
			if constRaw, ok := prop["const"]; ok {
				branchConstNames[i][name] = true
				var value string
				if unmarshalProfileKeyword(constRaw, &value) == nil {
					candidates = append(candidates, name)
					values[name] = value
				}
			}
		}
		sort.Strings(candidates)
		if len(candidates) != 1 {
			invalidDiscriminator = true
			continue
		}
		name, value := candidates[0], values[candidates[0]]
		if commonDiscriminator == "" {
			commonDiscriminator = name
		} else if name != commonDiscriminator {
			invalidDiscriminator = true
		}
		if seenValues[value] {
			invalidDiscriminator = true
		}
		seenValues[value] = true
	}

	var issues []DefinitionIssue
	if requireTagged && (invalidDiscriminator || commonDiscriminator == "") {
		issues = append(issues, DefinitionIssue{Code: "invalidDiscriminator", Path: oneOfPath})
	}
	for i, branchRaw := range branches {
		ctx := schemaValidationContext{}
		if branchObjects[i] != nil {
			ctx.untypedConstNames = branchConstNames[i]
		}
		issues = append(issues, validateSchemaObject(fmt.Sprintf("%s/oneOf/%d", prefix, i), branchRaw, rootDefs, ctx)...)
	}
	return issues
}

// validateGoSchemaNames enforces the deterministic identifier conversion used
// by generated Go. Names are rejected rather than repaired or suffixed.
func validateGoSchemaNames(root map[string]json.RawMessage) []DefinitionIssue {
	var issues []DefinitionIssue
	var walk func(map[string]json.RawMessage, string)
	walk = func(node map[string]json.RawMessage, path string) {
		if enumRaw, ok := node["enum"]; ok {
			var values []json.RawMessage
			if json.Unmarshal(enumRaw, &values) == nil {
				seen := make(map[string]bool)
				collision := false
				for i, raw := range values {
					var wire string
					if json.Unmarshal(raw, &wire) != nil {
						continue // numeric and boolean enums use stable ValueN/True/False names
					}
					name := codegen.GoFieldName(wire)
					if !validGeneratedIdentifier(name) {
						issues = append(issues, DefinitionIssue{Code: "invalidName", Path: fmt.Sprintf("%s/enum/%d", path, i)})
					}
					if seen[name] {
						collision = true
					}
					seen[name] = true
				}
				if collision {
					issues = append(issues, DefinitionIssue{Code: "nameCollision", Path: path + "/enum"})
				}
			}
		}

		if propsRaw, ok := node["properties"]; ok {
			var props map[string]json.RawMessage
			if rawJSONObject(propsRaw, &props) {
				issues = append(issues, validateGoNameCollection(props, path+"/properties")...)
				for _, wire := range sortedRawKeys(props) {
					var child map[string]json.RawMessage
					if rawJSONObject(props[wire], &child) {
						walk(child, path+"/properties/"+escapeProfilePointer(wire))
					}
				}
			}
		}
		if itemsRaw, ok := node["items"]; ok {
			var items map[string]json.RawMessage
			if rawJSONObject(itemsRaw, &items) {
				walk(items, path+"/items")
			}
		}
		if oneOfRaw, ok := node["oneOf"]; ok {
			var branches []json.RawMessage
			discriminator, nameable := "", false
			if json.Unmarshal(oneOfRaw, &branches) == nil {
				discriminator, nameable = taggedUnionDiscriminator(branches)
			}
			if nameable {
				seenVariants := make(map[string]bool)
				variantCollision := false
				for i, raw := range branches {
					var branch map[string]json.RawMessage
					if !rawJSONObject(raw, &branch) {
						continue
					}
					walk(branch, fmt.Sprintf("%s/oneOf/%d", path, i))
					var props map[string]json.RawMessage
					if !rawJSONObject(branch["properties"], &props) {
						continue
					}
					propRaw := props[discriminator]
					var prop map[string]json.RawMessage
					if !rawJSONObject(propRaw, &prop) {
						continue
					}
					var wire string
					if json.Unmarshal(prop["const"], &wire) != nil {
						continue
					}
					name := codegen.GoFieldName(wire)
					constPath := fmt.Sprintf("%s/oneOf/%d/properties/%s/const", path, i, escapeProfilePointer(discriminator))
					if !validGeneratedIdentifier(name) {
						issues = append(issues, DefinitionIssue{Code: "invalidName", Path: constPath})
					}
					if seenVariants[name] {
						variantCollision = true
					}
					seenVariants[name] = true
				}
				if variantCollision {
					issues = append(issues, DefinitionIssue{Code: "nameCollision", Path: path + "/oneOf"})
				}
			}
		}
	}

	walk(root, "/valueSchema")
	if defsRaw, ok := root["$defs"]; ok {
		var defs map[string]json.RawMessage
		if rawJSONObject(defsRaw, &defs) {
			issues = append(issues, validateGoNameCollection(defs, "/valueSchema/$defs")...)
			for _, wire := range sortedRawKeys(defs) {
				var def map[string]json.RawMessage
				if rawJSONObject(defs[wire], &def) {
					walk(def, "/valueSchema/$defs/"+escapeProfilePointer(wire))
				}
			}
		}
	}
	return issues
}

func goNameableTaggedUnion(branches []json.RawMessage) bool {
	_, ok := taggedUnionDiscriminator(branches)
	return ok
}

func taggedUnionDiscriminator(branches []json.RawMessage) (string, bool) {
	if len(branches) == 0 {
		return "", false
	}
	discriminator := ""
	seenValues := make(map[string]bool)
	for _, raw := range branches {
		var branch map[string]json.RawMessage
		var props map[string]json.RawMessage
		var required []string
		if !rawJSONObject(raw, &branch) || !rawJSONObject(branch["properties"], &props) || unmarshalProfileKeyword(branch["required"], &required) != nil {
			return "", false
		}
		requiredSet := make(map[string]bool, len(required))
		for _, name := range required {
			requiredSet[name] = true
		}
		name, value := "", ""
		for propName, propRaw := range props {
			if !requiredSet[propName] {
				continue
			}
			var prop map[string]json.RawMessage
			if !rawJSONObject(propRaw, &prop) {
				continue
			}
			var candidate string
			if unmarshalProfileKeyword(prop["const"], &candidate) == nil {
				if name != "" {
					return "", false
				}
				name, value = propName, candidate
			}
		}
		if name == "" || discriminator != "" && discriminator != name || seenValues[value] {
			return "", false
		}
		discriminator = name
		seenValues[value] = true
	}
	return discriminator, true
}

func validateGoNameCollection(values map[string]json.RawMessage, path string) []DefinitionIssue {
	var issues []DefinitionIssue
	seen := make(map[string]bool, len(values))
	collision := false
	for _, wire := range sortedRawKeys(values) {
		name := codegen.GoFieldName(wire)
		if !validGeneratedIdentifier(name) {
			issues = append(issues, DefinitionIssue{Code: "invalidName", Path: path + "/" + escapeProfilePointer(wire)})
		}
		if seen[name] {
			collision = true
		}
		seen[name] = true
	}
	if collision {
		issues = append(issues, DefinitionIssue{Code: "nameCollision", Path: path})
	}
	return issues
}

func validGeneratedIdentifier(name string) bool {
	return token.IsIdentifier(name) && name != "_" && !token.Lookup(name).IsKeyword()
}

func rawJSONObject(raw json.RawMessage, out *map[string]json.RawMessage) bool {
	if unmarshalProfileKeyword(raw, out) != nil || *out == nil {
		return false
	}
	return true
}

// unmarshalProfileKeyword rejects JSON null before decoding a supported keyword.
// encoding/json otherwise accepts null into strings, booleans, slices, and maps
// without an error, leaving a zero value that can masquerade as a valid shape.
func unmarshalProfileKeyword(raw json.RawMessage, out any) error {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("keyword must not be null")
	}
	return json.Unmarshal(raw, out)
}

func sortedRawKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func escapeProfilePointer(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}

// localDefinitionReference resolves the one RFC 6901 token after #/$defs/.
// An unescaped slash would address below a definition and is outside Profile v1.
func localDefinitionReference(ref string) (string, bool) {
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return "", false
	}
	token := strings.TrimPrefix(ref, prefix)
	if token == "" || strings.Contains(token, "/") {
		return "", false
	}
	var decoded strings.Builder
	for i := 0; i < len(token); i++ {
		if token[i] != '~' {
			decoded.WriteByte(token[i])
			continue
		}
		if i+1 >= len(token) {
			return "", false
		}
		i++
		switch token[i] {
		case '0':
			decoded.WriteByte('~')
		case '1':
			decoded.WriteByte('/')
		default:
			return "", false
		}
	}
	return decoded.String(), true
}

func isNonNegativeProfileCount(raw json.RawMessage) (valid, safe bool) {
	value, err := decodeJSONNumber(raw)
	n, ok := value.(json.Number)
	if err != nil || !ok {
		return false, false
	}
	rat, ok := parseJSONRat(n.String())
	if !ok || !rat.IsInt() || rat.Sign() < 0 {
		return false, false
	}
	return true, rat.Cmp(big.NewRat(maxProfileSafeInteger, 1)) <= 0
}

func isProfileSafeNumber(raw json.RawMessage) bool {
	value, err := decodeJSONNumber(raw)
	n, ok := value.(json.Number)
	if err != nil || !ok {
		return false
	}
	rat, ok := parseJSONRat(n.String())
	if !ok {
		return false
	}
	limit := big.NewRat(maxProfileSafeInteger, 1)
	return rat.Cmp(limit) <= 0 && rat.Cmp(new(big.Rat).Neg(limit)) >= 0
}

func isFiniteJSONNumber(raw json.RawMessage) bool {
	value, err := decodeJSONNumber(raw)
	n, ok := value.(json.Number)
	if err != nil || !ok {
		return false
	}
	f, err := n.Float64()
	return err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
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
		if json.Unmarshal(refRaw, &ref) == nil {
			if target, ok := localDefinitionReference(ref); ok {
				targets = append(targets, target)
			}
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
		name, ok := localDefinitionReference(schema.Ref)
		if !ok || seen[name] {
			return ""
		}
		seen[name] = true
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
	compatible := true
	switch isEmpty {
	case "string":
		compatible = schemaType == "string"
	case "number":
		compatible = schemaType == "number" || schemaType == "integer"
	case "boolean":
		compatible = schemaType == "boolean"
	case "array":
		compatible = schemaType == "array"
	case "object":
		compatible = schemaType == "object"
	case "present":
		// "present" is the portable no-emptiness strategy (spec rule 5 only
		// constrains non-present strategies), so it is compatible with any type.
	}
	if !compatible {
		return &DefinitionIssue{Code: "incompatibleIsEmpty", Path: fmt.Sprintf("/umpire/fields/%s", escapeProfilePointer(fieldName))}
	}
	return nil
}

func checkDefaultCompatibility(fieldName string, defaultRaw, propRaw json.RawMessage, defs map[string]json.RawMessage) *DefinitionIssue {
	invalid := func() *DefinitionIssue {
		return &DefinitionIssue{Code: "invalidDefault", Path: fmt.Sprintf("/umpire/fields/%s/default", escapeProfilePointer(fieldName))}
	}

	value, err := decodeJSONNumber(defaultRaw)
	if err != nil || value == nil {
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
		if json.Unmarshal(refRaw, &ref) != nil {
			return nil, false
		}
		target, ok := localDefinitionReference(ref)
		if !ok || seen[target] {
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
		v, ok := parseJSONRat(n.String())
		if !ok || !v.IsInt() {
			return false
		}
		limit := big.NewRat(maxProfileSafeInteger, 1)
		return v.Cmp(limit) <= 0 && v.Cmp(new(big.Rat).Neg(limit)) >= 0
	case "object", "array":
		// Object and array defaults are forbidden by the base Umpire v1 contract.
		return false
	default:
		return false
	}
}

func parseJSONRat(text string) (*big.Rat, bool) {
	text = strings.TrimSpace(text)
	negative := strings.HasPrefix(text, "-")
	if negative {
		text = text[1:]
	}
	exponent := 0
	if at := strings.IndexAny(text, "eE"); at >= 0 {
		parsed, err := strconv.Atoi(text[at+1:])
		if err != nil {
			return nil, false
		}
		exponent = parsed
		text = text[:at]
	}
	fractionDigits := 0
	if dot := strings.IndexByte(text, '.'); dot >= 0 {
		fractionDigits = len(text) - dot - 1
		text = text[:dot] + text[dot+1:]
	}
	exponent -= fractionDigits
	if exponent > 4096 || exponent < -4096 {
		return nil, false
	}
	integer := new(big.Int)
	if _, ok := integer.SetString(text, 10); !ok {
		return nil, false
	}
	if negative {
		integer.Neg(integer)
	}
	magnitude := exponent
	if magnitude < 0 {
		magnitude = -magnitude
	}
	power := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(magnitude)), nil)
	if exponent >= 0 {
		integer.Mul(integer, power)
		return new(big.Rat).SetInt(integer), true
	}
	return new(big.Rat).SetFrac(integer, power), true
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
		ar, aOK := parseJSONRat(an.String())
		br, bOK := parseJSONRat(bn.String())
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
	if pr == nil || len(pr.Issues) == 0 {
		return nil
	}
	return &DefinitionError{Issues: append([]DefinitionIssue(nil), pr.Issues...)}
}
