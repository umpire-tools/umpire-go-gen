package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"unicode"
)

// Parse reads a public Umpire v1 document and converts it to the internal model.
func Parse(data []byte) (*Schema, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if err := validatePublicSchema(raw); err != nil {
		return nil, err
	}

	var spec specSchema
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("invalid public schema: %w", err)
	}
	s := convertSpecSchema(&spec, captureRuleBranchesOrder(data))
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

type specSchema struct {
	Version    int                         `json:"version"`
	Fields     map[string]specFieldDef     `json:"fields"`
	Conditions map[string]specCondDef      `json:"conditions"`
	Rules      []specRuleDef               `json:"rules"`
	Validators map[string]specValidatorDef `json:"validators,omitempty"`
	Excluded   []specExcluded              `json:"excluded,omitempty"`
}

type specFieldDef struct {
	Required bool `json:"required"`
	IsEmpty  any  `json:"isEmpty,omitempty"`
	Default  any  `json:"default,omitempty"`
}

type specValidatorDef struct {
	Op      string   `json:"op"`
	Pattern string   `json:"pattern,omitempty"`
	Value   *float64 `json:"value,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type specCondDef struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type specRuleDef struct {
	Type         string           `json:"type"`
	Field        string           `json:"field,omitempty"`
	Fields       []string         `json:"fields,omitempty"`
	When         *json.RawMessage `json:"when,omitempty"`
	Check        *json.RawMessage `json:"check,omitempty"`
	Op           string           `json:"op,omitempty"`
	Pattern      string           `json:"pattern,omitempty"`
	Reason       string           `json:"reason,omitempty"`
	Requires     []string         `json:"requires,omitempty"`
	Dependency   string           `json:"dependency,omitempty"`
	Dependencies []any            `json:"dependencies,omitempty"`
	DependsOn    string           `json:"dependsOn,omitempty"`
	Source       string           `json:"source,omitempty"`
	Targets      []string         `json:"targets,omitempty"`
	Excluded     bool             `json:"excluded,omitempty"`
	Branches     any              `json:"branches,omitempty"`
	Group        string           `json:"group,omitempty"`
	Rules        []specRuleDef    `json:"rules,omitempty"`
	Min          *float64         `json:"min,omitempty"`
	Max          *float64         `json:"max,omitempty"`
	Value        *float64         `json:"value,omitempty"`
}

type specExcluded struct {
	Type        string `json:"type"`
	Field       string `json:"field,omitempty"`
	Description string `json:"description,omitempty"`
}

func validatePublicSchema(doc map[string]json.RawMessage) error {
	if err := closedMembers(doc, "top-level schema", "version", "conditions", "fields", "rules", "validators", "excluded"); err != nil {
		return fmt.Errorf("fields: %w", err)
	}
	fields, ok := doc["fields"]
	if !ok {
		return fmt.Errorf("fields is required")
	}
	fieldDefs, err := rawObject(fields, "fields")
	if err != nil {
		return err
	}
	if len(fieldDefs) == 0 {
		return fmt.Errorf("fields must contain at least one field")
	}
	version, ok := doc["version"]
	if !ok || !isNumber(version) || !isOne(version) {
		return fmt.Errorf("version must be the numeric literal 1")
	}
	for name, def := range fieldDefs {
		if err := validateFieldDef(name, def); err != nil {
			return err
		}
	}
	rules, ok := doc["rules"]
	if !ok {
		return fmt.Errorf("rules is required")
	}
	if err := validateRules(rules); err != nil {
		return err
	}
	if raw, ok := doc["conditions"]; ok {
		conditions, err := rawObject(raw, "conditions")
		if err != nil {
			return err
		}
		for name, def := range conditions {
			if err := validateConditionDef(name, def); err != nil {
				return err
			}
		}
	}
	if raw, ok := doc["validators"]; ok {
		validators, err := rawObject(raw, "validators")
		if err != nil {
			return err
		}
		for name, def := range validators {
			if err := validateValidatorDef(name, def, true); err != nil {
				return err
			}
		}
	}
	if raw, ok := doc["excluded"]; ok {
		excluded, err := rawArray(raw, "excluded")
		if err != nil {
			return err
		}
		for _, def := range excluded {
			if err := validateExcluded(def); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateFieldDef(name string, raw json.RawMessage) error {
	def, err := rawObject(raw, "field "+name)
	if err != nil {
		return err
	}
	if err := closedMembers(def, "field "+name, "required", "default", "isEmpty"); err != nil {
		return err
	}
	if v, ok := def["required"]; ok && !isBool(v) {
		return fmt.Errorf("field %q required must be boolean", name)
	}
	if v, ok := def["default"]; ok && !isPrimitive(v) {
		return fmt.Errorf("field %q default must be a JSON primitive", name)
	}
	if v, ok := def["isEmpty"]; ok {
		value, ok := stringValue(v)
		if !ok || !oneOf(value, "string", "number", "boolean", "array", "object", "present") {
			return fmt.Errorf("field %q isEmpty is invalid", name)
		}
	}
	return nil
}

func validateConditionDef(name string, raw json.RawMessage) error {
	def, err := rawObject(raw, "condition "+name)
	if err != nil {
		return err
	}
	if err := closedMembers(def, "condition "+name, "type", "description"); err != nil {
		return err
	}
	typeName, ok := requiredString(def, "type")
	if !ok || !oneOf(typeName, "boolean", "string", "number", "string[]", "number[]") {
		return fmt.Errorf("condition %q has an invalid type", name)
	}
	if v, ok := def["description"]; ok && !isString(v) {
		return fmt.Errorf("condition %q description must be a string", name)
	}
	return nil
}

func validateExcluded(raw json.RawMessage) error {
	def, err := rawObject(raw, "Excluded")
	if err != nil {
		return err
	}
	if err := closedMembers(def, "Excluded", "type", "field", "description", "key", "signature"); err != nil {
		return err
	}
	typeName, typeOK := requiredString(def, "type")
	description, descriptionOK := requiredString(def, "description")
	if !typeOK || !descriptionOK || typeName == "" || description == "" {
		return fmt.Errorf("Excluded type and description must be non-empty strings")
	}
	for _, key := range []string{"field", "key", "signature"} {
		if v, ok := def[key]; ok && !isString(v) {
			return fmt.Errorf("Excluded %s must be a string", key)
		}
	}
	return nil
}

func validateRules(raw json.RawMessage) error {
	rules, err := rawArray(raw, "rules")
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if err := validateRule(rule); err != nil {
			return err
		}
	}
	return nil
}

func validateRule(raw json.RawMessage) error {
	rule, err := rawObject(raw, "rule")
	if err != nil {
		return err
	}
	typeName, ok := requiredString(rule, "type")
	if !ok {
		return fmt.Errorf("rule type is required")
	}
	switch typeName {
	case "requires":
		if err := closedMembers(rule, "requires rule", "type", "field", "dependency", "dependencies", "when", "reason"); err != nil {
			return err
		}
		if !hasString(rule, "field") {
			return fmt.Errorf("requires field is required")
		}
		variants := 0
		if hasString(rule, "dependency") {
			variants++
		}
		if _, present := rule["dependencies"]; present {
			variants++
			deps, err := rawArray(rule["dependencies"], "dependencies")
			if err != nil {
				return err
			}
			if len(deps) == 0 {
				return fmt.Errorf("dependencies must not be empty")
			}
			for _, dep := range deps {
				if isString(dep) {
					continue
				}
				if err := validateExpr(dep); err != nil {
					return err
				}
			}
		}
		if when, present := rule["when"]; present {
			variants++
			if err := validateExpr(when); err != nil {
				return err
			}
		}
		if variants != 1 {
			return fmt.Errorf("requires rule needs exactly one dependency form")
		}
		return optionalReason(rule)
	case "enabledWhen", "fairWhen":
		if err := closedMembers(rule, typeName+" rule", "type", "field", "when", "reason"); err != nil {
			return err
		}
		if !hasString(rule, "field") {
			return fmt.Errorf("%s field is required", typeName)
		}
		when, present := rule["when"]
		if !present {
			return fmt.Errorf("%s when is required", typeName)
		}
		if err := validateExpr(when); err != nil {
			return err
		}
		return optionalReason(rule)
	case "disables":
		if err := closedMembers(rule, "disables rule", "type", "source", "when", "targets", "reason"); err != nil {
			return err
		}
		if err := stringArrayMember(rule, "targets", true); err != nil {
			return err
		}
		variants := 0
		if hasString(rule, "source") {
			variants++
		}
		if when, present := rule["when"]; present {
			variants++
			if err := validateExpr(when); err != nil {
				return err
			}
		}
		if variants != 1 {
			return fmt.Errorf("disables rule needs source or when")
		}
		return optionalReason(rule)
	case "oneOf", "eitherOf":
		if err := closedMembers(rule, typeName+" rule", "type", "group", "branches"); err != nil {
			return err
		}
		if !hasString(rule, "group") {
			return fmt.Errorf("%s group is required", typeName)
		}
		branchesRaw, present := rule["branches"]
		if !present {
			return fmt.Errorf("%s branches is required", typeName)
		}
		branches, err := rawObject(branchesRaw, "branches")
		if err != nil {
			return err
		}
		for _, branch := range branches {
			entries, err := rawArray(branch, "branch")
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				return fmt.Errorf("branch must not be empty")
			}
			for _, entry := range entries {
				if typeName == "oneOf" {
					if !isString(entry) {
						return fmt.Errorf("branch fields must be strings")
					}
				} else if err := validateRule(entry); err != nil {
					return err
				}
			}
		}
		return nil
	case "anyOf":
		if err := closedMembers(rule, "anyOf rule", "type", "rules"); err != nil {
			return err
		}
		nested, present := rule["rules"]
		if !present {
			return fmt.Errorf("anyOf rules is required")
		}
		rules, err := rawArray(nested, "anyOf rules")
		if err != nil {
			return err
		}
		if len(rules) == 0 {
			return fmt.Errorf("anyOf rules must not be empty")
		}
		for _, nestedRule := range rules {
			if err := validateRule(nestedRule); err != nil {
				return err
			}
		}
		return nil
	case "check":
		if err := closedMembers(rule, "check rule", "type", "field", "reason", "op", "pattern", "value", "min", "max"); err != nil {
			return err
		}
		if !hasString(rule, "field") {
			return fmt.Errorf("check field is required")
		}
		if err := optionalReason(rule); err != nil {
			return err
		}
		return validateValidatorMap(validatorSpec(rule), false)
	default:
		return fmt.Errorf("unsupported rule type %q", typeName)
	}
}

func validateExpr(raw json.RawMessage) error {
	expr, err := rawObject(raw, "expression")
	if err != nil {
		return err
	}
	op, ok := requiredString(expr, "op")
	if !ok {
		return fmt.Errorf("expression op is required")
	}
	switch op {
	case "eq", "neq":
		if err := closedMembers(expr, "expression", "op", "field", "value"); err != nil {
			return err
		}
		if !hasString(expr, "field") || !isPrimitive(expr["value"]) {
			return fmt.Errorf("expression %s requires field and primitive value", op)
		}
	case "gt", "gte", "lt", "lte":
		if err := closedMembers(expr, "expression", "op", "field", "value"); err != nil {
			return err
		}
		if !hasString(expr, "field") || !isNumber(expr["value"]) {
			return fmt.Errorf("expression %s requires field and numeric value", op)
		}
	case "present", "absent", "truthy", "falsy":
		if err := closedMembers(expr, "expression", "op", "field"); err != nil {
			return err
		}
		if !hasString(expr, "field") {
			return fmt.Errorf("expression %s requires field", op)
		}
	case "in", "notIn":
		if err := closedMembers(expr, "expression", "op", "field", "values"); err != nil {
			return err
		}
		if !hasString(expr, "field") {
			return fmt.Errorf("expression %s requires field", op)
		}
		if err := primitiveArray(expr["values"], "values"); err != nil {
			return err
		}
	case "cond":
		if err := closedMembers(expr, "expression", "op", "condition"); err != nil {
			return err
		}
		if !hasString(expr, "condition") {
			return fmt.Errorf("expression cond requires condition")
		}
	case "condEq":
		if err := closedMembers(expr, "expression", "op", "condition", "value"); err != nil {
			return err
		}
		if !hasString(expr, "condition") || !isPrimitive(expr["value"]) {
			return fmt.Errorf("expression condEq requires condition and primitive value")
		}
	case "condIn":
		if err := closedMembers(expr, "expression", "op", "condition", "values"); err != nil {
			return err
		}
		if !hasString(expr, "condition") {
			return fmt.Errorf("expression condIn requires condition")
		}
		if err := primitiveArray(expr["values"], "values"); err != nil {
			return err
		}
	case "fieldInCond":
		if err := closedMembers(expr, "expression", "op", "field", "condition"); err != nil {
			return err
		}
		if !hasString(expr, "field") || !hasString(expr, "condition") {
			return fmt.Errorf("expression fieldInCond requires field and condition")
		}
	case "and", "or":
		if err := closedMembers(expr, "expression", "op", "exprs"); err != nil {
			return err
		}
		children, err := rawArray(expr["exprs"], "exprs")
		if err != nil {
			return err
		}
		if len(children) == 0 {
			return fmt.Errorf("expression %s requires non-empty exprs", op)
		}
		for _, child := range children {
			if err := validateExpr(child); err != nil {
				return err
			}
		}
	case "not":
		if err := closedMembers(expr, "expression", "op", "expr"); err != nil {
			return err
		}
		child, present := expr["expr"]
		if !present {
			return fmt.Errorf("expression not requires expr")
		}
		if err := validateExpr(child); err != nil {
			return err
		}
	case "check":
		if err := closedMembers(expr, "expression", "op", "field", "check"); err != nil {
			return err
		}
		if !hasString(expr, "field") {
			return fmt.Errorf("expression check requires field")
		}
		check, present := expr["check"]
		if !present {
			return fmt.Errorf("expression check requires validator")
		}
		if err := validateValidatorDef("check", check, false); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported expression op %q", op)
	}
	return nil
}

func validateValidatorDef(name string, raw json.RawMessage, allowError bool) error {
	def, err := rawObject(raw, "validator "+name)
	if err != nil {
		return err
	}
	return validateValidatorMap(def, allowError)
}

func validatorSpec(def map[string]json.RawMessage) map[string]json.RawMessage {
	spec := make(map[string]json.RawMessage)
	for _, key := range []string{"op", "pattern", "value", "min", "max", "error"} {
		if value, ok := def[key]; ok {
			spec[key] = value
		}
	}
	return spec
}

func validateValidatorMap(def map[string]json.RawMessage, allowError bool) error {
	allowed := []string{"op", "pattern", "value", "min", "max"}
	if allowError {
		allowed = append(allowed, "error")
	}
	if err := closedMembers(def, "validator", allowed...); err != nil {
		return err
	}
	op, ok := requiredString(def, "op")
	if !ok {
		return fmt.Errorf("validator op is required")
	}
	if allowError {
		if v, present := def["error"]; present && !isString(v) {
			return fmt.Errorf("validator error must be a string")
		}
	}
	switch op {
	case "email", "url", "integer":
		if len(def) != 1 && !(allowError && len(def) == 2) {
			return fmt.Errorf("validator %s has invalid members", op)
		}
	case "matches":
		if !hasString(def, "pattern") || len(def) != 2 && !(allowError && len(def) == 3) {
			return fmt.Errorf("validator matches requires pattern")
		}
	case "minLength", "maxLength", "min", "max":
		if !isNumber(def["value"]) || len(def) != 2 && !(allowError && len(def) == 3) {
			return fmt.Errorf("validator %s requires value", op)
		}
	case "range":
		if !isNumber(def["min"]) || !isNumber(def["max"]) || len(def) != 3 && !(allowError && len(def) == 4) {
			return fmt.Errorf("validator range requires min and max")
		}
	default:
		return fmt.Errorf("unsupported validator op %q", op)
	}
	return nil
}

func closedMembers(object map[string]json.RawMessage, context string, allowed ...string) error {
	for key := range object {
		if !oneOf(key, allowed...) {
			return fmt.Errorf("%s has unexpected member %q", context, key)
		}
	}
	return nil
}
func rawObject(raw json.RawMessage, context string) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, fmt.Errorf("%s must be an object", context)
	}
	return value, nil
}
func rawArray(raw json.RawMessage, context string) ([]json.RawMessage, error) {
	var value []json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, fmt.Errorf("%s must be an array", context)
	}
	return value, nil
}
func stringValue(raw json.RawMessage) (string, bool) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '"' {
		return "", false
	}
	var value string
	return value, json.Unmarshal(raw, &value) == nil
}
func requiredString(object map[string]json.RawMessage, key string) (string, bool) {
	value, ok := object[key]
	if !ok {
		return "", false
	}
	return stringValue(value)
}
func hasString(object map[string]json.RawMessage, key string) bool {
	_, ok := requiredString(object, key)
	return ok
}
func isString(raw json.RawMessage) bool { _, ok := stringValue(raw); return ok }
func isBool(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || (raw[0] != 't' && raw[0] != 'f') {
		return false
	}
	var value bool
	return json.Unmarshal(raw, &value) == nil
}
func isNumber(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] == 'n' || raw[0] == '"' || raw[0] == 't' || raw[0] == 'f' {
		return false
	}
	var value float64
	return json.Unmarshal(raw, &value) == nil
}
func isOne(raw json.RawMessage) bool {
	var value float64
	return json.Unmarshal(raw, &value) == nil && value == 1
}
func isPrimitive(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	switch value.(type) {
	case nil, string, float64, bool:
		return true
	}
	return false
}
func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
func optionalReason(object map[string]json.RawMessage) error {
	if raw, ok := object["reason"]; ok && !isString(raw) {
		return fmt.Errorf("reason must be a string")
	}
	return nil
}
func stringArrayMember(object map[string]json.RawMessage, key string, required bool) error {
	raw, ok := object[key]
	if !ok {
		if required {
			return fmt.Errorf("%s is required", key)
		}
		return nil
	}
	values, err := rawArray(raw, key)
	if err != nil {
		return err
	}
	for _, value := range values {
		if !isString(value) {
			return fmt.Errorf("%s entries must be strings", key)
		}
	}
	return nil
}
func primitiveArray(raw json.RawMessage, context string) error {
	values, err := rawArray(raw, context)
	if err != nil {
		return err
	}
	for _, value := range values {
		if !isPrimitive(value) {
			return fmt.Errorf("%s entries must be JSON primitives", context)
		}
	}
	return nil
}

func defaultCheckReason(op string, value, min, max *float64) string {
	switch op {
	case "email":
		return "Must be a valid email address"
	case "url":
		return "Must be a valid URL"
	case "integer":
		return "Must be a whole number"
	case "matches":
		return "Must match the required format"
	case "minLength":
		if value != nil {
			return fmt.Sprintf("Must be at least %g characters", *value)
		}
	case "maxLength":
		if value != nil {
			return fmt.Sprintf("Must be %g characters or fewer", *value)
		}
	case "min":
		if value != nil {
			return fmt.Sprintf("Must be at least %g", *value)
		}
	case "max":
		if value != nil {
			return fmt.Sprintf("Must be %g or less", *value)
		}
	case "range":
		if min != nil && max != nil {
			return fmt.Sprintf("Must be between %g and %g", *min, *max)
		}
	}
	return "Validation failed"
}

func convertSpecSchema(ss *specSchema, ruleBranchOrder map[int][]string) *Schema {
	s := &Schema{
		Fields:     make([]FieldDef, 0, len(ss.Fields)),
		Conditions: make([]ConditionDef, 0, len(ss.Conditions)),
		Rules:      make([]Rule, 0, len(ss.Rules)),
	}

	fieldNames := make([]string, 0, len(ss.Fields))
	for name := range ss.Fields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	for _, name := range fieldNames {
		fd := ss.Fields[name]
		def := FieldDef{Name: name, Required: fd.Required}
		if v, ok := fd.IsEmpty.(string); ok {
			def.IsEmpty = v
		}
		s.Fields = append(s.Fields, def)
	}

	condNames := make([]string, 0, len(ss.Conditions))
	for name := range ss.Conditions {
		condNames = append(condNames, name)
	}
	sort.Strings(condNames)
	for _, name := range condNames {
		cd := ss.Conditions[name]
		s.Conditions = append(s.Conditions, ConditionDef{Name: name, Type: cd.Type})
	}

	for i, rule := range ss.Rules {
		convertSpecRule(rule, s, ruleBranchOrder[i])
	}
	if len(ss.Validators) > 0 {
		s.Validators = make(map[string]ValidatorDef, len(ss.Validators))
		for name, validator := range ss.Validators {
			s.Validators[name] = ValidatorDef{
				Op: validator.Op, Pattern: validator.Pattern, Value: validator.Value,
				Min: validator.Min, Max: validator.Max, Error: validator.Error,
			}
		}
	}
	for _, ex := range ss.Excluded {
		if ex.Field != "" {
			s.Rules = append(s.Rules, Rule{
				Type:        "excluded",
				Field:       ex.Field,
				Excluded:    true,
				Description: ex.Description,
			})
		}
	}
	return s
}

func convertSpecRule(sr specRuleDef, s *Schema, branchOrder []string) {
	switch sr.Type {
	case "oneOf", "eitherOf":
		convertBranchRule(sr, s, branchOrder)
	case "anyOf":
		convertAnyOfRule(sr, s)
	case "disables":
		convertDisablesRule(sr, s)
	case "requires":
		convertRequiresRule(sr, s)
	default:
		r := Rule{
			Type:      sr.Type,
			Field:     sr.Field,
			Fields:    sr.Fields,
			Reason:    sr.Reason,
			Requires:  sr.Requires,
			Excluded:  sr.Excluded,
			DependsOn: sr.DependsOn,
		}
		if sr.When != nil {
			if e, err := rawMsgToExpr(sr.When); err == nil && e != nil {
				switch sr.Type {
				case "enabledWhen":
					r.Expr = e
				case "fairWhen":
					r.FairWhen = e
				case "disables":
					r.DisabledWhen = e
				default:
					r.Expr = e
				}
			}
		}
		if sr.Check != nil {
			if e, err := rawMsgToExpr(sr.Check); err == nil && e != nil {
				e.Field = sr.Field
				r.Check = e
			}
		}
		if sr.Type == "check" && sr.Op != "" {
			e := &Expr{Op: sr.Op, Field: sr.Field}
			if r.Reason == "" {
				r.Reason = defaultCheckReason(sr.Op, sr.Value, sr.Min, sr.Max)
			}
			switch sr.Op {
			case "matches":
				e.Pattern, e.Value = sr.Pattern, sr.Pattern
			case "minLength", "maxLength", "min", "max":
				if sr.Value != nil {
					e.Value = *sr.Value
				}
			case "range":
				if sr.Min != nil && sr.Max != nil {
					e.Value = map[string]float64{"min": *sr.Min, "max": *sr.Max}
				}
			}
			r.Check = e
		}
		s.Rules = append(s.Rules, r)
	}
}

func convertBranchRule(sr specRuleDef, s *Schema, branchOrder []string) {
	targetField := sr.Field
	var branchFields []string
	branches, _ := sr.Branches.(map[string]any)
	if targetField == "" {
		for _, branchVal := range branches {
			if vals, ok := branchVal.([]any); ok && len(vals) > 0 {
				if fn, ok := vals[0].(string); ok {
					targetField = fn
					break
				}
				if ruleMap, ok := vals[0].(map[string]any); ok {
					if fn, ok := ruleMap["field"].(string); ok {
						targetField = fn
						break
					}
				}
			}
		}
	}

	if len(branches) > 0 {
		isRuleBased := false
		for _, branchVal := range branches {
			vals, ok := branchVal.([]any)
			if ok && len(vals) > 0 {
				_, isRuleBased = vals[0].(map[string]any)
			}
			if isRuleBased {
				break
			}
		}
		names := orderedBranchNames(branches, branchOrder)
		if isRuleBased {
			ensureBranchMaps(s)
			for _, branchName := range names {
				vals, _ := branches[branchName].([]any)
				var exprs []Expr
				var subs []*Expr
				var reasons []string
				branchType := ""
				for _, raw := range vals {
					ruleMap, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					if t, ok := ruleMap["type"].(string); ok && branchType == "" {
						branchType = t
					}
					if whenRaw, ok := ruleMap["when"]; ok {
						e := exprFromAny(whenRaw)
						if e != nil {
							exprs = append(exprs, *e)
							subs = append(subs, e)
						}
					}
					if reason, ok := ruleMap["reason"].(string); ok && reason != "" {
						reasons = append(reasons, reason)
					}
				}
				if len(exprs) == 0 {
					continue
				}
				branchFields = append(branchFields, branchName)
				branchExpr := exprs[0]
				if len(exprs) > 1 {
					branchExpr = Expr{Op: "and", Exprs: exprs}
				}
				s.BranchExpressions[branchName] = &branchExpr
				s.BranchReasons[branchName] = reasons
				s.BranchSubConditions[branchName] = subs
				s.BranchSubReasons[branchName] = reasons
				if branchType != "" {
					s.BranchRuleTypes[branchName] = branchType
				}
				s.BranchOrder = append(s.BranchOrder, branchName)
			}
			if targetField != "" {
				s.FieldBranches[targetField] = branchFields
			}
		} else {
			if s.BranchKeys == nil {
				s.BranchKeys = make(map[string]string)
			}
			for _, branchKey := range names {
				vals, _ := branches[branchKey].([]any)
				for _, value := range vals {
					if fn, ok := value.(string); ok {
						branchFields = append(branchFields, fn)
						s.BranchKeys[goFieldName(fn)] = branchKey
					}
				}
			}
		}
	}

	groupName := sr.Group
	if groupName == "" {
		groupName = targetField
	}
	if targetField != "" {
		s.Rules = append(s.Rules, Rule{
			Type:     sr.Type,
			Field:    targetField,
			Fields:   []string{groupName + "Branch"},
			Group:    groupName,
			Branches: branchFields,
		})
	}
}

func convertAnyOfRule(sr specRuleDef, s *Schema) {
	targetField := sr.Field
	if targetField == "" {
		for _, r := range sr.Rules {
			if r.Field != "" {
				targetField = r.Field
				break
			}
		}
	}
	if targetField == "" {
		return
	}
	ensureBranchMaps(s)
	var exprs []Expr
	var subs []*Expr
	var reasons []string
	for _, r := range sr.Rules {
		if r.When == nil {
			continue
		}
		e, err := rawMsgToExpr(r.When)
		if err != nil || e == nil {
			continue
		}
		exprs = append(exprs, *e)
		subs = append(subs, e)
		if r.Reason != "" {
			reasons = append(reasons, r.Reason)
		}
	}
	if len(exprs) == 0 {
		return
	}
	branchExpr := exprs[0]
	if len(exprs) > 1 {
		branchExpr = Expr{Op: "or", Exprs: exprs}
	}
	s.BranchExpressions[targetField] = &branchExpr
	s.BranchReasons[targetField] = reasons
	s.BranchSubConditions[targetField] = subs
	s.BranchSubReasons[targetField] = reasons
	s.FieldBranches[targetField] = []string{targetField}
	s.Rules = append(s.Rules, Rule{Type: "eitherOf", Field: targetField, Group: targetField})
}

func convertDisablesRule(sr specRuleDef, s *Schema) {
	source := sr.Source
	if source == "" {
		source = sr.Field
	}
	targets := sr.Targets
	if len(targets) == 0 {
		targets = sr.Fields
	}
	if len(targets) == 0 && source != "" {
		targets = []string{source}
	}
	for _, target := range targets {
		reason := sr.Reason
		if reason == "" && source != "" {
			reason = "overridden by " + source
		}
		r := Rule{Type: "disables", Field: target, Reason: reason}
		if sr.When != nil {
			if e, err := rawMsgToExpr(sr.When); err == nil && e != nil {
				r.DisabledWhen = e
			}
		} else if source != "" {
			r.DisabledWhen = &Expr{Op: "present", Field: source}
		}
		s.Rules = append(s.Rules, r)
	}
}

func convertRequiresRule(sr specRuleDef, s *Schema) {
	deps := append([]string{}, sr.Requires...)
	if sr.Dependency != "" {
		deps = append(deps, sr.Dependency)
	}
	if sr.DependsOn != "" {
		deps = append(deps, sr.DependsOn)
	}
	for _, d := range sr.Dependencies {
		if dep, ok := d.(string); ok {
			deps = append(deps, dep)
			continue
		}
		depMap, ok := d.(map[string]any)
		if !ok {
			continue
		}
		checkRaw, ok := depMap["check"].(map[string]any)
		if !ok {
			continue
		}
		checkExpr, err := rawToExpr(checkRaw)
		if err != nil || checkExpr == nil {
			continue
		}
		if field, ok := depMap["field"].(string); ok {
			checkExpr.Field = field
		}
		reason := sr.Reason
		if reason == "" {
			reason = "check failed"
		}
		s.Rules = append(s.Rules, Rule{
			Type:         "disables",
			Field:        sr.Field,
			Reason:       reason,
			DisabledWhen: &Expr{Op: "not", Exprs: []Expr{*checkExpr}},
		})
	}
	reason := sr.Reason
	if reason == "" && len(deps) > 0 {
		reason = "requires " + deps[0]
	}
	r := Rule{Type: "requires", Field: sr.Field, Reason: reason, Requires: deps}
	if sr.When != nil {
		if e, err := rawMsgToExpr(sr.When); err == nil && e != nil {
			r.Expr = e
		}
	}
	s.Rules = append(s.Rules, r)
}

func rawToExpr(raw map[string]any) (*Expr, error) {
	op, _ := raw["op"].(string)
	if op == "" {
		return nil, fmt.Errorf("missing op")
	}
	e := &Expr{Op: op}
	if field, ok := raw["field"].(string); ok {
		e.Field = field
	}
	if cond, ok := raw["condition"].(string); ok {
		e.Condition = cond
	}
	if value, ok := raw["value"]; ok {
		e.Value = value
	}
	if values, ok := raw["values"].([]any); ok {
		e.Value = values
	}
	if pattern, ok := raw["pattern"].(string); ok {
		e.Pattern = pattern
		e.Value = pattern
	}
	if min, ok := raw["min"]; ok {
		e.Value = map[string]any{"min": min}
	}
	if max, ok := raw["max"]; ok {
		if m, ok := e.Value.(map[string]any); ok {
			m["max"] = max
		} else {
			e.Value = map[string]any{"max": max}
		}
	}
	for _, key := range []string{"expr", "check"} {
		if exprRaw, ok := raw[key].(map[string]any); ok {
			sub, err := rawToExpr(exprRaw)
			if err != nil {
				return nil, err
			}
			e.Exprs = append(e.Exprs, *sub)
		}
	}
	if exprsRaw, ok := raw["exprs"].([]any); ok {
		for _, item := range exprsRaw {
			if itemMap, ok := item.(map[string]any); ok {
				sub, err := rawToExpr(itemMap)
				if err != nil {
					return nil, err
				}
				e.Exprs = append(e.Exprs, *sub)
			}
		}
	}
	return e, nil
}

func rawMsgToExpr(msg *json.RawMessage) (*Expr, error) {
	if msg == nil || len(*msg) == 0 {
		return nil, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(*msg, &raw); err != nil {
		return nil, err
	}
	return rawToExpr(raw)
}

func exprFromAny(v any) *Expr {
	switch value := v.(type) {
	case string:
		raw := json.RawMessage(value)
		e, _ := rawMsgToExpr(&raw)
		return e
	case map[string]any:
		e, _ := rawToExpr(value)
		return e
	default:
		return nil
	}
}

func ensureBranchMaps(s *Schema) {
	if s.BranchExpressions == nil {
		s.BranchExpressions = make(map[string]*Expr)
	}
	if s.BranchReasons == nil {
		s.BranchReasons = make(map[string][]string)
	}
	if s.BranchSubConditions == nil {
		s.BranchSubConditions = make(map[string][]*Expr)
	}
	if s.BranchSubReasons == nil {
		s.BranchSubReasons = make(map[string][]string)
	}
	if s.FieldBranches == nil {
		s.FieldBranches = make(map[string][]string)
	}
	if s.BranchRuleTypes == nil {
		s.BranchRuleTypes = make(map[string]string)
	}
}

func orderedBranchNames(branches map[string]any, order []string) []string {
	if len(order) > 0 {
		return order
	}
	names := make([]string, 0, len(branches))
	for name := range branches {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func captureRuleBranchesOrder(data []byte) map[int][]string {
	result := make(map[int][]string)
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := walkToArrayKey(dec, "rules"); err != nil {
		return result
	}
	idx := 0
	for dec.More() {
		if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
			idx++
			continue
		}
		var branchKeys []string
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return result
			}
			key, _ := keyTok.(string)
			if key == "branches" {
				branchKeys, _ = readKeysFromObject(dec)
			} else if err := skipJSONValue(dec); err != nil {
				return result
			}
		}
		_, _ = dec.Token()
		if len(branchKeys) > 0 {
			result[idx] = branchKeys
		}
		idx++
	}
	return result
}

func walkToArrayKey(dec *json.Decoder, key string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return fmt.Errorf("expected top-level object")
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return err
		}
		ks, _ := k.(string)
		if ks == key {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			if tok != json.Delim('[') {
				return fmt.Errorf("expected array at key %q", key)
			}
			return nil
		}
		if err := skipJSONValue(dec); err != nil {
			return err
		}
	}
	_, _ = dec.Token()
	return fmt.Errorf("key %q not found", key)
}

func readKeysFromObject(dec *json.Decoder) ([]string, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if tok != json.Delim('{') {
		return nil, fmt.Errorf("expected object, got %v", tok)
	}
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, err
		}
		ks, _ := k.(string)
		keys = append(keys, ks)
		if err := skipJSONValue(dec); err != nil {
			return nil, err
		}
	}
	_, _ = dec.Token()
	return keys, nil
}

func skipJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		for dec.More() {
			if _, err := dec.Token(); err != nil {
				return err
			}
			if err := skipJSONValue(dec); err != nil {
				return err
			}
		}
		_, _ = dec.Token()
	case json.Delim('['):
		for dec.More() {
			if err := skipJSONValue(dec); err != nil {
				return err
			}
		}
		_, _ = dec.Token()
	}
	return nil
}

func goFieldName(name string) string {
	if name == "" {
		return ""
	}
	runes := []rune(name)
	runes[0] = unicode.ToUpper(runes[0])
	out := make([]rune, 0, len(runes))
	upperNext := false
	for i, r := range runes {
		if i == 0 {
			out = append(out, runes[0])
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upperNext = true
			continue
		}
		if upperNext {
			r = unicode.ToUpper(r)
		}
		upperNext = false
		out = append(out, r)
	}
	return string(out)
}
