---
layout: home
hero:
  name: Umpire
  text: Cross-language conformance
  tagline: JSON Schema + fixtures for portable validation rules
  image:
    src: /umpire-mark.svg
    alt: Umpire
  actions:
    - theme: brand
      text: Schema Reference
      link: /schema
    - theme: brand
      text: JSON Schema Profile
      link: /profiles
    - theme: alt
      text: Integrating a Port
      link: /integrating
---

# Umpire JSON Schema

The canonical JSON Schema (draft-07) for `.umpire.json` files.
Available at [`https://spec.umpire.tools/umpire.schema.json`](https://spec.umpire.tools/umpire.schema.json).

Editors that integrate with SchemaStore pick this up automatically for files named `*.umpire.json`.
You can also configure it manually:

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

## JSON Schema Composition Profile

The [JSON Schema Composition Profile](/profiles) wraps Umpire field availability and
JSON Schema structural validation into a single portable document.

Profile v1 schema:

```
https://spec.umpire.tools/profiles/json-schema/v1/profile.schema.json
```

The profile is versioned independently from the base Umpire schema.
