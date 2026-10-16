# file_manager

`file_manager` manages a directory through a browser UI and serves every file in it by path. Files can be anything: JSON, YAML, text, images or binaries. Text files open in an editor with syntax highlighting; other files are previewed or downloaded.

```yaml
server:
  http:
    middlewares:
      files:
        file_manager:
          path: ./data
          prefix_path: /files
          title: Shared files
    routers:
      files:
        path: /files/*
        middlewares:
          - files
```

With this config:

| Request | Result |
| --- | --- |
| `GET /files/` | The file manager UI. |
| `GET /files/raw/configs/app.json` | The file, with `Content-Type: application/json`. |
| `GET /files/raw/configs/app.yaml` | The file, with `Content-Type: application/yaml`. |
| `/files/api/...` | The JSON API the UI uses, see [API](#api). |

The three areas never overlap: a file named `api/info` in the directory is still served at `/files/raw/api/info`.

Raw responses support `ETag`, `If-None-Match`, `Last-Modified` and range requests. Directories are not listed on the raw path.

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `path` | | Directory to manage. Created when missing. Required. |
| `prefix_path` | | Prefix stripped from the request path before lookup. Requests outside it return `404`. |
| `read_only` | `false` | Disable saving, creating, uploading, moving and deleting. |
| `disable_ui` | `false` | Serve only the raw files under `/raw/`; the UI and the API return `404`. |
| `max_upload_size` | `33554432` | Limit for a single save or upload request body, in bytes. |
| `title` | `Files` | Title shown in the UI. |
| `cache_control` | `no-cache` | `Cache-Control` header for raw file responses. |

## UI

- Folder tree with drag and drop: drag a file or folder onto another folder to move it, or drop files and whole folders from your computer to upload them.
- Previews by type: images, video and audio (played with seeking through range requests), PDF in the browser viewer, fonts with a sample text, CSV and TSV as a table, and Markdown with an edit, split or preview view.
- Other binaries (archives, office documents, executables, databases) are recognised by extension and offered as a download.
- Files are not downloaded in full to decide how to show them: the type comes from the extension, unknown files are sniffed from their first 8 KB, and text files over 5 MB open as a download.
- Editor for text files with highlighting for about 60 languages and formats, among them JSON, YAML, TOML, INI and `.env`, XML, Markdown, HTML, CSS, SCSS, JavaScript, TypeScript, Vue, Go, Python, Rust, Java, Kotlin, C, C++, C#, Swift, PHP, Ruby, Lua, SQL, GraphQL, Protocol Buffers, Terraform, shell, PowerShell, Dockerfile, Makefile and Nginx. Files without an extension such as `Dockerfile`, `Makefile` or `.gitignore` are recognised by name. JSON is validated before saving and can be formatted.
- Code is always shown dark, in the Vesper theme used by these docs, whatever the page theme.
- `Ctrl S` / `Cmd S` saves. Saving is guarded by the file's ETag: when someone else saved the file after you opened it, you are asked before overwriting.
- Every file shows its raw URL with copy and `curl` actions.
- Light and dark themes.

## Access control

The middleware has no authentication of its own. Put auth middlewares in front of it. Because raw files live under their own `/raw/` path, one middleware can serve both, with a separate router for public reads and a protected router for everything else:

```yaml
server:
  http:
    middlewares:
      admin_auth:
        basic_auth:
          users:
            - admin:$2y$05$...
      files:
        file_manager:
          path: /srv/files
          prefix_path: /files
    routers:
      files_raw:
        path: /files/raw/*
        middlewares:
          - files
      files_admin:
        path: /files/*
        middlewares:
          - admin_auth
          - files
```

Raw responses carry `Content-Security-Policy: sandbox` (except PDFs) and `X-Content-Type-Options: nosniff`, so an uploaded HTML or SVG file cannot run scripts against the UI's API.

All paths are resolved inside `path`; `..` segments and symlinks pointing outside the directory are rejected.

## API

The UI uses a small JSON API under `{prefix_path}/api/`. Write endpoints return `403` when `read_only` is set.

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `info` | UI settings: title, read-only flag, prefix and raw path. |
| `GET` | `list?path=/dir` | Directory entries, folders first. |
| `GET` | `stat?path=/x` | One entry. |
| `GET` | `file?path=/x[&download]` | File content. |
| `PUT` | `file?path=/x[&create]` | Write the body to the file. `If-Match: <etag>` rejects stale writes with `412`; `create` fails with `409` if it exists. Missing folders are created. |
| `DELETE` | `file?path=/x` | Delete a file or a folder with its contents. |
| `POST` | `mkdir` | Body `{"path": "/dir"}`. |
| `POST` | `move` | Body `{"from": "/a", "to": "/b", "overwrite": false}`. Used for rename too. |
| `POST` | `upload?path=/dir[&overwrite]` | Multipart files; file names may contain folders. |

```sh
curl -X PUT --data-binary @app.yaml 'http://localhost:8080/files/api/file?path=/configs/app.yaml'
```
