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
