# Umpire Schema Reference

## Top-level shape

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `version` | `1` (literal) | yes | Schema version |
| `conditions` | `Record<string, JsonConditionDef>` | no | Named runtime conditions |
| `fields` | `Record<string, JsonFieldDef>` | yes | Field definitions (min 1) |
| `rules` | `JsonRule[]` | yes | Rule array |
| `validators` | `Record<string, JsonValidatorDef>` | no | Named portable validators |
| `excluded` | `ExcludedRule[]` | no | Rules excluded during migration |

## Rule types

The `type` field discriminates between rule variants.

| `type` | Required properties | Description |
|--------|-------------------|-------------|
| `enabledWhen` | `field`, `when` | Enable a field when an expression is true |
| `disables` | `source` / `when`, `targets` | Disable target fields when a source is satisfied |
| `requires` | `field`, (`dependency` \| `dependencies` \| `when`) | Require a dependency before a field is enabled |
| `fairWhen` | `field`, `when` | Mark a field as foul when an expression is true |
| `check` | `field`, `op` (and validator params) | Attach a portable validator to a field |
| `oneOf` | `group`, `branches` | Present exactly one named branch |
| `anyOf` | `rules` | At least one nested rule must pass |
| `eitherOf` | `group`, `branches` | Named groups of ANDed rules |

All rule types accept an optional `reason` string for the blocking message.

### `requires` variants

```json
// Single dependency
{ "type": "requires", "field": "a", "dependency": "b" }

// Multiple dependencies
{ "type": "requires", "field": "a", "dependencies": ["b", { "op": "present", "field": "c" }] }

// Conditional dependency
{ "type": "requires", "field": "a", "when": { "op": "eq", "field": "b", "value": true } }
```

### `disables` variants

```json
// Source field
{ "type": "disables", "source": "a", "targets": ["b", "c"] }

// Conditional
{ "type": "disables", "when": { "op": "present", "field": "a" }, "targets": ["b"] }
```

## Expression operators

The `op` field discriminates between expression variants.

### Comparisons

| `op` | Properties | Description |
|------|-----------|-------------|
| `eq` | `field`, `value` | Field equals value |
| `neq` | `field`, `value` | Field does not equal value |
| `gt` | `field`, `value` | Field greater than value (number) |
| `gte` | `field`, `value` | Field greater than or equal (number) |
| `lt` | `field`, `value` | Field less than value (number) |
| `lte` | `field`, `value` | Field less than or equal (number) |

### Presence

| `op` | Properties | Description |
|------|-----------|-------------|
| `present` | `field` | Field has a non-empty value |
| `absent` | `field` | Field is missing or empty |
| `truthy` | `field` | Field value is truthy |
| `falsy` | `field` | Field value is falsy |

### Membership

| `op` | Properties | Description |
|------|-----------|-------------|
| `in` | `field`, `values` | Field value is in the list |
| `notIn` | `field`, `values` | Field value is not in the list |

### Conditions

| `op` | Properties | Description |
|------|-----------|-------------|
| `cond` | `condition` | Named condition is truthy |
| `condEq` | `condition`, `value` | Named condition equals value |
| `condIn` | `condition`, `values` | Named condition value is in the list |
| `fieldInCond` | `field`, `condition` | Field value exists in a named array condition |

### Logical combinators

| `op` | Properties | Description |
|------|-----------|-------------|
| `and` | `exprs` | All sub-expressions must be true |
| `or` | `exprs` | At least one sub-expression must be true |
| `not` | `expr` | Negate a sub-expression |

### Check expression

| `op` | Properties | Description |
|------|-----------|-------------|
| `check` | `field`, `check` | Inline validator spec on a field |

## Validator operators

Used in top-level `validators` and inline in `check` rules and expressions.

| `op` | Properties | Description |
|------|-----------|-------------|
| `email` | — | Valid email address |
| `url` | — | Valid URL |
| `integer` | — | Integer value |
| `matches` | `pattern` | Matches regex pattern |
| `minLength` | `value` | Minimum string length |
| `maxLength` | `value` | Maximum string length |
| `min` | `value` | Minimum numeric value |
| `max` | `value` | Maximum numeric value |
| `range` | `min`, `max` | Numeric range (inclusive) |

All validators accept an optional `error` string for the failure message.

## Field definitions

| Property | Type | Description |
|----------|------|-------------|
| `required` | `boolean` | Field must have a value |
| `default` | `string \| number \| boolean \| null` | Default value when absent |
| `isEmpty` | `"string" \| "number" \| "boolean" \| "array" \| "object" \| "present"` | How to determine emptiness |

## Condition definitions

| Property | Type | Description |
|----------|------|-------------|
| `type` | `"boolean" \| "string" \| "number" \| "string[]" \| "number[]"` | Condition value type |
| `description` | `string` | Human-readable description |
