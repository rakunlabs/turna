# Replace

This preprocessor replaces a string with another string in a folder paths.

```yaml
preprocess:
  - replace:
      path: "/dist" # path to replace
      skip_files: [] # default: [] files to skip, use glob pattern, included your path like /dist/**/index.html
      skip_dirs: [] # default: [] dirs to skip, it will skip all files in the dir, included your path like /dist/assets
      contents:
      - regex: "" # regex to find, override old if not empty
        old: "" # old string
        new: "" # new string
        value: "" # value from load name, key value and type is map[string]interface{}
```

## Example

In this example, before to serve the folder, replacing all content with the value from load.  
This is useful for frontend applications to adding environment variables for each platform.

```yaml
loads:
  - statics:
    - content:
        content: |
          Turna: XXX2
        name: values

preprocess:
  - replace:
      path: ./testdata/html
      contents:
        value: values

server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      project:
        folder:
          path: ./testdata/html
          browse: false
          spa: false
          index: true
          cache_regex:
            - regex: .*
              cache_control: no-cache
    routers:
      project:
        path: /*
        middlewares:
          - project
```
