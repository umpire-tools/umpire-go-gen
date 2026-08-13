package umpiregen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/umpire-tools/umpire-go-gen/pkg/codegen"
)

type profileConformanceIndex struct {
	Fixtures []profileConformanceIndexEntry `json:"fixtures"`
	Failures []profileConformanceIndexEntry `json:"failures"`
}

type profileConformanceIndexEntry struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type profileConformanceFixture struct {
	ID      string                          `json:"id"`
	Profile json.RawMessage                 `json:"profile"`
	Cases   []profileConformanceFixtureCase `json:"cases"`
}

type profileConformanceFixtureCase struct {
	ID                   string          `json:"id"`
	Values               json.RawMessage `json:"values"`
	Conditions           json.RawMessage `json:"conditions"`
	Prev                 json.RawMessage `json:"prev"`
	ExpectedStructure    profileExpectedStructure
	ExpectedAvailability json.RawMessage `json:"expectedAvailability"`
}

type profileExpectedStructure struct {
	Valid  bool                     `json:"valid"`
	Issues []profileStructuralTuple `json:"issues"`
}

type profileStructuralTuple struct {
	Source string `json:"source"`
	Code   string `json:"code"`
	Path   string `json:"path"`
}

type profileConformanceFailureFixture struct {
	ID       string                      `json:"id"`
	Failures []profileConformanceFailure `json:"failures"`
}

type profileConformanceFailure struct {
	ID                       string                   `json:"id"`
	Profile                  json.RawMessage          `json:"profile"`
	ExpectedDefinitionIssues []profileDefinitionTuple `json:"expectedDefinitionIssues"`
}

type profileDefinitionTuple struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

type profileConformanceResult struct {
	Structural   []profileStructuralTuple `json:"structural"`
	Availability json.RawMessage          `json:"availability"`
}

// TestProfileConformance executes every fixture indexed by the Profile v1
// conformance suite. It deliberately exercises the public GenerateProfile API:
// the generated package is compiled and run in a dependency-free temporary
// module rather than validating fixtures with implementation helpers.
func TestProfileConformance(t *testing.T) {
	root := profileConformanceRoot(t)
	indexData, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatalf("read profile conformance index: %v", err)
	}

	var index profileConformanceIndex
	if err := json.Unmarshal(indexData, &index); err != nil {
		t.Fatalf("parse profile conformance index: %v", err)
	}
	profileAssertConformanceIndex(t, index)

	for _, entry := range index.Fixtures {
		entry := entry
		t.Run(entry.ID, func(t *testing.T) {
			profileRunPositiveConformanceFixture(t, filepath.Join(root, filepath.FromSlash(entry.Path)), entry.ID)
		})
	}
	for _, entry := range index.Failures {
		entry := entry
		t.Run("failures/"+entry.ID, func(t *testing.T) {
			profileRunFailureConformanceFixture(t, filepath.Join(root, filepath.FromSlash(entry.Path)), entry.ID)
		})
	}
}

func profileConformanceRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "spec", "profiles", "conformance")
		if info, err := os.Stat(filepath.Join(candidate, "index.json")); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("find spec/profiles/conformance/index.json from %q", wd)
		}
	}
}

func profileAssertConformanceIndex(t *testing.T, index profileConformanceIndex) {
	t.Helper()
	if len(index.Fixtures) == 0 {
		t.Fatal("profile conformance index has no fixtures")
	}
	if len(index.Failures) == 0 {
		t.Fatal("profile conformance index has no failures")
	}

	ids := make(map[string]bool, len(index.Fixtures)+len(index.Failures))
	paths := make(map[string]bool, len(index.Fixtures)+len(index.Failures))
	for _, entry := range append(append([]profileConformanceIndexEntry(nil), index.Fixtures...), index.Failures...) {
		if entry.ID == "" || ids[entry.ID] {
			t.Fatalf("profile conformance index has empty or duplicate fixture id %q", entry.ID)
		}
		if entry.Path == "" || paths[entry.Path] {
			t.Fatalf("profile conformance index has empty or duplicate fixture path %q", entry.Path)
		}
		ids[entry.ID] = true
		paths[entry.Path] = true
	}
}

func profileRunPositiveConformanceFixture(t *testing.T, path, id string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture profileConformanceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if fixture.ID != id {
		t.Fatalf("fixture id = %q, want indexed id %q", fixture.ID, id)
	}
	if len(fixture.Profile) == 0 {
		t.Fatal("fixture has no profile")
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	caseIDs := make(map[string]bool, len(fixture.Cases))
	for _, tc := range fixture.Cases {
		if tc.ID == "" || caseIDs[tc.ID] {
			t.Fatalf("fixture has empty or duplicate case id %q", tc.ID)
		}
		caseIDs[tc.ID] = true
	}

	schemaName := profileConformanceName(id)
	source, issues, err := GenerateProfile(fixture.Profile, Config{PkgName: "profilefixture", SchemaName: schemaName})
	if err != nil {
		t.Fatalf("GenerateProfile: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("GenerateProfile returned definition issues for accepted fixture: %+v", issues)
	}

	results := profileRunGeneratedCases(t, source, schemaName, fixture.Cases)
	if len(results) != len(fixture.Cases) {
		t.Fatalf("generated runner returned %d results for %d cases", len(results), len(fixture.Cases))
	}
	for i, tc := range fixture.Cases {
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			profileAssertStructural(t, results[i].Structural, tc.ExpectedStructure)
			if tc.ExpectedStructure.Valid && len(tc.ExpectedAvailability) != 0 {
				profileAssertAvailability(t, results[i].Availability, tc.ExpectedAvailability)
			} else if len(results[i].Availability) != 0 {
				t.Fatal("generated runner decoded availability for a structurally invalid or availability-less case")
			}
		})
	}
}

func profileRunFailureConformanceFixture(t *testing.T, path, id string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failure fixture: %v", err)
	}
	var fixture profileConformanceFailureFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse failure fixture: %v", err)
	}
	if fixture.ID != id {
		t.Fatalf("failure fixture id = %q, want indexed id %q", fixture.ID, id)
	}
	if len(fixture.Failures) == 0 {
		t.Fatal("failure fixture has no failures")
	}
	failureIDs := make(map[string]bool, len(fixture.Failures))
	for _, failure := range fixture.Failures {
		if failure.ID == "" || failureIDs[failure.ID] {
			t.Fatalf("failure fixture has empty or duplicate failure id %q", failure.ID)
		}
		if len(failure.ExpectedDefinitionIssues) == 0 {
			t.Fatalf("failure %q has no expected definition issues", failure.ID)
		}
		failureIDs[failure.ID] = true
	}

	for _, failure := range fixture.Failures {
		failure := failure
		t.Run(failure.ID, func(t *testing.T) {
			// Definition issues are Profile compilation rejection. GenerateProfile
			// still runs so callers can observe any declared problems, even when
			// generation ultimately fails.
			source, issues, err := GenerateProfile(failure.Profile, Config{
				PkgName:    "profilefixture",
				SchemaName: profileConformanceName(failure.ID),
			})
			_ = source
			_ = err
			profileAssertDefinitionIssues(t, issues, failure.ExpectedDefinitionIssues)
		})
	}
}

// profileRunGeneratedCases compiles one generated package per fixture. Every
// case calls Validate<Schema>JSON, every structurally valid case also calls
// Decode<Schema>, and only valid cases with availability expectations call Check.
func profileRunGeneratedCases(t *testing.T, source, schemaName string, cases []profileConformanceFixtureCase) []profileConformanceResult {
	t.Helper()
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "generated")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("create generated package: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "generated.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write generated source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "go.mod"), []byte("module generated\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write generated go.mod: %v", err)
	}

	var requests strings.Builder
	for _, tc := range cases {
		if len(tc.Values) == 0 {
			t.Fatalf("case %q has no values", tc.ID)
		}
		conditions := tc.Conditions
		if len(conditions) == 0 {
			conditions = json.RawMessage("null")
		}
		previousValues := "nil"
		if len(tc.Prev) != 0 {
			previousValues = fmt.Sprintf("json.RawMessage(%q)", string(tc.Prev))
		}
		fmt.Fprintf(&requests, "{values: json.RawMessage(%q), previousValues: %s, previousValuesPresent: %t, conditions: json.RawMessage(%q), structurallyValid: %t, availability: %t},\n",
			string(tc.Values), previousValues, len(tc.Prev) != 0, string(conditions), tc.ExpectedStructure.Valid, tc.ExpectedStructure.Valid && len(tc.ExpectedAvailability) != 0)
	}

	mainSource := fmt.Sprintf(`package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strings"

	generated "generated"
)

type request struct {
	values json.RawMessage
	previousValues json.RawMessage
	previousValuesPresent bool
	conditions json.RawMessage
	structurallyValid bool
	availability bool
}

type tuple struct {
	Source string `+"`json:\"source\"`"+`
	Code string `+"`json:\"code\"`"+`
	Path string `+"`json:\"path\"`"+`
}

type result struct {
	Structural []tuple `+"`json:\"structural\"`"+`
	Availability json.RawMessage `+"`json:\"availability,omitempty\"`"+`
}

func clone[T any](value T) (T, error) {
	data, err := json.Marshal(value)
	if err != nil {
		var zero T
		return zero, err
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}

// assertDecodedFields walks the actual generated fields directly. The expected
// side is independently decoded with UseNumber, so this does not rely on the
// generated types' MarshalJSON behavior and includes nested objects and arrays.
func assertDecodedFields(actual any, raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var expected any
	if err := decoder.Decode(&expected); err != nil {
		return err
	}
	return compareSemantic(expected, reflect.ValueOf(actual), "$")
}

func compareSemantic(expected any, actual reflect.Value, path string) error {
	for actual.IsValid() && (actual.Kind() == reflect.Interface || actual.Kind() == reflect.Pointer) {
		if actual.IsNil() {
			return fmt.Errorf("decoded %%s is nil; expected %%v", path, expected)
		}
		actual = actual.Elem()
	}
	switch expected := expected.(type) {
	case map[string]any:
		if !actual.IsValid() || actual.Kind() != reflect.Struct {
			return fmt.Errorf("decoded %%s kind = %%v, want object", path, actual.Kind())
		}
		// Tagged unions use a sealed interface in a one-field wrapper so arrays and
		// nested objects retain ordinary encoding/json behavior. Compare the selected
		// branch rather than treating the private wrapper as a wire object.
		if actual.NumField() == 1 && actual.Type().Field(0).Name == "Value" && actual.Type().Field(0).Tag.Get("json") == "-" {
			return compareSemantic(expected, actual.Field(0), path)
		}
		fields := make(map[string]reflect.Value, actual.NumField())
		for i := 0; i < actual.NumField(); i++ {
			fieldInfo := actual.Type().Field(i)
			name := strings.Split(fieldInfo.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				fields[name] = actual.Field(i)
			}
		}
		for name, value := range expected {
			field, ok := fields[name]
			if !ok {
				return fmt.Errorf("decoded %%s is missing field %%q", path, name)
			}
			if err := compareSemantic(value, field, path+"/"+name); err != nil {
				return err
			}
		}
		for name, field := range fields {
			if _, ok := expected[name]; !ok && !field.IsZero() {
				return fmt.Errorf("decoded %%s/%%s is unexpectedly non-zero", path, name)
			}
		}
	case []any:
		if !actual.IsValid() || actual.Kind() != reflect.Slice || actual.IsNil() || actual.Len() != len(expected) {
			return fmt.Errorf("decoded %%s length/kind mismatch", path)
		}
		for i, value := range expected {
			if err := compareSemantic(value, actual.Index(i), fmt.Sprintf("%%s/%%d", path, i)); err != nil {
				return err
			}
		}
	case json.Number:
		parsed, _, err := big.ParseFloat(expected.String(), 10, 256, big.ToNearestEven)
		if err != nil {
			return fmt.Errorf("invalid expected number at %%s", path)
		}
		rat, _ := parsed.Rat(nil)
		switch actual.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if rat.Cmp(big.NewRat(actual.Int(), 1)) != 0 {
				return fmt.Errorf("decoded %%s = %%d, want %%s", path, actual.Int(), expected)
			}
		case reflect.Float32, reflect.Float64:
			want, err := expected.Float64()
			if err != nil || actual.Float() != want {
				return fmt.Errorf("decoded %%s = %%v, want %%s", path, actual.Float(), expected)
			}
		default:
			return fmt.Errorf("decoded %%s kind = %%v, want number", path, actual.Kind())
		}
	case string:
		if !actual.IsValid() || actual.Kind() != reflect.String || actual.String() != expected {
			return fmt.Errorf("decoded %%s = %%v, want %%q", path, actual, expected)
		}
	case bool:
		if !actual.IsValid() || actual.Kind() != reflect.Bool || actual.Bool() != expected {
			return fmt.Errorf("decoded %%s = %%v, want %%t", path, actual, expected)
		}
	case nil:
		if actual.IsValid() && !actual.IsZero() {
			return fmt.Errorf("decoded %%s is non-zero, want null", path)
		}
	default:
		return fmt.Errorf("unsupported expected value %%T at %%s", expected, path)
	}
	return nil
}

func main() {
	requests := []request{
%s	}
	results := make([]result, 0, len(requests))
	for _, request := range requests {
		valuesBefore := append(json.RawMessage(nil), request.values...)
		issues, err := generated.Validate%sJSON(request.values)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if !bytes.Equal(request.values, valuesBefore) {
			fmt.Fprintln(os.Stderr, "ValidateJSON mutated input bytes")
			os.Exit(1)
		}
		out := result{Structural: make([]tuple, 0, len(issues))}
		for _, issue := range issues {
			out.Structural = append(out.Structural, tuple{Source: issue.Source, Code: issue.Code, Path: issue.Path})
		}

		var fields generated.%sFields
		if request.structurallyValid {
			fields, err = generated.Decode%s(request.values)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if !bytes.Equal(request.values, valuesBefore) {
				fmt.Fprintln(os.Stderr, "Decode mutated input bytes")
				os.Exit(1)
			}
			if err := assertDecodedFields(fields, request.values); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		if request.availability {
			var conditions generated.%sConditions
			var previousFields generated.%sFields
			// Preserve omitted prev (nil raw bytes) versus explicit JSON, including
			// {}, in the generated-runner request. Check accepts a concrete Fields
			// value, so omission is deliberately converted here to its zero value.
			if request.previousValuesPresent {
				if len(request.previousValues) == 0 {
					fmt.Fprintln(os.Stderr, "previousValues marked present without JSON")
					os.Exit(1)
				}
				if err := json.Unmarshal(request.previousValues, &previousFields); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			} else if request.previousValues != nil {
				fmt.Fprintln(os.Stderr, "omitted previousValues was not preserved as absent before Check conversion")
				os.Exit(1)
			}
			if err := json.Unmarshal(request.conditions, &conditions); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			// Deep-copy and compare the actual typed arguments passed to Check so
			// mutations through nested pointers or slices cannot hide behind aliases.
			fieldsBefore, fieldsErr := clone(fields)
			previousFieldsBefore, previousFieldsErr := clone(previousFields)
			conditionsBefore, conditionsErr := clone(conditions)
			if fieldsErr != nil || previousFieldsErr != nil || conditionsErr != nil {
				fmt.Fprintln(os.Stderr, "clone Check arguments")
				os.Exit(1)
			}
			checked := generated.Check(fields, conditions, previousFields)
			if !reflect.DeepEqual(fields, fieldsBefore) || !reflect.DeepEqual(previousFields, previousFieldsBefore) || !reflect.DeepEqual(conditions, conditionsBefore) {
				fmt.Fprintln(os.Stderr, "Check mutated decoded values, prev, or conditions")
				os.Exit(1)
			}
			availability, err := json.Marshal(checked)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			out.Availability = availability
		}
		results = append(results, out)
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`, requests.String(), schemaName, schemaName, schemaName, schemaName, schemaName)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write generated runner: %v", err)
	}
	goMod := "module profileconformance\n\ngo 1.22\n\nrequire generated v0.0.0\nreplace generated => ./generated\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write runner go.mod: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(dir, ".gocache"),
		"GOMODCACHE="+filepath.Join(dir, ".gomodcache"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated profile conformance runner failed: %v\n%s\n--- source ---\n%s", err, output, source)
	}
	var results []profileConformanceResult
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatalf("decode generated runner output: %v\noutput: %s", err, output)
	}
	return results
}

func TestProfileGeneratedRunnerUsesPreviousSnapshot(t *testing.T) {
	profile := []byte(`{
		"$schema":"https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
		"profileVersion":1,
		"valueSchema":{
			"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object",
			"properties":{"creditCard":{"type":"string"},"bankTransfer":{"type":"string"}},
			"additionalProperties":false
		},
		"umpire":{
			"version":1,
			"fields":{"creditCard":{"isEmpty":"string"},"bankTransfer":{"isEmpty":"string"}},
			"rules":[{"type":"oneOf","group":"Payment","branches":{"creditCard":["creditCard"],"bankTransfer":["bankTransfer"]}}]
		}
	}`)
	source, issues, err := GenerateProfile(profile, Config{PkgName: "profilefixture", SchemaName: "Previous"})
	if err != nil || len(issues) != 0 {
		t.Fatalf("GenerateProfile() = issues %+v, err %v", issues, err)
	}
	values := json.RawMessage(`{"creditCard":"new-card","bankTransfer":"new-bank"}`)
	cases := []profileConformanceFixtureCase{
		{ID: "omitted-prev", Values: values, Conditions: json.RawMessage(`{}`), ExpectedStructure: profileExpectedStructure{Valid: true}, ExpectedAvailability: json.RawMessage(`{}`)},
		{ID: "explicit-prev", Values: values, Conditions: json.RawMessage(`{}`), Prev: json.RawMessage(`{"creditCard":"old-card"}`), ExpectedStructure: profileExpectedStructure{Valid: true}, ExpectedAvailability: json.RawMessage(`{}`)},
	}
	results := profileRunGeneratedCases(t, source, "Previous", cases)
	type status struct {
		Enabled bool `json:"enabled"`
	}
	decode := func(raw json.RawMessage) map[string]status {
		var availability map[string]status
		if err := json.Unmarshal(raw, &availability); err != nil {
			t.Fatal(err)
		}
		return availability
	}
	withoutPrev := decode(results[0].Availability)
	withPrev := decode(results[1].Availability)
	if !withoutPrev["CreditCard"].Enabled || withoutPrev["BankTransfer"].Enabled {
		t.Fatalf("omitted prev selected wrong branch: %s", results[0].Availability)
	}
	if withPrev["CreditCard"].Enabled || !withPrev["BankTransfer"].Enabled {
		t.Fatalf("explicit prev did not select newly satisfied branch: %s", results[1].Availability)
	}
}

func profileAssertStructural(t *testing.T, got []profileStructuralTuple, expected profileExpectedStructure) {
	t.Helper()
	if (len(got) == 0) != expected.Valid {
		t.Fatalf("structural valid = %v, want %v; issues: %+v", len(got) == 0, expected.Valid, got)
	}
	// Structural issue order is normative; compare generated order directly to
	// the canonical fixture order so the harness exposes ordering regressions.
	if !reflect.DeepEqual(got, expected.Issues) {
		t.Fatalf("structural tuples = %+v, want %+v", got, expected.Issues)
	}
}

func profileAssertAvailability(t *testing.T, raw, expectedRaw json.RawMessage) {
	t.Helper()
	if len(raw) == 0 {
		t.Fatal("generated runner did not return availability")
	}
	got, err := profileCanonicalAvailability(raw, false)
	if err != nil {
		t.Fatalf("decode generated availability: %v\noutput: %s", err, raw)
	}
	want, err := profileCanonicalAvailability(expectedRaw, true)
	if err != nil {
		t.Fatalf("decode expected availability: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("availability = %s, want %s", got, want)
	}
}

// profileCanonicalAvailability compares the complete JSON shape instead of
// decoding statuses into a permissive struct that could drop unknown members or
// synthesize missing bools. Expected fixture field names are mapped to generated
// Go field names before both sides are canonically marshaled.
func profileCanonicalAvailability(raw json.RawMessage, mapGeneratedNames bool) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var availability map[string]any
	if err := decoder.Decode(&availability); err != nil {
		return nil, err
	}
	if mapGeneratedNames {
		mapped := make(map[string]any, len(availability))
		for jsonName, status := range availability {
			goName := codegen.GoFieldName(jsonName)
			if _, exists := mapped[goName]; exists {
				return nil, fmt.Errorf("availability field names collide at %q", goName)
			}
			mapped[goName] = status
		}
		availability = mapped
	}
	return json.Marshal(availability)
}

func TestProfileCanonicalAvailabilityPreservesCompleteStatusShape(t *testing.T) {
	expected := json.RawMessage(`{"json-name":{"enabled":true,"required":false,"satisfied":true,"fair":true,"reason":null,"reasons":[],"futureStatus":true}}`)
	withoutUnknown := json.RawMessage(`{"JsonName":{"enabled":true,"required":false,"satisfied":true,"fair":true,"reason":null,"reasons":[]}}`)
	missingRequired := json.RawMessage(`{"JsonName":{"enabled":true,"satisfied":true,"fair":true,"reason":null,"reasons":[],"futureStatus":true}}`)

	want, err := profileCanonicalAvailability(expected, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]json.RawMessage{
		"unknown member": withoutUnknown,
		"missing member": missingRequired,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := profileCanonicalAvailability(raw, false)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(got, want) {
				t.Fatalf("incomplete status shape compared equal: %s", got)
			}
		})
	}
}

func profileAssertDefinitionIssues(t *testing.T, got []DefinitionIssue, expected []profileDefinitionTuple) {
	t.Helper()
	counts := make(map[profileDefinitionTuple]int, len(got))
	actual := make([]profileDefinitionTuple, len(got))
	for i, issue := range got {
		actual[i] = profileDefinitionTuple{Code: issue.Code, Path: issue.Path}
		counts[actual[i]]++
	}
	for _, issue := range expected {
		counts[issue]--
	}
	for _, count := range counts {
		if count != 0 {
			t.Fatalf("definition issue tuples = %+v, want unordered %+v", actual, expected)
		}
	}
	if len(got) != len(expected) {
		t.Fatalf("definition issue tuples = %+v, want unordered %+v", actual, expected)
	}
}

// profileConformanceName turns arbitrary fixture identifiers into a stable,
// exported Go identifier. Prefixing prevents digit-leading names and keywords.
func profileConformanceName(id string) string {
	var b strings.Builder
	b.WriteString("Profile")
	upperNext := true
	for _, r := range id {
		switch {
		case unicode.IsLetter(r):
			if upperNext {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(r)
			}
			upperNext = false
		case unicode.IsDigit(r):
			b.WriteRune(r)
			upperNext = false
		default:
			upperNext = true
		}
	}
	return b.String()
}
