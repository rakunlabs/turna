# Preprocess

Preprocessors run after `loads` and before the server or services start. Use them to prepare files that applications or static file servers need at runtime.

```yaml
preprocess:
  - replace:
      path: ./dist
      contents: []
```

## Modules

| Module | Description |
| --- | --- |
| [`replace`](./modules/replace) | Rewrites files under a path using strings, regular expressions, templates, or loaded values. |

## Execution Notes

Preprocess modules run once during startup. They can read data from `loads` through Turna's template memory, so define `loads` before `preprocess` when replacement values come from external configuration.
