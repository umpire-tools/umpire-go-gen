package umpiregen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
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

// profileRunGeneratedCases compiles one generated package per fixture and calls
// Validate<Schema>JSON with each fixture's raw values JSON. Structurally invalid
// cases assert structural parity only; they never decode fields or call Check.
// Structurally valid cases with availability expectations compare Check's complete output.
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
		fmt.Fprintf(&requests, "{values: json.RawMessage(%q), previousValues: json.RawMessage(\"{}\"), conditions: json.RawMessage(%q), availability: %t},\n",
			string(tc.Values), string(conditions), tc.ExpectedStructure.Valid && len(tc.ExpectedAvailability) != 0)
	}

	mainSource := fmt.Sprintf(`package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	generated "generated"
)

type request struct {
	values json.RawMessage
	previousValues json.RawMessage
	conditions json.RawMessage
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

func main() {
	requests := []request{
%s	}
	results := make([]result, 0, len(requests))
	var firstAvailabilityRequest *request
	for i := range requests {
		request := requests[i]
		valuesBefore := append(json.RawMessage(nil), request.values...)
		previousValuesBefore := append(json.RawMessage(nil), request.previousValues...)
		conditionBytesBefore := append(json.RawMessage(nil), request.conditions...)
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
		if request.availability {
			if firstAvailabilityRequest == nil {
				firstAvailabilityRequest = &requests[i]
			}
			var fields generated.%sFields
			var conditions generated.%sConditions
			var previousFields generated.%sFields
			if err := json.Unmarshal(request.values, &fields); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if err := json.Unmarshal(request.previousValues, &previousFields); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if err := json.Unmarshal(request.conditions, &conditions); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fieldsBefore, err := json.Marshal(fields)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			previousFieldsBefore, err := json.Marshal(previousFields)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			conditionsBefore, err := json.Marshal(conditions)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			checked := generated.Check(fields, conditions, previousFields)
			fieldsAfter, fieldsErr := json.Marshal(fields)
			previousFieldsAfter, previousFieldsErr := json.Marshal(previousFields)
			conditionsAfter, conditionsErr := json.Marshal(conditions)
			if fieldsErr != nil || previousFieldsErr != nil || conditionsErr != nil || !bytes.Equal(fieldsBefore, fieldsAfter) || !bytes.Equal(previousFieldsBefore, previousFieldsAfter) || !bytes.Equal(conditionsBefore, conditionsAfter) {
				fmt.Fprintln(os.Stderr, "Check mutated decoded fields, previous fields, or conditions")
				os.Exit(1)
			}
			if !bytes.Equal(request.values, valuesBefore) || !bytes.Equal(request.previousValues, previousValuesBefore) || !bytes.Equal(request.conditions, conditionBytesBefore) {
				fmt.Fprintln(os.Stderr, "generated validation/check mutated input bytes")
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
	if firstAvailabilityRequest != nil {
		mutationValuesBefore := append(json.RawMessage(nil), firstAvailabilityRequest.values...)
		mutationPreviousBefore := append(json.RawMessage(nil), firstAvailabilityRequest.previousValues...)
		mutationConditionsBefore := append(json.RawMessage(nil), firstAvailabilityRequest.conditions...)
		var mutationValues map[string]json.RawMessage
		if err := json.Unmarshal(firstAvailabilityRequest.values, &mutationValues); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		mutationValues["__mutation_only_marker"] = json.RawMessage("true")
		mutationPrevious, err := json.Marshal(mutationValues)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		mutationPreviousBytesBefore := append(json.RawMessage(nil), mutationPrevious...)
		var fields generated.%sFields
		var conditions generated.%sConditions
		var previousFields generated.%sFields
		if err := json.Unmarshal(firstAvailabilityRequest.values, &fields); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := json.Unmarshal(mutationPrevious, &previousFields); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := json.Unmarshal(firstAvailabilityRequest.conditions, &conditions); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fieldsBefore, err := json.Marshal(fields)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		previousFieldsBefore, err := json.Marshal(previousFields)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		conditionsBefore, err := json.Marshal(conditions)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		checked := generated.Check(fields, conditions, previousFields)
		fieldsAfter, fieldsErr := json.Marshal(fields)
		previousFieldsAfter, previousFieldsErr := json.Marshal(previousFields)
		conditionsAfter, conditionsErr := json.Marshal(conditions)
		if fieldsErr != nil || previousFieldsErr != nil || conditionsErr != nil || !bytes.Equal(fieldsBefore, fieldsAfter) || !bytes.Equal(previousFieldsBefore, previousFieldsAfter) || !bytes.Equal(conditionsBefore, conditionsAfter) {
			fmt.Fprintln(os.Stderr, "mutation-only generated-runner check mutated decoded fields, previous fields, or conditions")
			os.Exit(1)
		}
		if !bytes.Equal(firstAvailabilityRequest.values, mutationValuesBefore) || !bytes.Equal(firstAvailabilityRequest.previousValues, mutationPreviousBefore) || !bytes.Equal(firstAvailabilityRequest.conditions, mutationConditionsBefore) || !bytes.Equal(mutationPrevious, mutationPreviousBytesBefore) {
			fmt.Fprintln(os.Stderr, "mutation-only generated-runner check mutated raw bytes")
			os.Exit(1)
		}
		_ = checked
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`, requests.String(), schemaName, schemaName, schemaName, schemaName, schemaName, schemaName, schemaName)
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

func profileAssertStructural(t *testing.T, got []profileStructuralTuple, expected profileExpectedStructure) {
	t.Helper()
	if (len(got) == 0) != expected.Valid {
		t.Fatalf("structural valid = %v, want %v; issues: %+v", len(got) == 0, expected.Valid, got)
	}
	want := append([]profileStructuralTuple(nil), expected.Issues...)
	got = append([]profileStructuralTuple(nil), got...)
	profileSortStructuralTuples(got)
	profileSortStructuralTuples(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("structural tuples = %+v, want %+v", got, want)
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
	actual := make([]profileDefinitionTuple, len(got))
	for i, issue := range got {
		actual[i] = profileDefinitionTuple{Code: issue.Code, Path: issue.Path}
	}
	want := append([]profileDefinitionTuple(nil), expected...)
	sort.Slice(actual, func(i, j int) bool {
		if actual[i].Path != actual[j].Path {
			return actual[i].Path < actual[j].Path
		}
		return actual[i].Code < actual[j].Code
	})
	sort.Slice(want, func(i, j int) bool {
		if want[i].Path != want[j].Path {
			return want[i].Path < want[j].Path
		}
		return want[i].Code < want[j].Code
	})
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("definition issue tuples = %+v, want %+v", actual, want)
	}
}

func profileSortStructuralTuples(tuples []profileStructuralTuple) {
	sort.Slice(tuples, func(i, j int) bool {
		if tuples[i].Path != tuples[j].Path {
			return tuples[i].Path < tuples[j].Path
		}
		if tuples[i].Code != tuples[j].Code {
			return tuples[i].Code < tuples[j].Code
		}
		return tuples[i].Source < tuples[j].Source
	})
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
