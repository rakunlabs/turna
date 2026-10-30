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

Raw responses support `ETag`, `If-None-Match`, `If-Range`, `Last-Modified` and range requests. Directories are not listed on the raw path.

## ETag

By default the ETag is weak (`W/"…"`) and built from the modification time and size, which costs nothing. Tags are compared weakly, so `W/"x"` and `"x"` match; a compress middleware in front that weakens strong tags does not break conditional requests or saves.

| Mode | ETag | Cost | Range resume (`If-Range`) |
| --- | --- | --- | --- |
| `weak` | `W/"<mtime>-<size>"` | none | no, the full file is sent again |
| `strong` | `"<mtime>-<size>"` | none | yes |
| `hash` | `"<sha256>"` | first request after a change reads the whole file | yes |

`strong` is safe because every write goes through a rename, so the modification time changes even when the size does not. `hash` only changes when the bytes do, so a file rewritten with the same content stays cached; hashes are kept in memory per path.

Use `etag_rules` to pick a mode per file. Patterns are [doublestar](https://github.com/bmatcuk/doublestar#patterns) globs on the path inside `path`, without a leading slash (one is stripped if given): `*` stays within a folder, `**` crosses folders, `{a,b}` lists alternatives. Rules are checked in order, the first match wins, and files matching none use `etag`. An unknown mode or a malformed pattern stops the server from starting:

```yaml
file_manager:
  path: /srv/files
  etag_rules:
    - pattern: "videos/**"
      etag: strong
    - pattern: "**/*.{mp4,webm,mkv,iso}"
      etag: strong
    - pattern: "configs/**/*.json"
      etag: hash
```

The file details panel shows the mode and current ETag of each file.

Choosing a mode:

- Leave the default for configs and small files; edits are protected the same way in every mode.
- Use `strong` for large media, disk images and anything downloaded in parts, so interrupted downloads resume.
- Use `hash` when the same content is rewritten often (generated configs, CI artefacts) and clients should keep their cache. Avoid it for very large files: the first request after each change waits for the whole file to be read.

### How video playback uses ranges

The browser never downloads a video in full before playing. It sends `Range: bytes=0-` first, the server answers `206 Partial Content`, and the browser reads the header (duration, keyframe index) and starts playing as data arrives. Seeking to minute 23 makes the browser look up that time in the index and send a new request such as `Range: bytes=812345678-`. Each request is answered by `http.ServeContent` with only those bytes.

The ETag matters when a range request depends on an earlier one: a resumed download or a player reusing cached bytes sends `If-Range: <etag>` to say "send the rest only if the file is still the one I started". By the HTTP spec this needs a strong tag. With a weak tag the server must ignore the range and send the whole file with `200`, which for a large video means the download restarts from zero. Plain seeking in the UI player works in every mode; `strong` makes resumes and cached partial reads work too.

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
| `etag` | `weak` | `weak`, `strong` or `hash`, see [ETag](#etag). |
| `etag_rules` | | List of `{pattern, etag}`; picks the mode per file, first match wins. |

## UI

- Folder tree with drag and drop: drag a file or folder onto another folder to move it, or drop files and whole folders from your computer to upload them.
- Previews by type: images, video and audio (played with seeking through range requests), PDF in the browser viewer, fonts with a sample text, CSV and TSV as a table, and Markdown with an edit, split or preview view.
- Other binaries (archives, office documents, executables, databases) are recognised by extension and offered as a download.
- Files are not downloaded in full to decide how to show them: the type comes from the extension, unknown files are sniffed from their first 8 KB, and text files over 5 MB open as a download.
- Editor for text files with highlighting for about 60 languages and formats, among them JSON, YAML, TOML, INI and `.env`, XML, Markdown, HTML, CSS, SCSS, JavaScript, TypeScript, Vue, Go, Python, Rust, Java, Kotlin, C, C++, C#, Swift, PHP, Ruby, Lua, SQL, GraphQL, Protocol Buffers, Terraform, shell, PowerShell, Dockerfile, Makefile and Nginx. Files without an extension such as `Dockerfile`, `Makefile` or `.gitignore` are recognised by name.
- JSON and YAML are checked as you type: problems are underlined in place, marked in the gutter and counted in the status bar (click it to jump to the first one, `Ctrl Shift M` lists all). YAML also reports duplicate keys and keeps multi-document files apart; custom tags such as CloudFormation's `!Ref` or Ansible's `!vault` are accepted. Saving a file with errors asks first. `.jsonc` and `.json5` are highlighted but not checked, since they allow comments.
- **Format** (or `Shift Alt F`) rewrites JSON and YAML with two-space indentation without changing any value: numbers keep their exact digits (big IDs, `1.0`, `010`), JSON keeps duplicate keys, and YAML keeps comments, anchors, tags and document separators. Long lines are not wrapped. Formatting is a normal edit, so `Ctrl Z` undoes it.
- Code is always shown dark, in the Vesper theme used by these docs, whatever the page theme.
- `Ctrl S` / `Cmd S` saves. Saving is guarded by the file's ETag: when someone else saved the file after you opened it, you are asked before overwriting. Typing while a save is in flight stays marked as unsaved.
- The editor never resets while you work. Saving, reloading or changes in other folders keep the cursor, scroll, folds and undo history. When the open file changes on disk and you have no unsaved edits, the new text is swapped in place; with unsaved edits it is left alone and the next save asks before overwriting. **Discard** also swaps the text in place, so `Ctrl Z` can bring the edits back.
- Video, audio, images and PDFs are only reloaded when the file's ETag changes, so a playing video is not restarted by unrelated changes.
- Every file shows its raw URL with copy and `curl` actions, and its ETag mode and value in the details panel.
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
| `GET` | `stat?path=/x` | One entry. For files it also has `etag` and `etag_mode`, and the `ETag` header. |
| `GET` | `file?path=/x[&download]` | File content. |
| `PUT` | `file?path=/x[&create]` | Write the body to the file. `If-Match: <etag>` (a list or `*` also works) rejects stale writes with `412`; `If-None-Match: *` writes only when the file does not exist yet; `create` fails with `409` if it exists. Missing folders are created. |
| `DELETE` | `file?path=/x` | Delete a file or a folder with its contents. |
| `POST` | `mkdir` | Body `{"path": "/dir"}`. |
| `POST` | `move` | Body `{"from": "/a", "to": "/b", "overwrite": false}`. Used for rename too. |
| `POST` | `upload?path=/dir[&overwrite]` | Multipart files; file names may contain folders. |

```sh
curl -X PUT --data-binary @app.yaml 'http://localhost:8080/files/api/file?path=/configs/app.yaml'
```
