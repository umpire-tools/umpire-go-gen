# Integrating umpire-spec into a Language Port

Language ports (Python, Dart, Kotlin, etc.) consume the conformance fixtures from this repo to verify behavioral correctness, using a [mise](https://mise.jdx.dev/) task that fetches the spec at a pinned tag, verifies a SHA-256 checksum, and extracts fixtures to a local `spec/` directory.

## Why mise

- **Cross-language** — the same `spec-sync` task works for Python, Dart, Kotlin, and any future port.
- **Incremental** — `sources`/`outputs` skip the fetch when the pin hasn't changed.
- **Dependency chaining** — `depends = ["spec-sync"]` means `mise run test` auto-syncs before running.

## Canonical snippet

Add this to your port's `mise.toml`:

```toml
[env]
UMPIRE_SPEC_VERSION = "v1.0.0"
UMPIRE_SPEC_SHA256 = "abcd1234…"  # tarball sha256; bump this when updating the version

[tasks.spec-sync]
description = "Sync umpire-spec fixtures into spec/ at the pinned tag"
sources = ["mise.toml"]
outputs = ["spec/.synced-at-version"]
run = """
set -euo pipefail
tarball=$(mktemp -t umpire-spec-XXXXXX.tar.gz)
curl -fsSL "https://github.com/umpire-tools/umpire-spec/archive/refs/tags/${UMPIRE_SPEC_VERSION}.tar.gz" -o "$tarball"
echo "${UMPIRE_SPEC_SHA256}  $tarball" | shasum -a 256 -c -
rm -rf spec/
mkdir -p spec/
tar -xzf "$tarball" -C spec/ --strip-components=1
echo "${UMPIRE_SPEC_VERSION}" > spec/.synced-at-version
rm "$tarball"
"""

[tasks.test]
depends = ["spec-sync"]
run = "uv run pytest"

[tasks.check]
depends = ["spec-sync"]
run = ["uv run ruff check", "uv run mypy", "uv run pytest"]
```

Adapt the `test` and `check` commands to your toolchain:

- **Dart**: `run = "dart test"`
- **Kotlin**: `run = "./gradlew test"`
- **Python**: `run = "uv run pytest"`

## Updating the pin

When a new tag is released:

1. Update `UMPIRE_SPEC_VERSION` to the new tag (e.g. `v1.1.0`).
2. Compute the new tarball SHA-256:
   ```bash
   curl -fsSL "https://github.com/umpire-tools/umpire-spec/archive/refs/tags/v1.1.0.tar.gz" | shasum -a 256
   ```
3. Update `UMPIRE_SPEC_SHA256` with the new hash.

Both values live in the `[env]` block. A pin bump is a two-line diff, reviewable in a PR.

## Anti-patterns

- **Do not use git submodules.** Fresh clones without `--recurse-submodules` produce a working repo with failing tests, and CI requires explicit `submodules: true` configuration everywhere.
- **Do not publish conformance fixtures as a separate package** on PyPI, pub.dev, or Maven Central. Fixtures are test infrastructure — vendored at build-test time and invisible to downstream consumers.
- **Do not fetch from npm.** The TS monorepo is the only consumer that ships fixtures inside its npm package (via git subtree). Language ports fetch from this repo's GitHub releases.

## Fixture consumption

Once synced, your test runner loads `spec/conformance/index.json` and iterates:

```
load spec/conformance/index.json
base_dir = spec/conformance/
for each entry in index.fixtures:
    fixture = parse_json(base_dir / entry.path)
    for each case in fixture.cases:
        result = your_umpire_impl.check(
            schema   = fixture.schema,
            values   = case.values,
            conds    = case.conditions ?? {},
            prev     = case.prev ?? {},
        )
        assert result == case.expectedAvailability, case.id
```

See `spec/conformance/README.md` for the full fixture shape and the `expectedAvailability` map structure.
