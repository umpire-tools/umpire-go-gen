# umpire-gen

Generate typed, zero-dependency Go from `.umpire.json` schemas. The output is a self-contained Go package with structs, enums, and a `Check()` function — no reflection, no `any`, no runtime map lookups.

## Quick Start

### CLI

```bash
go build -o umpire-gen

# Generate from a file
./umpire-gen -i checkout.umpire.json -o ./gen -pkg availability

# Generate from stdin
cat checkout.umpire.json | ./umpire-gen -i - -pkg availability > checkout_umpire.go
```

Flags:

| Flag | Description | Default |
|------|-------------|---------|
| `-i` | Input `.umpire.json` path (use `-` for stdin) | **required** |
| `-o` | Output directory | `.` |
| `-pkg` | Go package name for generated file | **required** |
| `-fields` | Override `Fields` struct name | derived from filename |
| `-conditions` | Override `Conditions` struct name | derived from filename |

### Library

```go
import "github.com/umpire-tools/umpire-gen/pkg/umpiregen"

source, err := umpiregen.Generate(schemaJSON, umpiregen.Config{
    PkgName:    "availability",
    SchemaName: "Checkout",
})
```

## Development

```bash
# Sync conformance fixtures and run all tests
make test

# Just run tests (assumes fixtures are already synced)
go test ./...

# Build the CLI binary
go build .
```

## Project Layout

| Package | Visibility | Purpose |
|---------|-----------|---------|
| `pkg/umpiregen` | **Public** | Single-entry library API (`Generate`) |
| `pkg/codegen` | **Public** | Core codegen engine |
| `pkg/schema` | **Public** | Schema types and validation |
| `internal/cli` | **Internal** | CLI flag parsing and helpers |

## Generated Output

For a schema named `checkout.umpire.json`, the tool emits `checkout_umpire.go` containing:

- `CheckoutFields` — one field per schema field, with inferred Go types
- `CheckoutConditions` — one field per condition
- `CheckoutAvailability` — one `FieldStatus` per field + `Active*Branch` fields for `oneOf`/`eitherOf` groups
- `Check(f, c, prev)` — evaluates all rules and returns the full availability map
- `Challenge(field, f, c, prev)` — debug helper that explains which rules affected a field

## Conformance

The test suite loads fixtures from `spec/conformance/` and asserts generated `Check()` output matches expected availability. The TypeScript `@umpire/json` implementation is the reference runner.

All conformance cases must pass before release.
