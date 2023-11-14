# folder

Serve a folder with options.

```yaml
middlewares:
  test:
    folder:
      path: ./dist # default is empty, string, path to the folder
      index: false # default is false, bool, automatically redirect to index.html
      strip_index_name: false # default is false, bool, strip index name from url
      index_name: index.html # default is index.html, string, the name of the index file
      spa: false # default is false, bool, automatically redirect to index.html if not found
      spa_enable_file: false # default is false, bool, enable .* file to be served to index.html if not found
      spa_index: index.html # default is index_name, string, set the index.html location
      spa_index_regex: # default is not set, pointer, set the index.html location by regex
        - regex: ^/testdata/([^/]*)/.*$
          replacement: /$1/index.html # replacement with regex and URL.Path value
      browse: false # default is false, bool, enable directory browsing
      utc: false # default is false, bool, browse time format
      prefix_path: /test # default is empty, string, the base path of internal project code to redirect to correct path
      file_path_regex: # default is not set, pointer, set the file path by regex, comes after prefix_path apply, file path doesn't include / suffix
        - regex: "^/docs/([^/]*)/?(.*)$"
          replacement: "/$1/latest/$2"
      cache_regex: # default is not set, pointer, set the cache control by regex
        - regex: .*
          cache_control: no-cache
      browse_cache: no-cache # default is no-cache, string, set the cache control for browse page
```

Example:

```yaml
server:
  entrypoints:
    http:
      address: ":8080"
  http:
    middlewares:
      project:
        folder:
          # path: ../../testdata/serve
          path: ./testdata/serve
          browse: false
          spa: true
          index: true
          cache_regex:
            - regex: .*
              cache_control: no-cache
          spa_index_regex:
            - regex: "^/docs/([^/]*)/.*$"
              replacement: "/$1/index.html"
          file_path_regex:
            - regex: "^/docs/([^/]*)/?(.*)$"
              replacement: "/$1/latest/$2"
    routers:
      project:
        path: /docs/*
        middlewares:
          - project
```
