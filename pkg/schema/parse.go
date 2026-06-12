package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"unicode"
)

// Parse reads a .umpire.json payload. It accepts both the public umpire-spec
// shape and the older internal array shape used by early generator tests.
func Parse(data []byte) (*Schema, error) {
	var internal Schema
	if err := json.Unmarshal(data, &internal); err == nil {
		if len(internal.Fields) > 0 {
			return &internal, nil
		}
	} else if looksLikeInternalSchema(data) {
		return nil, err
	}

	var spec specSchema
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, err
	}
	if len(spec.Fields) == 0 {
		return nil, fmt.Errorf("schema must have at least one field definition")
	}
	return convertSpecSchema(&spec, captureRuleBranchesOrder(data)), nil
}

func looksLikeInternalSchema(data []byte) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	for _, key := range []string{"fields", "conditions"} {
		value := bytes.TrimSpace(raw[key])
		if len(value) > 0 && value[0] == '[' {
			return true
		}
	}
	return false
}

type specSchema struct {
	Version    int                     `json:"version"`
	Fields     map[string]specFieldDef `json:"fields"`
	Conditions map[string]specCondDef  `json:"conditions"`
	Rules      []specRuleDef           `json:"rules"`
	Excluded   []specExcluded          `json:"excluded,omitempty"`
}

type specFieldDef struct {
	Required bool   `json:"required"`
	IsEmpty  any    `json:"isEmpty,omitempty"`
	TypeHint string `json:"type,omitempty"`
	Default  any    `json:"default,omitempty"`
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
		def := FieldDef{Name: name, Required: fd.Required, TypeHint: fd.TypeHint}
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
