# Services

With services you can run multiple applications. Define the order of the services, or define dependencies between them.

```yaml
services:
  - name: "" # name of the service, must be unique.
    path: "" # path of service, command run inside this path
    command: "" # command to run with args
    env: {} # environment variables, usable with go template (mugo funcs)
    env_values: [] # list of environment variables path from exported config
    inherit_env: false # inherit environment variables
    filters: [] # to filter stdout
    filters_values: [] # list of filters variables path from exported config
    order: 0 # order of the service, default is 0, lower is first. If same order set, they will run in parallel. All should be done to continue next order.
    depends: [] # Depends is a list of service names to depend on. Order is ignoring if depend is set
    allow_failure: false # allow failure of the service, default is false
    user: "" # set user, uid:gid or username to run the command. Example: 0 or root:root or 1234:5555
```

## Example

Run multiple command first and start the main program.

```yaml
services:
  - name: echo
    command: echo sss
  - name: fail
    command: /bin/bash -c "exit 2"
    allow_failure: true
  - name: main
    command: echo main
    depends:
      - echo
      - fail
```
