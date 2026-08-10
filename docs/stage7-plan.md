# Stage 7 — Generate structural Go types and validation

Branch: `feat/json-spec-gen`. Builds on merged Stage 6 (profile + valueSchema validation).

## Architecture

New package `pkg/structgen` owns the valueSchema-driven structural codegen. It is
fee-only invoked when a profile supplies a valueSchema (chunk 8: no valueSchema ⇒
output unchanged, existing availability-only path preserved).

```
valueSchema (validated in pkg/umpiregen)
      │
      ▼
pkg/structgen
  spec.go   : structural IR (Spec, TypeDef, FieldDef, FieldType)
  build.go  : valueSchema → Spec  (chunk 1-4 type mapping)
  emit.go   : Spec → Go source: structs/enums/unions (chunks 2-4)
  validate.go: emit Validate() with RFC 6901 paths + normalized codes (chunk 5)
  decode.go : emit strict UnmarshalJSON, reject unknown/trailing (chunk 6)
      │
      ▼
pkg/umpiregen : wires root valueSchema types into the generated root Fields,
                and generates availability Check()/Challenge() over richer types
                without calling structural validation (chunk 7)
```

## Chunks (each independently testable, "done when" keeps codegen unit-green)

1. **Type mapping** — deterministic valueSchema → IR for required/optional/scalar/
   object/slice/enum/reference shapes. Unit tests per shape.
2. **Object generation** — named nested structs; original JSON property names in tags.
3. **Enum generation** — named types + constants; wire values unchanged.
4. **Tagged unions** — strict discriminator-based decoding from each branch's `const`.
5. **Structural validation** — dependency-free `Validate()`; normalized issue codes +
   RFC 6901 (JSON Pointer) paths.
6. **Strict decoding** — reject unknown properties (where `additionalProperties:false`
   or profile-wide required); reject trailing JSON values. Duplicate-key and raw-byte
   limits stay host responsibilities.
7. **Availability integration** — existing `Check()`/`Challenge()` over richer field
   types; do NOT call structural validation from those functions.
8. **Compatibility** — availability-only generated output unchanged for inputs without
   a valueSchema.

Constraints: stdlib only; never expose `any` for a profile-declared object or union
value.

## Design decisions

- **Placement:** new `pkg/structgen` (not `pkg/codegen`). Keeps the availability
  generator untouched (chunk 8 base), isolates "no `any`" + validation into one place.
- **Type-robust availability (chunk 7):** `Check/Challenge` satisfaction must not
  switch on the closed `GoType` constant set. For richer types, treat a field as
  satisfied by `isEmpty` semantics generically (e.g. slice/struct present checks)
  rather than a literal `[]string` match, so `[]Node`, `WorkflowType`, and unions
  all compile.
- **Optionality:** optional scalar fields emit pointer types; required emit value
  types. Omission ⇒ nil/zero; explicit `null` ⇒ handled by decoder (chunk 6).
- **Union decoding:** discriminated on the shared `const` property; each branch
  decoded into a struct with all its declared fields; `UnmarshalJSON` selects branch
  by discriminator value, rejects unknown discriminators.

## Target output (avenor-workflow)

```go
type Workflow struct {
    Nodes        []Node         `json:"nodes"`
    Edges        []Edge         `json:"edges"`
    Title        string         `json:"title"`
    WorkflowType WorkflowType   `json:"workflowType"`
    Version      int            `json:"version"`
    Status       map[string]any // status:{} unresolved — see edge cases
    MaxAttempts  *int           `json:"maxAttempts"`
}
type Node struct { ID string `json:"id"`; Action Action `json:"action"` }
type Action struct {
    Kind ActionKind `json:"kind"`
    // branch fields
    Instructions *string `json:"instructions,omitempty"`
    Command      *string `json:"command,omitempty"`
    Timeout      *int    `json:"timeout,omitempty"`
    MaxIterations *int   `json:"maxIterations,omitempty"`
    Condition    *string `json:"condition,omitempty"`
}
type ActionKind string
const ( ActionManual ActionKind = "manual"; ActionRun ActionKind = "run"; ActionLoop ActionKind = "loop" )
type Edge struct { From, To string }
```

## Open edge cases (flagged, not silently decided)

- `status: {}` (object type with no `properties`) — empty-schema object. Decide
  representation (currently could degrade to a named empty struct; never `any`).
- Scalar fields with only `const` (e.g. `version: {const:1}`) — fixed named value.
- Naming collisions after `GoFieldName` conversion (already validated in Stage 6).
