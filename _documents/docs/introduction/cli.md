# Command Line Interface

Turna need a configuration file to run, if not give any configuration it just don't do anything.

```sh
________________
▒▒▒▒▒▒    ▒▒▒▒▒▒ ████    ████  ██████████  ████████████  ████████████
▒▒▒▒▒▒    ▒▒▒▒▒▒ ████    ████  ██████████  ████████████  ████████████
▒▒            ▒▒ ████    ████  ████        ████    ████  ████    ████
▒▒            ▒▒ ████    ████  ████        ████    ████  ████    ████
▒▒▒▒▒▒    ▒▒▒▒▒▒ ████████████  ████        ████    ████  ████████████
▒▒▒▒▒▒____▒▒▒▒▒▒ ████████████  ████        ████    ████  ████    ████

turna extends functionality of services
version:[v0.0.0] commit:[?] buildDate:[]

Usage:
  turna [flags]
  turna [command]

Available Commands:
  api         Trigger api
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command

Flags:
      --config-consul      first config get in consul
      --config-file        first config get in file (default true)
      --config-vault       first config get in vault
  -h, --help               help for turna
  -l, --log-level string   log level (default "info")
  -v, --version            version for turna

Use "turna [command] --help" for more information about a command.
```
