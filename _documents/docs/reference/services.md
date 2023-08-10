# Services

With services you can run multiple applications.

Currently all services run multiple with go routines.

```yaml
services:
  - name: "" # name of the service
    path: "" # path of service, command run inside this path
    command: "" # command to run with args
    env: {} # environment variables, usable with go template (mugo funcs)
    env_values: [] # list of environment variables path from exported config
    inherit_env: false # inherit environment variables
    filters: [] # to filter stdout
    filters_values: [] # list of filters variables path from exported config
```
