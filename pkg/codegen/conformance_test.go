package codegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/umpire-tools/umpire-gen/pkg/schema"
)

// ── Fixture schema types (umpire-spec format) ──────────────────────────

// FixtureSchema is the schema as written in umpire-spec test fixtures.
type FixtureSchema struct {
	Version    int                         `json:"version"`
	Fields     map[string]FixtureFieldDef  `json:"fields"`
	Conditions map[string]FixtureCondDef   `json:"conditions"`
	Rules      []FixtureRuleDef            `json:"rules"`
	Excluded   []FixtureExcluded           `json:"excluded,omitempty"`
}

// FixtureFieldDef mirrors a field definition in fixture schemas.
type FixtureFieldDef struct {
	Required bool   `json:"required"`
	IsEmpty  any    `json:"isEmpty,omitempty"`
	TypeHint string `json:"type,omitempty"`
	Default  any    `json:"default,omitempty"`
}

// FixtureCondDef mirrors a condition definition.
type FixtureCondDef struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// FixtureRuleDef mirrors a rule definition in fixture schemas.
// Branches can be either []FixtureRuleDef (for eitherOf branches with rules)
// or []string (for oneOf branches that list field names).
type FixtureRuleDef struct {
	Type         string        `json:"type"`
	Field        string        `json:"field,omitempty"`
	Fields       []string      `json:"fields,omitempty"`
	When         *json.RawMessage `json:"when,omitempty"`
	Check        *json.RawMessage `json:"check,omitempty"`
	Op           string        `json:"op,omitempty"`
	Pattern      string        `json:"pattern,omitempty"`
	Reason       string        `json:"reason,omitempty"`
	Requires     []string      `json:"requires,omitempty"`
	Dependency   string        `json:"dependency,omitempty"`
	Dependencies []any          `json:"dependencies,omitempty"`
	DependsOn    string        `json:"dependsOn,omitempty"`
	Source       string        `json:"source,omitempty"`
	Targets      []string      `json:"targets,omitempty"`
	Excluded     bool          `json:"excluded,omitempty"`
	Branches     any           `json:"branches,omitempty"` // map[string]any — either []FixtureRuleDef or []string
	Group        string        `json:"group,omitempty"`
	Rules        []FixtureRuleDef `json:"rules,omitempty"` // anyOf
	Min          *float64      `json:"min,omitempty"`
	Max          *float64      `json:"max,omitempty"`
	Value        *float64      `json:"value,omitempty"`
}

// FixtureExcluded marks an excluded rule.
type FixtureExcluded struct {
	Type        string `json:"type"`
	Field       string `json:"field,omitempty"`
	Description string `json:"description,omitempty"`
}

// ── Fixture case / index types ─────────────────────────────────────────

// PositiveFixture is a full positive test fixture.
type PositiveFixture struct {
	FixtureVersion int             `json:"fixtureVersion"`
	ID             string          `json:"id"`
	Description    string          `json:"description"`
	Schema         FixtureSchema   `json:"schema"`
	Cases          []FixtureCase   `json:"cases"`
}

// FixtureCase is a single test case within a positive fixture.
type FixtureCase struct {
	ID                   string            `json:"id"`
	Values               map[string]any    `json:"values"`
	Prev                 map[string]any    `json:"prev,omitempty"`
	Conditions           map[string]any    `json:"conditions"`
	ExpectedAvailability map[string]ExpAvail `json:"expectedAvailability"`
}

// ExpAvail is the expected availability for a field in a test case.
type ExpAvail struct {
	Enabled   bool     `json:"enabled"`
	Required  bool     `json:"required"`
	Satisfied bool     `json:"satisfied"`
	Fair      bool     `json:"fair"`
	Reason    *string  `json:"reason"`
	Reasons   []string `json:"reasons"`
	Valid     *bool    `json:"valid,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// GeneratedAvail mirrors the JSON-marshalled GeneratedAvail struct.
type GeneratedAvail struct {
	Enabled   bool     `json:"enabled"`
	Required  bool     `json:"required"`
	Satisfied bool     `json:"satisfied"`
	Fair      bool     `json:"fair"`
	Reason    *string  `json:"reason"`
	Reasons   []string `json:"reasons"`
	Valid     *bool    `json:"valid"`
	Error     string   `json:"error"`
	// ActiveBranch captures the oneOf active branch value (if present).
	ActiveBranch map[string]any `json:"-"`
}

// ConformanceIndex is the index.json structure.
type ConformanceIndex struct {
	FixtureVersion int          `json:"fixtureVersion"`
	Fixtures       []IndexEntry `json:"fixtures"`
	Failures       []IndexEntry `json:"failures"`
}

// IndexEntry is an entry in the conformance index.
type IndexEntry struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

// ── Failure fixture types ──────────────────────────────────────────────

// FailureFixture is a full failure test fixture.
type FailureFixture struct {
	FixtureVersion int          `json:"fixtureVersion"`
	ID             string       `json:"id"`
	Description    string       `json:"description"`
	Failures       []FailCase   `json:"failures"`
}

// FailCase is a single failure test case.
type FailCase struct {
	ID            string         `json:"id"`
	Phase         string         `json:"phase"` // "validate" or "evaluate"
	Schema        FixtureSchema  `json:"schema"`
	Values        map[string]any `json:"values,omitempty"`
	Conditions    map[string]any `json:"conditions,omitempty"`
	ErrorIncludes string         `json:"errorIncludes"`
}

// ── Expression conversion ──────────────────────────────────────────────

// walkToObjectKey walks a json.Decoder into the value at the given key, which
// must be a JSON object. The decoder is left ready to read the object's first
// key. If the value is a JSON array, the decoder is left at the start of the
// array.
func walkToObjectKey(dec *json.Decoder, key string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return fmt.Errorf("expected object, got %v", tok)
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return err
		}
		ks, _ := k.(string)
		if ks == key {
			return nil
		}
		if err := skipJSONValue(dec); err != nil {
			return err
		}
	}
	_, _ = dec.Token()
	return fmt.Errorf("key %q not found", key)
}

// readKeysFromObject reads the keys of the next object literal from the
// decoder, then leaves the decoder positioned just before the matching
// closing brace (i.e. ready to skip the value or stop).
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

// skipJSONValue skips a single JSON value at the current decoder position.
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

func rawToExpr(raw map[string]any) (*schema.Expr, error) {
	op, _ := raw["op"].(string)
	if op == "" {
		return nil, fmt.Errorf("missing op")
	}

	e := &schema.Expr{Op: op}

	if f, ok := raw["field"].(string); ok {
		e.Field = f
	}
	if c, ok := raw["condition"].(string); ok {
		e.Condition = c
	}
	if v, ok := raw["value"]; ok {
		e.Value = v
	}
	if vals, ok := raw["values"].([]any); ok {
		e.Value = vals
	}
	if pat, ok := raw["pattern"].(string); ok {
		e.Value = pat
		e.Pattern = pat
	}
	if minV, ok := raw["min"]; ok {
		e.Value = map[string]any{"min": minV}
	}
	if maxV, ok := raw["max"]; ok {
		if m, ok := e.Value.(map[string]any); ok {
			m["max"] = maxV
		} else {
			e.Value = map[string]any{"max": maxV}
		}
	}

	if exprsRaw, ok := raw["exprs"].([]any); ok {
		for _, er := range exprsRaw {
			if erMap, ok := er.(map[string]any); ok {
				sub, err := rawToExpr(erMap)
				if err != nil {
					return nil, err
				}
				e.Exprs = append(e.Exprs, *sub)
			}
		}
	}

	// Handle singular "expr" (used by "not" and some other ops)
	if exprRaw, ok := raw["expr"].(map[string]any); ok {
		sub, err := rawToExpr(exprRaw)
		if err != nil {
			return nil, err
		}
		e.Exprs = append(e.Exprs, *sub)
	}

	// Handle the "check" field (nested check expression)
	if checkRaw, ok := raw["check"].(map[string]any); ok {
		sub, err := rawToExpr(checkRaw)
		if err != nil {
			return nil, err
		}
		e.Exprs = append(e.Exprs, *sub)
	}

	return e, nil
}

func rawMsgToExpr(msg *json.RawMessage) (*schema.Expr, error) {
	if msg == nil || len(*msg) == 0 {
		return nil, nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(*msg), &raw); err != nil {
		return nil, fmt.Errorf("unmarshal raw json: %w", err)
	}
	return rawToExpr(raw)
}

// ── Schema conversion: fixture → internal ──────────────────────────────

// captureRuleBranchesOrder scans a fixture's rules array and returns, for each
// rule index, the key order of its "branches" object as it appears in the
// source JSON document. The result is used by convertRule to iterate branches
// deterministically (the standard encoding/json package randomizes map
// iteration order).
func captureRuleBranchesOrder(data []byte) map[int][]string {
	result := make(map[int][]string)
	dec := json.NewDecoder(bytes.NewReader(data))
	// Walk into the top-level "schema" object, then its "rules" array.
	if err := walkToObjectKey(dec, "schema"); err != nil {
		return result
	}
	if err := walkToArrayKey(dec, "rules"); err != nil {
		return result
	}
	// Read each rule object in order and record the branches object key order.
	idx := 0
	for dec.More() {
		// Expect each rule to be a JSON object.
		openTok, err := dec.Token()
		if err != nil {
			return result
		}
		if openTok != json.Delim('{') {
			idx++
			continue
		}
		// We are now inside a rule object. Iterate its keys/values.
		var branchKeys []string
		for dec.More() {
			kTok, err := dec.Token()
			if err != nil {
				return result
			}
			ks, _ := kTok.(string)
			if ks == "branches" {
				// The next value is the branches object. Read its key order.
				branchKeys, _ = readKeysFromObject(dec)
				continue
			}
			// Skip the value.
			if err := skipJSONValue(dec); err != nil {
				return result
			}
		}
		_, _ = dec.Token() // consume '}' of rule object
		if len(branchKeys) > 0 {
			result[idx] = branchKeys
		}
		idx++
	}
	return result
}

// walkToArrayKey positions the decoder at the start of the array at the given
// top-level key. The decoder is left ready to read the array's first element.
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

func convertFixtureSchema(fs *FixtureSchema, rawRulesOrder map[int][]string) (*schema.Schema, error) {
	s := &schema.Schema{
		Fields:     make([]schema.FieldDef, 0, len(fs.Fields)),
		Conditions: make([]schema.ConditionDef, 0, len(fs.Conditions)),
		Rules:      make([]schema.Rule, 0),
	}

// Convert fields (map → array)
for name, fd := range fs.Fields {
	def := schema.FieldDef{Name: name}
	def.Required = fd.Required

	// Preserve isEmpty as type hint: "string", "number", "boolean", "array", "object"
	if v, ok := fd.IsEmpty.(string); ok {
		def.IsEmpty = v
	}

	if fd.TypeHint != "" {
		def.TypeHint = fd.TypeHint
	}

	s.Fields = append(s.Fields, def)
}

	// Convert conditions (map → array)
	for name, cd := range fs.Conditions {
		s.Conditions = append(s.Conditions, schema.ConditionDef{
			Name: name,
			Type: cd.Type,
		})
	}

	// Convert rules
	for i, fr := range fs.Rules {
		order := rawRulesOrder[i]
		convertRule(fr, s, order)
	}

	// Convert excluded rules
	for _, ex := range fs.Excluded {
		if ex.Field != "" {
			s.Rules = append(s.Rules, schema.Rule{
				Type:        "excluded",
				Field:       ex.Field,
				Excluded:    true,
				Description: ex.Description,
			})
		}
	}

	return s, nil
}

func convertRule(fr FixtureRuleDef, s *schema.Schema, branchesOrder []string) {
	switch fr.Type {
	case "oneOf", "eitherOf":
		// Determine the target field and extract branch field names
		targetField := fr.Field
		var branchFields []string
		if targetField == "" && fr.Branches != nil {
			if branches, ok := fr.Branches.(map[string]any); ok {
				for _, branchVal := range branches {
					switch bv := branchVal.(type) {
					case []any:
						if len(bv) == 0 {
							continue
						}
						// Check if first element is a string (field names) or a map (rules)
						if _, ok := bv[0].(string); ok {
							// Use first branch field as targetField
							if targetField == "" {
								if fn, ok := bv[0].(string); ok {
									targetField = fn
								}
							}
						} else if _, ok := bv[0].(map[string]any); ok {
							// Branches are rules (eitherOf) — extract field from first rule
							if firstRule, ok := bv[0].(map[string]any); ok {
								if f, ok := firstRule["field"].(string); ok && f != "" {
									targetField = f
								}
							}
						}
					}
				}
			}
		}

	// Handle branches — they can be either []FixtureRuleDef or []string
		if fr.Branches != nil {
			switch b := fr.Branches.(type) {
			case map[string]any:
				// Either []FixtureRuleDef (eitherOf with rules) or []string (oneOf field list)
				isRuleBased := false
				for _, branchVal := range b {
					if branchRules, ok := branchVal.([]any); ok && len(branchRules) > 0 {
						if _, ok := branchRules[0].(map[string]any); ok {
							isRuleBased = true
							break
						}
					}
				}
				if isRuleBased {
					// Collect all branch names for the marker rule and build branch expressions
					// For eitherOf: do NOT create a combined enabledWhen rule - let RuleCompiler use BranchExpressions
					if s.BranchExpressions == nil {
						s.BranchExpressions = make(map[string]*schema.Expr)
					}
					if s.BranchReasons == nil {
						s.BranchReasons = make(map[string][]string)
					}
					if s.BranchSubConditions == nil {
						s.BranchSubConditions = make(map[string][]*schema.Expr)
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
					var fieldBranches []string
					// Iterate branches in the original JSON order (captured by the
					// caller via branchesOrder). Fall back to sorted names if
					// branchesOrder is not provided.
					branchNames := branchesOrder
					if len(branchNames) == 0 {
						for k := range b {
							branchNames = append(branchNames, k)
						}
						sort.Strings(branchNames)
					}
					for _, branchName := range branchNames {
						branchVal, ok := b[branchName]
						if !ok {
							continue
						}
						if branchRules, ok := branchVal.([]any); ok && len(branchRules) > 0 {
							if _, ok := branchRules[0].(map[string]any); !ok {
								continue
							}
							fieldBranches = append(fieldBranches, branchName)
							var branchExprs []schema.Expr
							var subExprs []*schema.Expr
							var subReasons []string
							branchType := ""
							for _, rraw := range branchRules {
								if rmap, ok := rraw.(map[string]any); ok {
									if t, ok := rmap["type"].(string); ok && branchType == "" {
										branchType = t
									}
									if whenRaw, ok := rmap["when"]; ok {
										var e *schema.Expr
										var err error
										switch wv := whenRaw.(type) {
										case string:
											rawMsg := json.RawMessage(wv)
											e, err = rawMsgToExpr(&rawMsg)
										case map[string]any:
											e, err = rawToExpr(wv)
										}
										if err != nil {
											continue
										}
										if e != nil {
											branchExprs = append(branchExprs, *e)
											subExprs = append(subExprs, e)
										}
									}
									if reason, ok := rmap["reason"].(string); ok && reason != "" {
										subReasons = append(subReasons, reason)
									}
								}
							}
							if len(branchExprs) == 0 {
								continue
							}
							var branchExpr schema.Expr
							if len(branchExprs) == 1 {
								branchExpr = branchExprs[0]
							} else {
								branchExpr = schema.Expr{Op: "and", Exprs: branchExprs}
							}
							// Store the AND-combined branch expression
							s.BranchExpressions[branchName] = &branchExpr
							// Store all reasons for this branch (concatenated)
							s.BranchReasons[branchName] = subReasons
							// Store sub-conditions and their reasons in order
							s.BranchSubConditions[branchName] = subExprs
							s.BranchSubReasons[branchName] = subReasons
							// Track the rule type that produced this branch's sub-conditions.
							if branchType != "" {
								s.BranchRuleTypes[branchName] = branchType
							}
							// Track branch order
							s.BranchOrder = append(s.BranchOrder, branchName)
						}
					}
					// Track which branches belong to which field
					if targetField != "" {
						s.FieldBranches[targetField] = fieldBranches
					}
				} else {
					// Non-rule-based oneOf — iterate over branches and collect field names per branch.
					// The CheckGenerator handles oneOf active-branch disabling via group.Active
					// comparison; do NOT also emit a disables rule per branch (they would conflict).
					// Track the original branch key alongside the field name so the "conflicts with
					// X strategy" reason text uses the original branch key.
					if s.BranchKeys == nil {
						s.BranchKeys = make(map[string]string)
					}
					// Use original JSON key order (from branchesOrder), falling
					// back to sorted names if the caller didn't provide it.
					branchKeys := branchesOrder
					if len(branchKeys) == 0 {
						for k := range b {
							branchKeys = append(branchKeys, k)
						}
						sort.Strings(branchKeys)
					}
					for _, branchKey := range branchKeys {
						branchVal, ok := b[branchKey]
						if !ok {
							continue
						}
						if branchFieldsList, ok := branchVal.([]any); ok {
							for _, fieldName := range branchFieldsList {
								if fn, ok := fieldName.(string); ok {
									branchFields = append(branchFields, fn)
									s.BranchKeys[GoFieldName(fn)] = branchKey
								}
							}
						}
					}
				}
			}
		}

		// Create a marker rule for the oneOf/eitherOf group (after branch fields are fully collected)
		groupName := fr.Group
		if groupName == "" {
			groupName = targetField
		}
		groupBranchName := groupName + "Branch"
		if targetField != "" {
			s.Rules = append(s.Rules, schema.Rule{
				Type:     fr.Type,
				Field:    targetField,
				Fields:   []string{groupBranchName},
				Group:    groupName,
				Branches: branchFields,
			})
		}

	case "anyOf":
		// anyOf: target field is enabled if ANY of the inner rules' conditions are met.
		// The reasons from all inner rules are collected.
		// The target field is determined from the inner rules' field (they should all target the same field).
		targetField := fr.Field
		if targetField == "" {
			// Find the field from the inner rules
			for _, r := range fr.Rules {
				if r.Field != "" {
					targetField = r.Field
					break
				}
			}
		}
		if targetField != "" {
			if s.BranchExpressions == nil {
				s.BranchExpressions = make(map[string]*schema.Expr)
			}
			if s.BranchReasons == nil {
				s.BranchReasons = make(map[string][]string)
			}
			if s.BranchSubConditions == nil {
				s.BranchSubConditions = make(map[string][]*schema.Expr)
			}
			if s.BranchSubReasons == nil {
				s.BranchSubReasons = make(map[string][]string)
			}
			if s.FieldBranches == nil {
				s.FieldBranches = make(map[string][]string)
			}
			branchName := targetField
			var subExprs []*schema.Expr
			var subReasons []string
			var combinedExprs []schema.Expr
			for _, r := range fr.Rules {
				if r.When != nil {
					e, err := rawMsgToExpr(r.When)
					if err == nil && e != nil {
						subExprs = append(subExprs, e)
						combinedExprs = append(combinedExprs, *e)
					}
					if r.Reason != "" {
						subReasons = append(subReasons, r.Reason)
					}
				}
			}
			if len(combinedExprs) > 0 {
				var branchExpr schema.Expr
				if len(combinedExprs) == 1 {
					branchExpr = combinedExprs[0]
				} else {
					branchExpr = schema.Expr{Op: "or", Exprs: combinedExprs}
				}
				s.BranchExpressions[branchName] = &branchExpr
				s.BranchReasons[branchName] = subReasons
				s.BranchSubConditions[branchName] = subExprs
				s.BranchSubReasons[branchName] = subReasons
				s.FieldBranches[targetField] = []string{branchName}
				// Add a marker rule for anyOf
				s.Rules = append(s.Rules, schema.Rule{
					Type:   "eitherOf",
					Field:  targetField,
					Group:  targetField,
					Reason: "",
				})
			}
		}

		case "disables":
			source := fr.Source
			if source == "" {
				source = fr.Field
			}
			targets := fr.Targets
			if len(targets) == 0 {
				targets = fr.Fields
			}
			if len(targets) == 0 && source != "" {
				targets = []string{source}
			}
			for _, target := range targets {
				reason := fr.Reason
				if reason == "" && source != "" {
					reason = "overridden by " + source
				}
				r := schema.Rule{
					Type:   "disables",
					Field:  target,
					Reason: reason,
				}
				if fr.When != nil {
					e, err := rawMsgToExpr(fr.When)
					if err == nil && e != nil {
						r.DisabledWhen = e
					}
				} else if source != "" {
					r.DisabledWhen = &schema.Expr{
						Op:    "present",
						Field: source,
					}
				}
				s.Rules = append(s.Rules, r)
			}

		case "requires":
			dep := fr.Dependency
			if dep == "" {
				dep = fr.DependsOn
			}
			var deps []string
			if dep != "" {
				deps = []string{dep}
			}
			// Process Dependencies (can be strings or check objects)
			for _, d := range fr.Dependencies {
				switch v := d.(type) {
				case string:
					deps = append(deps, v)
				case map[string]any:
					// Check expression dependency - adds a disables rule that triggers when check fails
					if checkRaw, ok := v["check"]; ok {
						if checkRawMap, ok := checkRaw.(map[string]any); ok {
							checkExpr, err := rawToExpr(checkRawMap)
							if err == nil && checkExpr != nil {
								// The check expression needs the field from the parent
								if field, ok := v["field"].(string); ok {
									checkExpr.Field = field
								}
								// Add a disables rule that triggers when the check fails
								disabledWhen := &schema.Expr{
									Op:    "not",
									Exprs: []schema.Expr{*checkExpr},
								}
								reason := fr.Reason
								if reason == "" {
									reason = "check failed"
								}
								s.Rules = append(s.Rules, schema.Rule{
									Type:         "disables",
									Field:        fr.Field,
									Reason:       reason,
									DisabledWhen: disabledWhen,
								})
							}
						}
					}
				}
			}
			if len(deps) == 0 && len(fr.Requires) > 0 {
				deps = fr.Requires
			}
			// Set default reason for requires
			reason := fr.Reason
			if reason == "" && len(deps) > 0 {
				reason = fmt.Sprintf("requires %s", deps[0])
			}
			r := schema.Rule{
				Type:     "requires",
				Field:    fr.Field,
				Reason:   reason,
				Requires: deps,
			}
			// If a "when" clause is present, treat it as an additional enabled expression
			// combined with the requires dependency check (i.e. enabled = whenExpr AND all deps).
			if fr.When != nil {
				if e, err := rawMsgToExpr(fr.When); err == nil && e != nil {
					r.Expr = e
				}
			}
			s.Rules = append(s.Rules, r)

	default:
		r := schema.Rule{
			Type:      fr.Type,
			Field:     fr.Field,
			Reason:    fr.Reason,
			Requires:  fr.Requires,
			Excluded:  fr.Excluded,
			DependsOn: fr.DependsOn,
		}

		if fr.When != nil {
			e, err := rawMsgToExpr(fr.When)
			if err == nil && e != nil {
				switch fr.Type {
				case "enabledWhen":
					r.Expr = e
				case "disables":
					r.DisabledWhen = e
				case "fairWhen":
					r.FairWhen = e
				}
			}
		}

		if fr.Check != nil {
			e, err := rawMsgToExpr(fr.Check)
			if err == nil && e != nil {
				r.Check = e
			}
		}

		// Handle check rules with op/pattern at top level (e.g., {"type": "check", "field": "X", "op": "matches", "pattern": "..."})
		if fr.Type == "check" && fr.Op != "" {
			e := &schema.Expr{Op: fr.Op, Field: fr.Field}
			// Set default reason if not provided
			if fr.Reason != "" {
				r.Reason = fr.Reason
			} else {
				r.Reason = defaultCheckReason(fr.Op, fr.Value, fr.Min, fr.Max)
			}
			switch fr.Op {
			case "matches":
				if fr.Pattern != "" {
					e.Value = fr.Pattern
					e.Pattern = fr.Pattern
				}
			case "minLength", "maxLength", "min", "max":
				if fr.Value != nil {
					e.Value = *fr.Value
				}
			case "range":
				if fr.Min != nil && fr.Max != nil {
					// Store range as a special object so compileCheck can extract
					e.Value = map[string]float64{"min": *fr.Min, "max": *fr.Max}
				}
			}
			r.Check = e
		}

		s.Rules = append(s.Rules, r)
	}
}

// collectConditionRefs collects all condition names referenced in an expression tree.
func collectConditionRefs(e *schema.Expr, refs map[string]bool) {
	if e == nil {
		return
	}
	if e.Condition != "" {
		refs[e.Condition] = true
	}
	for _, child := range e.Exprs {
		collectConditionRefs(&child, refs)
	}
}

// defaultCheckReason returns a default reason for a check operator.
func defaultCheckReason(op string, value *float64, minVal *float64, maxVal *float64) string {
	switch op {
	case "email":
		return "Must be a valid email address"
	case "url":
		return "Must be a valid URL"
	case "minLength":
		if value != nil {
			return fmt.Sprintf("Must be at least %g characters", *value)
		}
		return "Must have at least the minimum number of items"
	case "maxLength":
		if value != nil {
			return fmt.Sprintf("Must be %g characters or fewer", *value)
		}
		return "Has too many items"
	case "matches":
		return "Must match the required format"
	case "integer":
		return "Must be a whole number"
	case "min":
		if value != nil {
			return fmt.Sprintf("Must be at least %g", *value)
		}
		return "Below the minimum"
	case "max":
		if value != nil {
			return fmt.Sprintf("Must be %g or less", *value)
		}
		return "Above the maximum"
	case "range":
		if minVal != nil && maxVal != nil {
			return fmt.Sprintf("Must be between %g and %g", *minVal, *maxVal)
		}
		return "Must be in the allowed range"
	default:
		return "Validation failed"
	}
}

// ── Run Check() via generated Go code ──────────────────────────────────

func runCheck(t *testing.T, schemaName string, s *schema.Schema, source string, values, conditions map[string]any) (map[string]GeneratedAvail, error) {
	t.Helper()

	// DEBUG: print generated source
	t.Logf("=== GENERATED SOURCE ===\n%s", source)

	tmpDir := t.TempDir()
	pkgDir := filepath.Join(tmpDir, "pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		return nil, fmt.Errorf("MkdirAll: %w", err)
	}

	// Write the generated Go code
	if err := os.WriteFile(filepath.Join(pkgDir, "gen_gen.go"), []byte(source), 0644); err != nil {
		return nil, fmt.Errorf("WriteFile: %w", err)
	}

	// Build main.go that unmarshals values from JSON and calls Check()
	// This handles pointer types correctly via JSON unmarshalling
	fieldsJSON := make(map[string]any)
	for _, f := range s.Fields {
		if v, ok := values[f.Name]; ok {
			fieldsJSON[f.Name] = v
		}
	}
	fieldsData, _ := json.Marshal(fieldsJSON)

	condsJSON := make(map[string]any)
	for _, c := range s.Conditions {
		if v, ok := conditions[c.Name]; ok {
			condsJSON[c.Name] = v
		}
	}
	condsData, _ := json.Marshal(condsJSON)

	// Validate that all conditions referenced in rules have runtime values
	referencedConditions := make(map[string]bool)
	for _, rule := range s.Rules {
		if rule.Expr != nil {
			collectConditionRefs(rule.Expr, referencedConditions)
		}
		if rule.DisabledWhen != nil {
			collectConditionRefs(rule.DisabledWhen, referencedConditions)
		}
		if rule.FairWhen != nil {
			collectConditionRefs(rule.FairWhen, referencedConditions)
		}
		if rule.Check != nil {
			collectConditionRefs(rule.Check, referencedConditions)
		}
	}
	for condName := range referencedConditions {
		if _, ok := conditions[condName]; !ok {
			return nil, fmt.Errorf("Missing runtime condition %q", condName)
		}
	}

 	mainGo := fmt.Sprintf(`package main

import (
	"encoding/json"
	"os"
	"pkg"
)

func main() {
	var f pkg.%sFields
	var c pkg.%sConditions
	_ = json.Unmarshal([]byte(%s), &f)
	_ = json.Unmarshal([]byte(%s), &c)
	avail := pkg.Check(f, c, pkg.%sFields{})
	enc := json.NewEncoder(os.Stdout)
	enc.Encode(avail)
}
`, schemaName, schemaName, q(string(fieldsData)), q(string(condsData)), schemaName)

	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainGo), 0644); err != nil {
		return nil, fmt.Errorf("WriteFile main: %w", err)
	}

	// Create go.mod with separate module for pkg
	goMod := "module main\n\ngo 1.22\n\nrequire pkg v0.0.0\nreplace pkg => ./pkg\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return nil, fmt.Errorf("WriteFile go.mod: %w", err)
	}

	// Also create a go.mod in the pkg directory
	pkgGoMod := "module pkg\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(pkgDir, "go.mod"), []byte(pkgGoMod), 0644); err != nil {
		return nil, fmt.Errorf("WriteFile pkg go.mod: %w", err)
	}

	// Run go run
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go run failed: %w\n%s", err, output)
	}

	// Parse JSON output
	output = []byte(strings.TrimSpace(string(output)))
	if len(output) == 0 {
		return nil, fmt.Errorf("empty output from go run")
	}

	// Use a map with string keys and json.RawMessage values to handle unknown fields
	var rawResult map[string]json.RawMessage
	if err := json.Unmarshal(output, &rawResult); err != nil {
		return nil, fmt.Errorf("parse JSON raw: %w\noutput: %s", err, output)
	}
	result := make(map[string]GeneratedAvail)
	for k, v := range rawResult {
		var ga GeneratedAvail
		if err := json.Unmarshal(v, &ga); err != nil {
			return nil, fmt.Errorf("parse %s: %w", k, err)
		}
		result[k] = ga
	}

	return result, nil
}

// ── Conformance test ───────────────────────────────────────────────────

func TestConformance(t *testing.T) {
	indexPath := filepath.Join("spec", "conformance", "index.json")
	if _, err := os.Stat(indexPath); err != nil {
		indexPath = filepath.Join("..", "..", "spec", "conformance", "index.json")
	}

	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}

	var index ConformanceIndex
	if err := json.Unmarshal(indexData, &index); err != nil {
		t.Fatalf("parse index.json: %v", err)
	}

	// Run positive fixtures
	for _, entry := range index.Fixtures {
		t.Run(entry.ID, func(t *testing.T) {
			fixturePath := filepath.Join("spec", "conformance", entry.Path)
			if _, err := os.Stat(fixturePath); err != nil {
				fixturePath = filepath.Join("..", "..", "spec", "conformance", entry.Path)
			}
			runPositiveFixture(t, fixturePath, entry.ID)
		})
	}

	// Run failure fixtures
	for _, entry := range index.Failures {
		t.Run("failures/"+entry.ID, func(t *testing.T) {
			fixturePath := filepath.Join("spec", "conformance", entry.Path)
			if _, err := os.Stat(fixturePath); err != nil {
				fixturePath = filepath.Join("..", "..", "spec", "conformance", entry.Path)
			}
			runFailureFixture(t, fixturePath, entry.ID)
		})
	}
}

func runPositiveFixture(t *testing.T, path, id string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var fixture PositiveFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	// Capture the original JSON order of each rule's branches. The standard
	// encoding/json package randomizes object key order, so we walk the
	// document a second time to record the canonical key order.
	rawRulesOrder := captureRuleBranchesOrder(data)

	// Convert to internal schema
	internalSchema, err := convertFixtureSchema(&fixture.Schema, rawRulesOrder)
	if err != nil {
		t.Fatalf("convert schema: %v", err)
	}

	// Validate
	if err := internalSchema.Validate(); err != nil {
		t.Fatalf("schema validation: %v", err)
	}

	// Generate Go code once (shared across all cases in the fixture)
	inferred, err := InferTypes(internalSchema)
	if err != nil {
		t.Fatalf("InferTypes: %v", err)
	}

	t.Logf("[debug] Inferred.Branches: %+v", inferred.Branches)
	t.Logf("[debug] Total rules: %d", len(internalSchema.Rules))
	for i, rule := range internalSchema.Rules {
		t.Logf("[debug] Rule[%d]: Type=%q, Group=%q, Branches=%v, Field=%q", i, rule.Type, rule.Group, rule.Branches, rule.Field)
	}

	// Sanitize the schema name to be valid Go identifier
	goName := sanitizeName(id)

	gen := NewGenerator(goName, "pkg", goName+"Fields", goName+"Conditions", inferred)
	gen.WithFields(internalSchema.Fields)
	gen.WithRules(internalSchema.Rules)
	gen.WithSchema(internalSchema)

	result, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if result.Source == "" {
		t.Fatal("generated source is empty")
	}

		// Run each case
	for _, tc := range fixture.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			avail, err := runCheck(t, goName, internalSchema, result.Source, tc.Values, tc.Conditions)
			if err != nil {
				t.Fatalf("runCheck: %v", err)
			}

			// Compare each field — convert JSON field name to Go field name
			for fieldName, expected := range tc.ExpectedAvailability {
				goFieldName := GoFieldName(fieldName)
				got, ok := avail[goFieldName]
				if !ok {
					t.Errorf("field %q (Go: %q) not in generated availability, keys: %v", fieldName, goFieldName, getKeys(avail))
					continue
				}

				if got.Enabled != expected.Enabled {
					t.Errorf("%s.Enabled: want %v, got %v", fieldName, expected.Enabled, got.Enabled)
				}
				if got.Required != expected.Required {
					t.Errorf("%s.Required: want %v, got %v", fieldName, expected.Required, got.Required)
				}
				if got.Satisfied != expected.Satisfied {
					t.Errorf("%s.Satisfied: want %v, got %v", fieldName, expected.Satisfied, got.Satisfied)
				}
				if got.Fair != expected.Fair {
					t.Errorf("%s.Fair: want %v, got %v", fieldName, expected.Fair, got.Fair)
				}
				if expected.Reason != nil {
					if got.Reason == nil || *got.Reason != *expected.Reason {
						t.Errorf("%s.Reason: want %q, got %v", fieldName, *expected.Reason, got.Reason)
					}
				}
				if len(expected.Reasons) > 0 {
					if !equalStrSlice(got.Reasons, expected.Reasons) {
						t.Errorf("%s.Reasons: want %v, got %v", fieldName, expected.Reasons, got.Reasons)
					}
				}
			}
		})
	}
}

func runFailureFixture(t *testing.T, path, id string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var fixture FailureFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	for _, fc := range fixture.Failures {
		t.Run(fc.ID, func(t *testing.T) {
			rawRulesOrder := captureRuleBranchesOrder(data)
			internalSchema, err := convertFixtureSchema(&fc.Schema, rawRulesOrder)
			if err != nil {
				t.Fatalf("convert schema: %v", err)
			}

			goName := sanitizeName(id)

			switch fc.Phase {
			case "validate":
				// Try InferTypes first (it validates internally)
				_, inferErr := InferTypes(internalSchema)
				if inferErr == nil {
					// InferTypes passed, try Generate
					inferred, _ := InferTypes(internalSchema)
					gen := NewGenerator(goName, "pkg", goName+"Fields", goName+"Conditions", inferred)
					gen.WithFields(internalSchema.Fields)
					gen.WithRules(internalSchema.Rules)
					_, genErr := gen.Generate()
					if genErr == nil {
						t.Errorf("expected generation to fail with error containing %q", fc.ErrorIncludes)
					} else if !strings.Contains(genErr.Error(), fc.ErrorIncludes) {
						t.Errorf("error %q does not contain %q", genErr.Error(), fc.ErrorIncludes)
					}
				} else if !strings.Contains(inferErr.Error(), fc.ErrorIncludes) {
					t.Errorf("InferTypes error %q does not contain %q", inferErr.Error(), fc.ErrorIncludes)
				}

			case "evaluate":
				// Try to generate and run Check — should error
				inferred, err := InferTypes(internalSchema)
				if err != nil {
					if !strings.Contains(err.Error(), fc.ErrorIncludes) {
						t.Errorf("InferTypes error %q does not contain %q", err.Error(), fc.ErrorIncludes)
					}
					return
				}

				gen := NewGenerator(goName, "pkg", goName+"Fields", goName+"Conditions", inferred)
				gen.WithFields(internalSchema.Fields)
				gen.WithRules(internalSchema.Rules)
				result, err := gen.Generate()
				if err != nil {
					if !strings.Contains(err.Error(), fc.ErrorIncludes) {
						t.Errorf("Generate error %q does not contain %q", err.Error(), fc.ErrorIncludes)
					}
					return
				}

				_, runErr := runCheck(t, goName, internalSchema, result.Source, fc.Values, fc.Conditions)
				if runErr == nil {
					t.Errorf("expected Check() to error with string containing %q", fc.ErrorIncludes)
				} else if !strings.Contains(runErr.Error(), fc.ErrorIncludes) {
					t.Errorf("Check error %q does not contain %q", runErr.Error(), fc.ErrorIncludes)
				}
			}
		})
	}
}

// sanitizeName converts a fixture ID to a valid Go identifier.
// Replaces hyphens and other non-identifier chars with underscores,
// and capitalizes the first letter.
func sanitizeName(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i == 0 {
			if r >= 'a' && r <= 'z' {
				b.WriteRune(r - 32)
			} else if r >= '0' && r <= '9' || r == '_' {
				b.WriteString("_")
				b.WriteRune(r)
			} else {
				b.WriteRune(r)
			}
		} else {
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
				b.WriteRune(r)
			} else {
				b.WriteRune('_')
			}
		}
	}
	return b.String()
}

// getKeys returns the keys of a map for debugging.
func getKeys(m map[string]GeneratedAvail) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// equalStrSlice checks if two string slices are equal.
func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
