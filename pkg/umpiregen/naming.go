package umpiregen

import (
	"encoding/json"
	"fmt"

	"github.com/umpire-tools/umpire-go-gen/pkg/codegen"
	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
)

// validateGeneratedSymbols checks every package-level name emitted by the
// profile path. Go scopes still apply to struct fields, whose local collisions
// are checked by validateGoSchemaNames. Package symbols are never repaired with
// numeric suffixes.
func validateGeneratedSymbols(umpireJSON, valueSchemaJSON []byte, cfg Config) []DefinitionIssue {
	schemaName := cfg.SchemaName
	fieldsName := cfg.FieldsName
	if fieldsName == "" {
		fieldsName = schemaName + "Fields"
	}
	conditionsName := cfg.ConditionsName
	if conditionsName == "" {
		conditionsName = schemaName + "Conditions"
	}

	var issues []DefinitionIssue
	for path, name := range map[string]string{
		"/generation/packageName":    cfg.PkgName,
		"/generation/schemaName":     schemaName,
		"/generation/fieldsName":     fieldsName,
		"/generation/conditionsName": conditionsName,
	} {
		if !validGeneratedIdentifier(name) {
			issues = append(issues, DefinitionIssue{Code: "invalidName", Path: path})
		}
	}
	if len(issues) != 0 {
		return dedupeDefinitionIssues(issues)
	}

	type origin struct{ path string }
	symbols := make(map[string]origin)
	add := func(name, path string) {
		if !validGeneratedIdentifier(name) {
			issues = append(issues, DefinitionIssue{Code: "invalidName", Path: path})
			return
		}
		if previous, exists := symbols[name]; exists && previous.path != path {
			issues = append(issues, DefinitionIssue{Code: "nameCollision", Path: path})
			return
		}
		symbols[name] = origin{path: path}
	}

	for _, item := range []struct{ name, path string }{
		{schemaName, "/generation/schemaName"},
		{fieldsName, "/generation/fieldsName"},
		{conditionsName, "/generation/conditionsName"},
		{schemaName + "Availability", "/generation/schemaName"},
		{schemaName + "StructuralIssue", "/generation/schemaName"},
		{schemaName + "StructuralError", "/generation/schemaName"},
		{"Validate" + schemaName + "JSON", "/generation/schemaName"},
		{"Decode" + schemaName, "/generation/schemaName"},
		{schemaName + "StructuralIssueAt", "/generation/schemaName"},
		{schemaName + "StructuralKind", "/generation/schemaName"},
		{schemaName + "StructuralIntParts", "/generation/schemaName"},
		{schemaName + "StructuralSort", "/generation/schemaName"},
		{"svalidate" + schemaName, "/generation/schemaName"},
		{"FieldStatus", "/generation/helpers"},
		{"Issue", "/generation/helpers"},
		{"RuleMetaEntry", "/generation/helpers"},
		{"ChallengeResult", "/generation/helpers"},
		{"Check", "/generation/helpers"},
		{"Challenge", "/generation/helpers"},
		{"contains", "/generation/helpers"},
		{"depSatisfied", "/generation/helpers"},
		{"escapePtr", "/generation/helpers"},
		{"emailRegex", "/generation/helpers"},
		{"urlRegex", "/generation/helpers"},
		{"integerRegex", "/generation/helpers"},
		{"numberRegex", "/generation/helpers"},
		{"isValidEmail", "/generation/helpers"},
		{"isValidRegexPattern", "/generation/helpers"},
		{"isValidRegexMatch", "/generation/helpers"},
		{"isValidURL", "/generation/helpers"},
		{"isValidInteger", "/generation/helpers"},
		{"isValidNumber", "/generation/helpers"},
		{"parseFloat", "/generation/helpers"},
		{"isInRange", "/generation/helpers"},
		{"ruleMeta", "/generation/helpers"},
	} {
		add(item.name, item.path)
	}

	var valueSchema map[string]json.RawMessage
	if json.Unmarshal(valueSchemaJSON, &valueSchema) == nil {
		var walk func(map[string]json.RawMessage, string, string, bool)
		walk = func(node map[string]json.RawMessage, hint, path string, declared bool) {
			if declared {
				add("svalidate"+hint, path)
			}
			var schemaType string
			_ = json.Unmarshal(node["type"], &schemaType)
			if _, union := node["oneOf"]; union {
				if !declared {
					add(hint, path)
					add("svalidate"+hint, path)
				}
				interfaceName := hint + "Value"
				add(interfaceName, path)
				add(hint+"Kind", path)
				add("svalidate"+hint+"Kind", path)
				var branches []json.RawMessage
				_ = json.Unmarshal(node["oneOf"], &branches)
				if !goNameableTaggedUnion(branches) {
					return
				}
				for i, raw := range branches {
					var branch map[string]json.RawMessage
					if json.Unmarshal(raw, &branch) != nil {
						continue
					}
					wire := unionDiscriminatorWire(branch)
					suffix := codegen.GoFieldName(wire)
					branchPath := fmt.Sprintf("%s/oneOf/%d", path, i)
					add(interfaceName+suffix, branchPath)
					add(hint+"Kind"+suffix, branchPath)
					walkProperties(branch, interfaceName+suffix, branchPath, walk)
				}
				return
			}
			if _, enum := node["enum"]; enum {
				name := hint
				if !declared {
					name += "Value"
					add(name, path)
					add("svalidate"+name, path)
				}
				var values []json.RawMessage
				_ = json.Unmarshal(node["enum"], &values)
				for i, raw := range values {
					add(name+enumSymbolSuffix(raw, i), fmt.Sprintf("%s/enum/%d", path, i))
				}
				return
			}
			switch schemaType {
			case "object":
				if !declared {
					add(hint, path)
					add("svalidate"+hint, path)
				}
				walkProperties(node, hint, path, walk)
			case "array":
				var items map[string]json.RawMessage
				if json.Unmarshal(node["items"], &items) == nil {
					walk(items, hint+"Item", path+"/items", false)
				}
			}
		}
		walkProperties(valueSchema, schemaName, "/valueSchema", walk)
		var defs map[string]json.RawMessage
		if json.Unmarshal(valueSchema["$defs"], &defs) == nil {
			for _, wire := range sortedRawKeys(defs) {
				var def map[string]json.RawMessage
				if json.Unmarshal(defs[wire], &def) != nil {
					continue
				}
				path := "/valueSchema/$defs/" + escapeProfilePointer(wire)
				name := schemaName + codegen.GoFieldName(wire)
				add(name, path)
				walk(def, name, path, true)
			}
		}
	}

	if parsed, err := schema.Parse(umpireJSON); err == nil {
		if inferred, err := codegen.InferTypes(parsed); err == nil {
			conditionNames := make(map[string]bool)
			for _, condition := range inferred.Conditions {
				name := codegen.GoFieldName(condition.Name)
				if !validGeneratedIdentifier(name) {
					issues = append(issues, DefinitionIssue{Code: "invalidName", Path: "/umpire/conditions/" + escapeProfilePointer(condition.Name)})
				}
				if conditionNames[name] {
					issues = append(issues, DefinitionIssue{Code: "nameCollision", Path: "/umpire/conditions"})
				}
				conditionNames[name] = true
			}
			seenGroups := make(map[string]bool)
			for _, branch := range inferred.Branches {
				if !seenGroups[branch.GroupName] {
					add(branch.GroupName, "/umpire/rules")
					add(branch.GroupName+"None", "/umpire/rules")
					seenGroups[branch.GroupName] = true
				}
				add(branch.Branch, "/umpire/rules")
			}
		}
	}

	return dedupeDefinitionIssues(issues)
}

func walkProperties(node map[string]json.RawMessage, owner, path string, walk func(map[string]json.RawMessage, string, string, bool)) {
	var props map[string]json.RawMessage
	if json.Unmarshal(node["properties"], &props) != nil {
		return
	}
	for _, wire := range sortedRawKeys(props) {
		var child map[string]json.RawMessage
		if json.Unmarshal(props[wire], &child) != nil {
			continue
		}
		walk(child, owner+codegen.GoFieldName(wire), path+"/properties/"+escapeProfilePointer(wire), false)
	}
}

func unionDiscriminatorWire(branch map[string]json.RawMessage) string {
	var required []string
	var props map[string]json.RawMessage
	if json.Unmarshal(branch["required"], &required) != nil || json.Unmarshal(branch["properties"], &props) != nil {
		return ""
	}
	for _, name := range required {
		var property map[string]json.RawMessage
		if json.Unmarshal(props[name], &property) != nil {
			continue
		}
		var wire string
		if json.Unmarshal(property["const"], &wire) == nil {
			return wire
		}
	}
	return ""
}

func enumSymbolSuffix(raw json.RawMessage, index int) string {
	var wire string
	if json.Unmarshal(raw, &wire) == nil {
		return codegen.GoFieldName(wire)
	}
	var boolean bool
	if json.Unmarshal(raw, &boolean) == nil && (string(raw) == "true" || string(raw) == "false") {
		if boolean {
			return "True"
		}
		return "False"
	}
	return fmt.Sprintf("Value%d", index+1)
}

// generationConfigIssues is kept separate from profile parsing because
// configured names are not members of the canonical profile document.
func generationConfigIssues(profile *Profile, cfg Config) []DefinitionIssue {
	if profile == nil {
		return []DefinitionIssue{{Code: "invalidProfile", Path: "/valueSchema"}}
	}
	return validateGeneratedSymbols(profile.UmpireJSON, profile.ValueSchemaJSON, cfg)
}
