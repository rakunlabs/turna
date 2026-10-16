# file_manager test data

Sample files for trying the `file_manager` middleware locally:

```sh
make run CONFIG_FILE=testdata/config/file-manager.yml
```

Then open <http://localhost:8080/files/>.

## What is here

| Folder | Shows |
| --- | --- |
| `configs/` | JSON, YAML, TOML and `.env` files in the editor |
| `code/` | Highlighting for Go, Python, Rust, shell and a Dockerfile |
| `data/` | CSV rendered as a table, with quoted and multi-line cells |
| `flags/`, `schemas/` | JSON validation and formatting |
| `media/` | Video, audio and PDF previews |
| `assets/` | Image and font previews, and a binary file |

## Raw access

Every file is also served by path under `/files/raw/`:

- `GET /files/raw/configs/prod/gateway.json`
- `GET /files/raw/media/sample.webm` supports range requests, so players can seek.

> Changes made in the UI are written to this folder. Run `git checkout testdata/filemanager` to reset.
