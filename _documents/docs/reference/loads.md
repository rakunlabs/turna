# Loads

Loads can download resources from various sources and save them to a file or memory to use in other areas in configuration.

It is an array of objects, each object has a export key and a list of sources.  
Sources are divided into two groups, static and dynamic.  

Static sources are loaded once at startup.
Dynamic sources are loaded every time the configuration is updated.

If not set ant `export` key, it will not exported to file, just holds in memory.

```yaml
loads:
  - export: test.yml
    # file_perm: "0644" # default
    # folder_perm: "0755" # default
    # name using to export value in map
    name: mytest
    # static configuration merged
    statics: [] # check static section to see available sources
    dynamics: [] # check dynamic section to see available sources
```

## Static

Static sources are loaded once at startup.

Currently supported sources are `consul`, `vault`, `file` and `content`.

### Consul

Using `github.com/hashicorp/consul/api` when loading. 

```yaml
consul:
  name: myconsul # name using to export value in map
  path: test # you should set one path
  path_prefix: finops # it can usable as folder name, default is empty
  raw: false # raw load to without using any codec, don't mix with others merge not possible, default is false
  codec: "YAML" # default is YAML, [toml, yaml, json] supported
  inner_path: "test" # get the inner path, it is useful for getting a specific key with sperate '/', default is empty
  map: "myapp/inner" # remap key to put that values inside a key, default is empty
  template: false # run go-template (mugo functions) on the value, default is false
  base64: false # decode base64, default is false
```

### Vault

Using `github.com/hashicorp/vault/api` when loading.

```yaml
vault:
  name: myvault # name using to export value in map
  path: test/myapp # you should set one path
  path_prefix: secret # path_prefix is must, default is empty,
  app_role_base_path: auth/approle/login # default is auth/approle/login, not need to set
  inner_path: "test" # get the inner path, it is useful for getting a specific key with sperate '/', default is empty
  map: "myapp/inner" # remap key to put that values inside a key, default is empty
  template: false # run go-template (mugo functions) on the value, default is false
  base64: false # decode base64, default is false
```

### File

Load any file from the filesystem.

```yaml
file:
  name: myfile # name using to export value in map
  path: load.yml # default is empty, [toml, yml, yaml, json] supported
  raw: false # raw load to without using any codec, don't mix with others merge not possible, default is false
  inner_path: "test" # get the inner path, it is useful for getting a specific key with sperate '/', default is empty
  map: "myapp/inner" # remap key to put that values inside a key, default is empty
  template: false # run go-template (mugo functions) on the value, default is false
  base64: false # decode base64, default is false
```

### Content

Load content from configuration.

```yaml
content:
  name: mycontent # name using to export value in map
  codec: "YAML" # default is YAML, [toml, yaml, json] supported
  content: |
    test:
      test: 1
      test2: 2
  raw: false
  template: false
  inner_path: "" 
  map: "" # remap key
  base64: false # decode base64
```

## Dynamic

Dynamic sources are loaded every time the configuration is updated.  
It can mix with static sources, in that time it hold static sources in memory and just update dynamic sources.

Currently supported sources is `consul`.

### Consul

```yaml
consul:
  name: myconsulDynamic # name using to export value in map
  path: test
  path_prefix: finops # default is empty
  raw: false
  codec: "YAML" # default is YAML, [toml, yaml, json] supported
  inner_path: ""
  map: ""
  template: false
```
