# JSON Schema Composition Profile

> **Profile v1** — canonical specification for combining Umpire field availability and
> JSON Schema structural validation in one portable document.

## Overview

The composition profile wraps two independent authorities in a single JSON document:

| Authority | Responsibility |
|-----------|---------------|
| **Umpire JSON** | Field availability, satisfaction, availability-conditioned requiredness, fairness, reasons, transitions, and existing portable validator behavior |
| **JSON Schema** | Structural correctness: primitive types, nested objects, arrays, enums, constants, bounds, strict properties, and tagged unions |

Neither authority is translated into the other. Structural validation runs against raw
runtime values independently of Umpire satisfaction or availability. It must catch
malformed values even when `isEmpty` reports them unsatisfied.

## Canonical shape

```json
{
  "$schema": "https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json",
  "profileVersion": 1,
  "valueSchema": {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "type": "object",
    "properties": {},
    "additionalProperties": false
  },
  "umpire": {
    "version": 1,
    "fields": {},
    "rules": []
  }
}
```

- `$schema` — identifies the profile schema (profile v1 only).
- `profileVersion` — must be `1`. Independent of the Umpire document `version: 1`.
- `valueSchema` — a JSON Schema 2020-12 document describing structural constraints.
- `umpire` — a standard Umpire v1 availability document.

## Authority ordering

1. **Structural validation** runs against the raw runtime value using the `valueSchema`.
2. **Umpire evaluation** runs using the `umpire` document — independently of structural
   results.
3. Results are returned separately: structural issues under `structure` and field
   availability under `availability`.

Structural validation never:
- Mutates `ump.check()` output
- Enters the core Umpire evaluator hot path
- Gates on `enabled` or `satisfied` status

## Profile consistency rules

Compilation must reject a profile unless:

1. `valueSchema.$schema` is JSON Schema 2020-12.
2. The value-schema root has `type: "object"`, a `properties` object, and
   `additionalProperties: false`.
3. Root property names exactly equal `umpire.fields` names. Profile v1 favors
   deterministic field correspondence over partial projection.
4. Every Umpire default validates against the corresponding property schema.
5. A non-`present` portable `isEmpty` strategy is compatible with the property's
   structural type:
   - `"string"` → string
   - `"number"` → number or integer
   - `"boolean"` → boolean
   - `"array"` → array
   - `"object"` → object
6. Root `required` is permitted and retains JSON Schema semantics. It is never
   inferred from or compared as equivalent to Umpire `required`.
7. Every object schema declares `additionalProperties: false`, and every `required`
   name exists in that object's `properties`.
8. Property and `$defs` names produce valid, non-keyword Go identifiers through
   the existing `codegen.GoFieldName` conversion. Compilation rejects empty, invalid,
   or colliding converted names.
9. Integer schemas use signed 64-bit Go values but restrict schema literals and
   conformance instances to JavaScript's safe-integer range. Number schemas use finite
   IEEE 754 binary64 values in both runtimes.
10. Unsupported JSON Schema keywords fail profile compilation. They are not ignored
    or placed in Umpire `excluded`.

## Supported JSON Schema vocabulary

Profile v1 supports a closed, code-generatable subset:

| Feature | Details |
|---------|---------|
| **Objects** | `type: "object"`, `properties`, nested/root `required`, `additionalProperties: false` |
| **Scalars** | `string`, `number`, `integer`, `boolean`. Rejects `null` types, nullable type arrays, and explicit `null` |
| **Arrays** | `type: "array"` with one homogeneous `items` schema, optional `minItems`, `maxItems` |
| **String constraints** | `minLength`, `maxLength` (measured in Unicode code points) |
| **Numeric bounds** | `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum` |
| **Enums** | Homogeneous string, boolean, integer, or number `enum` values |
| **Constants** | Primitive `const` values; requires a compatible explicit type |
| **References** | Local, acyclic root `$defs` only. Form: `{ "$ref": "#/$defs/<name>" }` |
| **Tagged unions** | `oneOf` only. Every branch is an object with the same required discriminator property and distinct string `const` |
| **Annotations** | `title` and `description` can be preserved but do not affect validation |

### Rejected JSON Schema features

Profile v1 compiles these (they are not silently ignored):

| Feature | Rejection reason |
|---------|-----------------|
| Remote, dynamic, or recursive references | No network or filesystem resolution |
| Root unions and untagged unions | Non-deterministic code generation |
| `anyOf`, `allOf`, `not`, conditionals | Cannot generate code |
| Tuple arrays and `contains` | Requires heterogeneous element schemas |
| `uniqueItems`, `pattern`, dependent properties | Not supported in v1 |
| `format`, `multipleOf`, content keywords | Not supported in v1 |
| Coercion, transformation, JSON Schema `default` | Retained in Umpire domain |
| Explicit JSON `null` | Use omission for optionality |

## Go name compatibility

Property and `$defs` names are converted to Go identifiers using
`codegen.GoFieldName`. Compilation must reject:

- Names that produce empty strings
- Names that collide with Go keywords
- Names that collide with generated symbols (types, constants, variants, helpers)

Profile v1 never resolves collisions with numeric suffixes.

## Safe integer and binary64 semantics

- **Integer schemas**: use signed 64-bit Go values (`int64`) but restrict all schema
  literals and conformance instances to JavaScript's safe-integer range
  (-(2⁵³ − 1) to 2⁵³ − 1).
- **Number schemas**: use finite IEEE 754 binary64 values (`float64`) in both
  TypeScript and Go runtimes.
- Profile compilation validates that schema bounds are within these ranges and
  rejects out-of-range values as definition issues.

## Structural issue normalization

```ts
type StructuralIssue = {
  source: 'json-schema'
  code: string
  path: string // RFC 6901 pointer into the instance
  schemaPath?: string // RFC 6901 pointer into valueSchema (non-normative)
  message: string // non-normative across runtimes
}
```

Normative cross-runtime fields are `source`, `code`, and `path`. Human messages and
validator-internal schema paths are not required to match across runtimes.

### Issue codes

**JSON Schema keyword codes** (from the supported vocabulary):

`type`, `required`, `additionalProperties`, `minItems`, `maxItems`, `minLength`,
`maxLength`, `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum`, `enum`,
`const`

**Profile-defined runtime codes**: `discriminator`, `safeInteger`

**Profile definition codes**: `invalidProfile`, `unsupportedKeyword`, `fieldMismatch`,
`incompatibleIsEmpty`, `invalidDefault`, `invalidReference`, `referenceCycle`,
`invalidDiscriminator`, `invalidName`, `nameCollision`, `unsafeNumber`

### Normalization rules

1. Run structural validation with all-errors behavior; report every independently
   observable violation.
2. A parent `type` failure suppresses descendant issues that require traversing that
   value.
3. A missing required property points to the missing child path.
4. An unknown property points to that property's path.
5. A missing tagged-union discriminator produces `required` at the discriminator path.
6. An unknown discriminator produces `discriminator` at the discriminator path.
7. Once a discriminator selects a branch, report only that branch's issues; suppress
   generic `oneOf` branch noise.
8. Deduplicate identical `(source, code, path)` tuples.
9. Sort issues by `path`, then `code`.

Raw JSON duplicate-key detection, byte limits, and parser depth limits remain
host-ingress responsibilities.

## Security and resource boundaries

- Profile v1 does not define network or filesystem schema resolution. Remote
  references fail compilation.
- Structural validation has no access to Umpire field values, conditions, or
  previous snapshots.
- Schema compilation happens once at profile load time; structural validation is
  O(n) in the input value size for the supported vocabulary.
- Unbounded string patterns and `uniqueItems` are excluded to avoid pathological
  runtime cost in generated Go. String length constraints (`minLength`, `maxLength`)
  are bounded by compiled schema literal values.
- Array bounds (`minItems`, `maxItems`) are schema literal values; arbitrary
  arrays are bounded only by available host memory.

## Profile schema URL

The canonical profile schema is published at:

```
https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json
```
