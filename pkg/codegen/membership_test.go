package codegen_test

import (
	"testing"

	"github.com/umpire-tools/umpire-go-gen/pkg/internal/testutil"
	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
	"github.com/umpire-tools/umpire-go-gen/pkg/umpiregen"
)

// membershipCase is one set of input values and the enabled state each named
// gate must have after Check.
type membershipCase struct {
	name       string
	values     map[string]any
	conditions map[string]any
	enabled    map[string]bool
}

// assertMembershipGates generates plain-mode code for schemaJSON, compiles it,
// and checks every case's gates through the generated Check.
func assertMembershipGates(t *testing.T, schemaJSON string, cases []membershipCase) {
	t.Helper()
	source, err := umpiregen.Generate([]byte(schemaJSON), umpiregen.Config{PkgName: "pkg", SchemaName: "Membership"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	testutil.AssertGeneratedPackageCompiles(t, source)
	parsed, err := schema.Parse([]byte(schemaJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			avail, err := runCheck(t, "Membership", parsed, source, tc.values, tc.conditions, nil)
			if err != nil {
				t.Fatalf("runCheck: %v", err)
			}
			for gate, want := range tc.enabled {
				got, ok := avail[gate]
				if !ok {
					t.Errorf("%s missing from availability", gate)
					continue
				}
				if got.Enabled != want {
					t.Errorf("%s.Enabled = %v, want %v", gate, got.Enabled, want)
				}
			}
		})
	}
}

func TestPresenceOnAnyArrayField(t *testing.T) {
	assertMembershipGates(t, `{"version": 1,
		"fields": {"slots": {"isEmpty": "array"}, "presentGate": {}, "absentGate": {}, "containsGate": {}},
		"rules": [
			{"type": "enabledWhen", "field": "presentGate", "when": {"op": "present", "field": "slots"}},
			{"type": "enabledWhen", "field": "absentGate", "when": {"op": "absent", "field": "slots"}},
			{"type": "enabledWhen", "field": "containsGate", "when": {"op": "contains", "field": "slots", "value": true}}]}`,
		[]membershipCase{
			{name: "absent", values: map[string]any{}, enabled: map[string]bool{"PresentGate": false, "AbsentGate": true}},
			{name: "empty", values: map[string]any{"slots": []any{}}, enabled: map[string]bool{"PresentGate": false, "AbsentGate": true}},
			{name: "filled", values: map[string]any{"slots": []any{true}}, enabled: map[string]bool{"PresentGate": true, "AbsentGate": false, "ContainsGate": true}},
		})
}
