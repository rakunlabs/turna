# replace

The `replace` preprocess module walks a directory and rewrites files before Turna starts the server and services.

```yaml
preprocess:
  - replace:
      path: ./dist
      skip_dirs:
        - ./dist/assets
      contents:
        - old: __API_URL__
          new: https://api.example.com
        - regex: "version: .*"
          new: "version: 2026.05"
```

## Fields

| Field | Description |
| --- | --- |
| `path` | Root directory to walk. |
| `skip_dirs` | Exact directory paths to skip. |
| `skip_files` | File paths recognized by the walker. Prefer narrowing `path` or using `skip_dirs` for reliable exclusion. |
| `contents` | Replacement rules applied to each visited file. |

Replacement rule fields:

| Field | Description |
| --- | --- |
| `regex` | Regular expression to replace. When set, it overrides `old`. |
| `old` | Literal string to replace. |
| `old_template` | Render `old` as a Turna template before replacement. |
| `new` | Replacement string. |
| `new_template` | Render `new` as a Turna template before replacement. |
| `value` | Name of a loaded `map[string]any`. Each map key is replaced by its value. |

## Loaded Values Example

```yaml
loads:
  - name: frontend_env
    statics:
      - content:
          name: frontend_env
          content: |
            __API_URL__: https://api.example.com
            __APP_NAME__: turna

preprocess:
  - replace:
      path: ./dist
      contents:
        - value: frontend_env
```

This is useful for frontend builds that contain placeholder strings and need platform-specific values at runtime.
