# umpire-spec

The cross-language conformance specification for Umpire — the portable form-logic engine.

This repository provides two artifacts:

- **`umpire.schema.json`** — A JSON Schema (draft-07) that validates `.umpire.json` files. Available at `https://spec.umpire.tools/umpire.schema.json`. Editors can use this to enable autocomplete and inline validation without a Node.js toolchain.
- **`conformance/`** — Plain JSON fixtures. Any Umpire runtime (TypeScript, Python, Dart, Kotlin, etc.) can consume these to verify behavioral correctness against the reference implementation.

## Quick Start

```bash
bun install
bun run validate
```

This runs AJV against `umpire.schema.json`. It verifies that every conformance fixture's inner `schema` block is structurally valid and ensures that intentionally malformed documents are rejected.

## Conformance Fixtures

Read [`conformance/README.md`](conformance/README.md) to understand the fixture shape and how to write a port runner. 

To implement a runner:
1. Load `conformance/index.json`.
2. For each fixture in `index.fixtures`, parse the file, extract `fixture.schema`, and execute it using your Umpire implementation against each test `case`.
3. Assert that your output matches `case.expectedAvailability`.

All fixture paths in `index.json` are relative to the `conformance/` directory.

## Adding to Editors

To enable autocomplete for `.umpire.json` files:

- **VS Code**: Add this to your `.vscode/settings.json`:
  ```json
  {
    "json.schemas": [
      {
        "fileMatch": ["*.umpire.json"],
        "url": "https://spec.umpire.tools/umpire.schema.json"
      }
    ]
  }
  ```
- **SchemaStore**: Submission to the [SchemaStore](https://www.schemastore.org/) catalog is on the roadmap. The schema uses draft-07, which is the version SchemaStore recommends.

## Versioning

This repository uses semver tags (e.g., `v1.0.0`). Bump the tag whenever the schema or the fixture contract changes in a way that requires port updates.

## License

See [LICENSE](LICENSE).
