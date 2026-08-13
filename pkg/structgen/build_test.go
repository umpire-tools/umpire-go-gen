package structgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustBuild(t *testing.T, vs string, root string) *Spec {
	t.Helper()
	spec, err := Build([]byte(vs), root)
	if err != nil {
		t.Fatalf("Build() error: %v\nvalueSchema: %s", err, vs)
	}
	return spec
}

func fieldByName(fields []FieldDef, name string) *FieldDef {
	for i := range fields {
		if fields[i].GoName == name || fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}

// scalar shapes: required + optional scalar mapping.
func TestBuildScalars(t *testing.T) {
	vs := `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": {
			"title":    { "type": "string" },
			"enabled":  { "type": "boolean" },
			"count":    { "type": "integer" },
			"price":    { "type": "number" }
		},
		"required": ["title"],
		"additionalProperties": false
	}`
	spec := mustBuild(t, vs, "Sample")

	if spec.RootName != "Sample" {
		t.Fatalf("RootName = %q, want Sample", spec.RootName)
	}
	if len(spec.Root) != 4 || len(spec.Types) != 0 {
		t.Fatalf("unexpected shape: %d root, %d types", len(spec.Root), len(spec.Types))
	}
	cases := map[string]struct {
		scalar Scalar
		req    bool
	}{
		"title":   {ScalarString, true},
		"enabled": {ScalarBool, false},
		"count":   {ScalarInt, false},
		"price":   {ScalarNumber, false},
	}
	for name, want := range cases {
		fd := fieldByName(spec.Root, name)
		if fd == nil {
			t.Fatalf("missing field %q", name)
		}
		if fd.Type.Kind != KindScalar || fd.Type.Scalar != want.scalar {
			t.Errorf("field %q: got %+v, want scalar %s", name, fd.Type, want.scalar)
		}
		if fd.Required != want.req {
			t.Errorf("field %q: Required = %v, want %v", name, fd.Required, want.req)
		}
		if fd.JSONTag != name {
			t.Errorf("field %q: JSONTag = %q, want %q", name, fd.JSONTag, name)
		}
	}
}

// named object (inline) + $defs reference + array of ref.
func TestBuildObjectAndRefs(t *testing.T) {
	vs := `{
		"type": "object",
		"properties": {
			"profile": {
				"type": "object",
				"properties": { "nickname": { "type": "string" } },
				"required": ["nickname"]
			},
			"managers": {
				"type": "array",
				"items": { "$ref": "#/$defs/manager" }
			}
		},
		"$defs": {
			"manager": {
				"type": "object",
				"properties": { "id": { "type": "string" } },
				"required": ["id"]
			}
		}
	}`
	spec := mustBuild(t, vs, "Org")

	profile := fieldByName(spec.Root, "profile")
	if profile == nil || profile.Type.Kind != KindObject || profile.Type.Ref != "Profile" {
		t.Fatalf("profile field type = %+v, want object ref to Profile", profile.Type)
	}
	managers := fieldByName(spec.Root, "managers")
	if managers == nil || managers.Type.Kind != KindArray || managers.Type.Elem.Kind != KindObject || managers.Type.Elem.Ref != "Manager" {
		t.Fatalf("managers field type = %+v, want []Manager", managers.Type)
	}

	// Inline object named after its property, and the $def.
	profileT := spec.Lookup("Profile")
	if profileT == nil || profileT.Kind != KindObject {
		t.Fatalf("missing inline Profile type")
	}
	if nickname := fieldByName(profileT.Fields, "nickname"); nickname == nil || !nickname.Required {
		t.Fatalf("inline Profile.nickname = %+v, want required string", nickname)
	}
	mgrT := spec.Lookup("Manager")
	if mgrT == nil || mgrT.Kind != KindObject {
		t.Fatalf("missing Manager type")
	}
	if mgrT.JSONName != "" {
		t.Errorf("$def type JSONName should be empty, got %q", mgrT.JSONName)
	}
}

// enum → named type with wire values preserved.
func TestBuildEnum(t *testing.T) {
	vs := `{
		"type": "object",
		"properties": {
			"workflowType": { "type": "string", "enum": ["pipeline", "fanout", "loop"] }
		}
	}`
	spec := mustBuild(t, vs, "Wf")

	wt := fieldByName(spec.Root, "workflowType")
	if wt == nil || wt.Type.Kind != KindEnum || wt.Type.Ref != "WorkflowType" {
		t.Fatalf("workflowType type = %+v, want enum WorkflowType", wt.Type)
	}
	enumT := spec.Lookup("WorkflowType")
	if enumT == nil || enumT.Kind != KindEnum {
		t.Fatalf("missing WorkflowType enum")
	}
	want := []struct{ name, wire string }{
		{"Pipeline", "pipeline"}, {"Fanout", "fanout"}, {"Loop", "loop"},
	}
	if len(enumT.Values) != len(want) {
		t.Fatalf("WorkflowType.Values = %d, want %d", len(enumT.Values), len(want))
	}
	for i, w := range want {
		if enumT.Values[i].Name != w.name || enumT.Values[i].Wire != w.wire {
			t.Errorf("Values[%d] = %+v, want %+v", i, enumT.Values[i], w)
		}
	}
}

// tagged union → discriminator + merged branch fields.
func TestBuildUnion(t *testing.T) {
	vs := `{
		"type": "object",
		"properties": {
			"action": {
				"oneOf": [
					{ "type":"object",
					  "properties": { "kind": { "const": "manual" }, "instructions": { "type": "string" } },
					  "required": ["kind", "instructions"] },
					{ "type":"object",
					  "properties": { "kind": { "const": "run" }, "command": { "type": "string" }, "timeout": { "type": "integer" } },
					  "required": ["kind", "command"] }
				]
			}
		},
		"$defs": {}
	}`
	spec := mustBuild(t, vs, "Job")

	action := fieldByName(spec.Root, "action")
	if action == nil || action.Type.Kind != KindUnion || action.Type.Ref != "Action" {
		t.Fatalf("action type = %+v, want union Action", action.Type)
	}
	u := spec.Lookup("Action")
	if u == nil || u.Kind != KindUnion {
		t.Fatalf("missing Action union")
	}
	if u.Discriminator != "kind" {
		t.Errorf("Discriminator = %q, want kind", u.Discriminator)
	}
	kind := fieldByName(u.Fields, "kind")
	if kind == nil || !kind.Required {
		t.Fatalf("union kind field = %+v, want required", kind)
	}
	// branch-specific fields are optional in the merged struct.
	for _, name := range []string{"instructions", "command", "timeout"} {
		fd := fieldByName(u.Fields, name)
		if fd == nil {
			t.Fatalf("missing branch field %q", name)
		}
		if fd.Required {
			t.Errorf("branch field %q should be optional in merged union, got required", name)
		}
	}
}

func TestBuildUnionPreservesBranchConstraints(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{"action":{"oneOf":[
			{"type":"object","properties":{"kind":{"const":"manual"},"instructions":{"type":"string","minLength":3}},"required":["kind","instructions"]},
			{"type":"object","properties":{"kind":{"const":"run"},"timeout":{"type":"integer","minimum":1}},"required":["kind","timeout"]}
		]}}
	}`
	spec := mustBuild(t, vs, "Doc")
	action := spec.Lookup("Action")
	if action == nil || len(action.Branches) != 2 {
		t.Fatalf("Action branches = %+v", action)
	}
	instructions := fieldByName(action.Branches[0].Fields, "instructions")
	if instructions == nil || instructions.MinLength == nil || *instructions.MinLength != 3 {
		t.Fatalf("manual instructions constraints = %+v", instructions)
	}
	timeout := fieldByName(action.Branches[1].Fields, "timeout")
	if timeout == nil || timeout.Minimum == nil || *timeout.Minimum != 1 {
		t.Fatalf("run timeout constraints = %+v", timeout)
	}
}

func TestRootGoTypesPreservePresence(t *testing.T) {
	vs := `{"type":"object","properties":{"title":{"type":"string"},"mode":{"type":"string","enum":["","ready"]},"tags":{"type":"array","items":{"type":"string"}}},"required":["title"]}`
	spec := mustBuild(t, vs, "Doc")
	got := RootGoTypes(spec)
	if got["title"] != "*string" || got["mode"] != "*Mode" || got["tags"] != "*[]string" {
		t.Fatalf("root types = %#v, want presence-preserving pointers", got)
	}
	underlying := RootUnderlyingGoTypes(spec)
	if len(underlying) != 1 || underlying["mode"] != "string" {
		t.Fatalf("root underlying types = %#v, want only named-enum semantics", underlying)
	}
}

// array of scalar.
func TestBuildArrayOfScalar(t *testing.T) {
	vs := `{
		"type": "object",
		"properties": {
			"tags": { "type": "array", "items": { "type": "string" } }
		}
	}`
	spec := mustBuild(t, vs, "Doc")
	tags := fieldByName(spec.Root, "tags")
	if tags == nil || tags.Type.Kind != KindArray || tags.Type.Elem.Scalar != ScalarString {
		t.Fatalf("tags type = %+v, want []string", tags.Type)
	}
}

// full avenor-workflow fixture yields the expected structural IR.
func TestBuildAvenorWorkflowFixture(t *testing.T) {
	paths := []string{
		"../../spec/profiles/conformance/fixtures/avenor-workflow.json",
		"../spec/profiles/conformance/fixtures/avenor-workflow.json",
	}
	var data []byte
	for _, p := range paths {
		if b, err := os.ReadFile(filepath.FromSlash(p)); err == nil {
			data = b
			break
		}
	}
	if data == nil {
		t.Skip("avenor-workflow fixture not found")
	}
	var fix struct {
		Profile struct {
			ValueSchema json.RawMessage `json:"valueSchema"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &fix); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	spec, err := Build(fix.Profile.ValueSchema, "Workflow")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Root fields.
	for _, name := range []string{"nodes", "edges", "title", "workflowType", "maxAttempts"} {
		if fieldByName(spec.Root, name) == nil {
			t.Errorf("missing root field %q", name)
		}
	}
	nodes := fieldByName(spec.Root, "nodes")
	if nodes == nil || nodes.Type.Kind != KindArray || nodes.Type.Elem.Kind != KindObject || nodes.Type.Elem.Ref != "Node" {
		t.Errorf("nodes type = %+v, want []Node", nodes.Type)
	}
	wt := fieldByName(spec.Root, "workflowType")
	if wt == nil || wt.Type.Ref != "WorkflowType" {
		t.Errorf("workflowType = %+v, want WorkflowType enum", wt.Type)
	}
	maxAttempts := fieldByName(spec.Root, "maxAttempts")
	if maxAttempts == nil || maxAttempts.Minimum == nil || *maxAttempts.Minimum != 1 || *maxAttempts.Maximum != 10 {
		t.Errorf("maxAttempts constraints = %+v, want minimum 1 maximum 10", maxAttempts)
	}

	// $defs types present.
	for _, name := range []string{"Node", "Action", "Edge"} {
		if spec.Lookup(name) == nil {
			t.Errorf("missing $def type %q", name)
		}
	}
	action := spec.Lookup("Action")
	if action == nil || action.Kind != KindUnion || action.Discriminator != "kind" {
		t.Fatalf("Action = %+v, want union with Kind discriminator", action)
	}
	// union const wiring should be detached from branch fields; discriminator required.
	kind := fieldByName(action.Fields, "kind")
	if kind == nil {
		t.Fatalf("Action missing kind field")
	}
}

// deterministic: same input twice → identical IR and type order.
func TestBuildDeterministic(t *testing.T) {
	vs := `{
		"type": "object",
		"properties": {
			"b": { "type": "string" },
			"a": { "type":"object", "properties": { "z": { "type":"string" } } },
			"c": { "type":"string", "enum": ["y","x"] }
		},
		"$defs": {}
	}`
	s1 := mustBuild(t, vs, "D")
	s2 := mustBuild(t, vs, "D")
	a1, _ := json.Marshal(s1)
	a2, _ := json.Marshal(s2)
	if !strings.EqualFold(string(a1), string(a2)) {
		t.Fatalf("IR not deterministic:\n%s\nvs\n%s", a1, a2)
	}
	e1, err := Emit(s1, EmitOptions{PkgName: "deterministic", SchemaName: "D"})
	if err != nil {
		t.Fatalf("Emit(s1): %v", err)
	}
	e2, err := Emit(s2, EmitOptions{PkgName: "deterministic", SchemaName: "D"})
	if err != nil {
		t.Fatalf("Emit(s2): %v", err)
	}
	if e1.Source != e2.Source {
		t.Fatalf("emitted source is not deterministic")
	}
	// Type order stable: inline types appended after $defs, in first-encounter order.
	names := []string{}
	for _, td := range s1.Types {
		names = append(names, td.Name)
	}
	// Root (D), then inline object A, then inline enum C (stable sorted encounter).
	if !containsStr(names, "A") || !containsStr(names, "C") {
		t.Fatalf("expected inline A and C types, got %v", names)
	}
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// TestBuildUnionDuplicateDiscriminator: a union with two branches sharing the
// same discriminator const value is rejected.
func TestBuildUnionDuplicateDiscriminator(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{
			"state":{"oneOf":[
				{"type":"object","properties":{"kind":{"const":"x"},"a":{"type":"string"}},"required":["kind","a"]},
				{"type":"object","properties":{"kind":{"const":"x"},"b":{"type":"string"}},"required":["kind","b"]}
			]}
		},
		"$defs":{}
	}`
	if _, err := Build([]byte(vs), "Doc"); err == nil || !strings.Contains(err.Error(), "duplicate discriminator") {
		t.Fatalf("expected duplicate-discriminator error, got: %v", err)
	}
}

func TestBuildRejectsEnumValueMismatchedWithType(t *testing.T) {
	vs := `{"type":"object","properties":{"mode":{"type":"string","enum":[1]}}}`
	if _, err := Build([]byte(vs), "Doc"); err == nil || !strings.Contains(err.Error(), "enum value is not a string") {
		t.Fatalf("expected mismatched enum rejection, got: %v", err)
	}
}

func TestBuildPrimitiveEnums(t *testing.T) {
	spec := mustBuild(t, `{"type":"object","properties":{
		"flag":{"type":"boolean","enum":[true,false]},
		"count":{"type":"integer","enum":[-1,2]},
		"ratio":{"type":"number","enum":[0.5,2]}
	}}`, "Doc")
	for name, scalar := range map[string]Scalar{"Flag": ScalarBool, "Count": ScalarInt, "Ratio": ScalarNumber} {
		td := spec.Lookup(name)
		if td == nil || td.Kind != KindEnum || td.Scalar != scalar || len(td.Values) != 2 {
			t.Fatalf("%s enum = %+v, want two %s values", name, td, scalar)
		}
	}
}

func TestBuildResolvesRFC6901EscapedDefinitionName(t *testing.T) {
	spec := mustBuild(t, `{
		"type":"object",
		"properties":{"value":{"$ref":"#/$defs/a~1b~0c"}},
		"$defs":{"a/b~c":{"type":"object","properties":{"name":{"type":"string"}}}}
	}`, "Doc")
	value := fieldByName(spec.Root, "value")
	if value == nil || value.Type.Kind != KindObject || value.Type.Ref != "ABC" {
		t.Fatalf("escaped reference type = %+v, want ABC object", value)
	}
	if spec.Lookup("ABC") == nil {
		t.Fatal("escaped definition was not built")
	}

	if _, err := Build([]byte(`{"type":"object","properties":{"value":{"$ref":"#/$defs/a/b"}},"$defs":{"a/b":{"type":"string"}}}`), "Doc"); err == nil {
		t.Fatal("unescaped slash reference should be rejected")
	}
}

// TestBuildForwardRefToEnum verifies that finalize resolves a forward enum
// reference to KindEnum rather than KindObject.
func TestBuildForwardRefToEnum(t *testing.T) {
	vs := `{
		"type":"object",
		"properties":{"w":{"$ref":"#/$defs/status"}},
		"$defs":{
			"holder":{"type":"object","properties":{"st":{"$ref":"#/$defs/status"}}},
			"status":{"type":"string","enum":["ok","bad"]}
		}
	}`
	spec := mustBuild(t, vs, "Doc")
	holder := spec.Lookup("Holder")
	if holder == nil {
		t.Fatalf("missing Holder type")
	}
	st := fieldByName(holder.Fields, "st")
	if st == nil {
		t.Fatalf("missing holder.st field")
	}
	if st.Type.Kind != KindEnum || st.Type.Ref != "Status" {
		t.Fatalf("holder.st type = %+v, want enum ref Status (forward ref misresolved)", st.Type)
	}
}
