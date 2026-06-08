package codegen

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umpire-tools/umpire-gen/internal/schema"
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

func convertFixtureSchema(fs *FixtureSchema) (*schema.Schema, error) {
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
	for _, fr := range fs.Rules {
		convertRule(fr, s)
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

func convertRule(fr FixtureRuleDef, s *schema.Schema) {
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
							// Branches are field names — collect them
							for _, fieldName := range bv {
								if fn, ok := fieldName.(string); ok {
									branchFields = append(branchFields, fn)
								}
							}
							// Use first branch field as targetField
							if targetField == "" && len(branchFields) > 0 {
								targetField = branchFields[0]
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

		// Create a marker rule for the oneOf/eitherOf group
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

		// Handle branches — they can be either []FixtureRuleDef or []string
		if fr.Branches != nil {
			switch b := fr.Branches.(type) {
			case map[string]any:
				// Either []FixtureRuleDef (eitherOf with rules) or []string (oneOf field list)
				for branchName, branchVal := range b {
					switch bv := branchVal.(type) {
					case []any:
						if len(bv) == 0 {
							continue
						}
						// Check if first element is a string (field names) or a map (rules)
						if _, ok := bv[0].(string); ok {
							// Branches are field names — generate enabledWhen rules for each
							for _, fieldName := range bv {
								if fn, ok := fieldName.(string); ok {
									s.Rules = append(s.Rules, schema.Rule{
										Type:   "enabledWhen",
										Field:  targetField,
										Reason: fmt.Sprintf("conflicts with %s strategy", branchName),
										Expr: &schema.Expr{
											Op:    "present",
											Field: fn,
										},
									})
								}
							}
						} else if _, ok := bv[0].(map[string]any); ok {
							// Branches are rules (eitherOf)
							for _, brraw := range bv {
								if bmap, ok := brraw.(map[string]any); ok {
									data, _ := json.Marshal(bmap)
									var branchRule FixtureRuleDef
									if err := json.Unmarshal(data, &branchRule); err == nil {
										convertRule(branchRule, s)
									}
								}
							}
						}
					}
				}
			}
		}

	case "anyOf":
		// Flatten anyOf rules into individual enabledWhen rules
		for _, r := range fr.Rules {
			convertRule(r, s)
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
			r := schema.Rule{
				Type:     "requires",
				Field:    fr.Field,
				Reason:   fr.Reason,
				Requires: deps,
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
			if fr.Pattern != "" {
				e.Value = fr.Pattern
				e.Pattern = fr.Pattern
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
	if err := os.WriteFile(filepath.Join(pkgDir, "gen_umpire.go"), []byte(source), 0644); err != nil {
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

	// Convert to internal schema
	internalSchema, err := convertFixtureSchema(&fixture.Schema)
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
	for i, rule := range internalSchema.Rules {
		t.Logf("[debug] Rule[%d]: Type=%q, Group=%q, Branches=%v, Field=%q", i, rule.Type, rule.Group, rule.Branches, rule.Field)
	}

	// Sanitize the schema name to be valid Go identifier
	goName := sanitizeName(id)

	gen := NewGenerator(goName, "pkg", goName+"Fields", goName+"Conditions", inferred)
	gen.WithFields(internalSchema.Fields)
	gen.WithRules(internalSchema.Rules)

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
			internalSchema, err := convertFixtureSchema(&fc.Schema)
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
