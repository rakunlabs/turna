# folder

Serve a folder with options.

```yaml
middlewares:
  test:
    folder:
      path: ./dist # default is empty, string, path to the folder
      index: true # default is false, bool, automatically redirect to index.html
      strip_index_name: true # default is false, bool, strip index name from url
      index_name: index.html # default is index.html, string, the name of the index file
      spa: true # default is false, bool, automatically redirect to index.html if not found
      spa_enable_file: true # default is false, bool, enable .* file to be served to index.html if not found
      spa_index: index.html # default is index_name, string, set the index.html location
      spa_index_regex: # default is not set, pointer, set the index.html location by regex
        regex: ^/testdata/([^/]*)/.*$
        replacement: /$1/index.html # replacement with regex and URL.Path value
      browse: true # default is false, bool, enable directory browsing
      utc: true # default is false, bool, browse time format
      prefix_path: /test # default is empty, string, the base path of internal project code to redirect to correct path
```
