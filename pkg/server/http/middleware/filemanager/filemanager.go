package filemanager

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/folder"
)

//go:embed _ui/dist/*
var uiFS embed.FS

const (
	rawPath              = "/raw"
	apiPath              = "/api"
	defaultMaxUploadSize = 32 << 20
)

// extraMimeTypes fills gaps in the system MIME table so clients get a stable
// Content-Type whatever host the binary runs on.
var extraMimeTypes = map[string]string{
	// data and config
	".yaml":       "application/yaml",
	".yml":        "application/yaml",
	".json":       "application/json",
	".jsonc":      "application/json",
	".json5":      "application/json5",
	".map":        "application/json",
	".geojson":    "application/geo+json",
	".jsonld":     "application/ld+json",
	".ndjson":     "application/x-ndjson",
	".toml":       "application/toml",
	".xml":        "application/xml",
	".csv":        "text/csv; charset=utf-8",
	".tsv":        "text/tab-separated-values; charset=utf-8",
	".proto":      "text/plain; charset=utf-8",
	".graphql":    "application/graphql",
	".gql":        "application/graphql",
	".sql":        "application/sql",
	".hcl":        "text/plain; charset=utf-8",
	".tf":         "text/plain; charset=utf-8",
	".tfvars":     "text/plain; charset=utf-8",
	".properties": "text/plain; charset=utf-8",
	".env":        "text/plain; charset=utf-8",
	".ini":        "text/plain; charset=utf-8",
	".cfg":        "text/plain; charset=utf-8",
	".conf":       "text/plain; charset=utf-8",

	// text and docs
	".md":       "text/markdown; charset=utf-8",
	".markdown": "text/markdown; charset=utf-8",
	".txt":      "text/plain; charset=utf-8",
	".log":      "text/plain; charset=utf-8",
	".diff":     "text/x-diff; charset=utf-8",
	".patch":    "text/x-diff; charset=utf-8",
	".tex":      "application/x-tex",
	".rst":      "text/x-rst; charset=utf-8",
	".adoc":     "text/asciidoc; charset=utf-8",

	// source code, served as text so browsers show instead of download
	".sh":    "text/x-shellscript; charset=utf-8",
	".bash":  "text/x-shellscript; charset=utf-8",
	".zsh":   "text/x-shellscript; charset=utf-8",
	".ps1":   "text/plain; charset=utf-8",
	".go":    "text/x-go; charset=utf-8",
	".py":    "text/x-python; charset=utf-8",
	".rs":    "text/x-rust; charset=utf-8",
	".java":  "text/x-java; charset=utf-8",
	".kt":    "text/x-kotlin; charset=utf-8",
	".c":     "text/x-c; charset=utf-8",
	".h":     "text/x-c; charset=utf-8",
	".cpp":   "text/x-c++; charset=utf-8",
	".hpp":   "text/x-c++; charset=utf-8",
	".cs":    "text/x-csharp; charset=utf-8",
	".rb":    "text/x-ruby; charset=utf-8",
	".php":   "text/x-php; charset=utf-8",
	".lua":   "text/x-lua; charset=utf-8",
	".swift": "text/x-swift; charset=utf-8",
	".ts":    "text/typescript; charset=utf-8",
	".tsx":   "text/typescript; charset=utf-8",
	".jsx":   "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".vue":   "text/plain; charset=utf-8",

	// media
	".webp":  "image/webp",
	".avif":  "image/avif",
	".apng":  "image/apng",
	".ico":   "image/x-icon",
	".heic":  "image/heic",
	".mp4":   "video/mp4",
	".m4v":   "video/mp4",
	".webm":  "video/webm",
	".mov":   "video/quicktime",
	".mkv":   "video/x-matroska",
	".ogv":   "video/ogg",
	".mp3":   "audio/mpeg",
	".wav":   "audio/wav",
	".ogg":   "audio/ogg",
	".oga":   "audio/ogg",
	".opus":  "audio/ogg",
	".m4a":   "audio/mp4",
	".aac":   "audio/aac",
	".flac":  "audio/flac",
	".weba":  "audio/webm",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".pdf":   "application/pdf",
	".wasm":  "application/wasm",
	".zip":   "application/zip",
	".gz":    "application/gzip",
	".tgz":   "application/gzip",
	".zst":   "application/zstd",
}

// FileManager serves files of a directory by path and ships a UI to browse,
// edit, upload, move and delete them.
type FileManager struct {
	// Path is the directory to manage.
	Path string `cfg:"path"`
	// PrefixPath is stripped from the request path before lookup.
	PrefixPath string `cfg:"prefix_path"`
	// ReadOnly disables every write operation.
	ReadOnly bool `cfg:"read_only"`
	// DisableUI serves only the raw files under /raw, without UI and API.
	DisableUI bool `cfg:"disable_ui"`
	// MaxUploadSize limits a write or upload request body in bytes. Default is 32MB.
	MaxUploadSize int64 `cfg:"max_upload_size"`
	// Title is shown in the UI header.
	Title string `cfg:"title"`
	// CacheControl is set on raw file responses. Default is no-cache.
	CacheControl string `cfg:"cache_control"`

	root *os.Root
	ui   http.Handler
}

type entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Dir     bool      `json:"dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	Type    string    `json:"type,omitempty"`
}

type apiError struct {
	Error string `json:"error"`
}

type infoResponse struct {
	Title    string `json:"title"`
	ReadOnly bool   `json:"read_only"`
	Prefix   string `json:"prefix"`
	RawPath  string `json:"raw_path"`
	MaxSize  int64  `json:"max_upload_size"`
}

type moveRequest struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Overwrite bool   `json:"overwrite"`
}

type pathRequest struct {
	Path string `json:"path"`
}

func (m *FileManager) Middleware() (func(http.Handler) http.Handler, error) {
	if m.Path == "" {
		return nil, errors.New("file_manager: path is required")
	}

	if err := os.MkdirAll(m.Path, 0o755); err != nil {
		return nil, fmt.Errorf("file_manager: create path: %w", err)
	}

	root, err := os.OpenRoot(m.Path)
	if err != nil {
		return nil, fmt.Errorf("file_manager: open path: %w", err)
	}

	m.root = root

	m.PrefixPath = "/" + strings.Trim(m.PrefixPath, "/")
	if m.PrefixPath == "/" {
		m.PrefixPath = ""
	}

	if m.MaxUploadSize <= 0 {
		m.MaxUploadSize = defaultMaxUploadSize
	}

	if m.CacheControl == "" {
		m.CacheControl = "no-cache"
	}

	if !m.DisableUI {
		sub, err := fs.Sub(uiFS, "_ui/dist")
		if err != nil {
			return nil, err
		}

		f := folder.Folder{
			Index: true,
			CacheRegex: []*folder.RegexCacheStore{
				{Regex: `index\.html$`, CacheControl: "no-store"},
				{Regex: `.*`, CacheControl: "public, max-age=259200"},
			},
		}
		f.SetFs(http.FS(sub))

		uiM, err := f.Middleware()
		if err != nil {
			return nil, err
		}

		m.ui = uiM(nil)
	}

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(m.serveHTTP)
	}, nil
}

func (m *FileManager) serveHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if m.PrefixPath != "" {
		if p != m.PrefixPath && !strings.HasPrefix(p, m.PrefixPath+"/") {
			http.NotFound(w, r)

			return
		}

		p = strings.TrimPrefix(p, m.PrefixPath)
	}

	if rest, ok := strings.CutPrefix(p, rawPath+"/"); ok {
		m.serveRaw(w, r, rest)

		return
	}

	if m.DisableUI {
		http.NotFound(w, r)

		return
	}

	if p == "" {
		http.Redirect(w, r, m.PrefixPath+"/", http.StatusTemporaryRedirect)

		return
	}

	if rest, ok := strings.CutPrefix(p, apiPath+"/"); ok {
		m.serveAPI(w, r, rest)

		return
	}

	r2 := r.Clone(r.Context())
	r2.URL.Path = p
	m.ui.ServeHTTP(w, r2)
}

// rel converts a user path into a name usable with os.Root.
func rel(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean("/" + p)

	if p == "/" {
		return "."
	}

	return strings.TrimPrefix(p, "/")
}

func toPublic(name string) string {
	if name == "." {
		return "/"
	}

	return "/" + filepath.ToSlash(name)
}

func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if v, ok := extraMimeTypes[ext]; ok {
		return v
	}

	if v := mime.TypeByExtension(ext); v != "" {
		return v
	}

	return ""
}

func etag(info fs.FileInfo) string {
	return `W/"` + strconv.FormatInt(info.ModTime().UnixNano(), 36) + "-" + strconv.FormatInt(info.Size(), 36) + `"`
}

func (m *FileManager) serveRaw(w http.ResponseWriter, r *http.Request, p string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

		return
	}

	m.serveFile(w, r, rel(p), false)
}

func (m *FileManager) serveFile(w http.ResponseWriter, r *http.Request, name string, download bool) {
	f, err := m.root.Open(name)
	if err != nil {
		writeFSError(w, err, false)

		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		writeFSError(w, err, false)

		return
	}

	if info.IsDir() {
		writeFSError(w, fs.ErrNotExist, false)

		return
	}

	if ct := contentType(name); ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	// Served files share the UI origin; keep uploaded HTML or SVG from running
	// scripts against the API. Browsers refuse to render PDFs in a sandbox.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if strings.ToLower(path.Ext(name)) != ".pdf" {
		w.Header().Set("Content-Security-Policy", "sandbox")
	}

	if download {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(name)}))
	}

	w.Header().Set("Cache-Control", m.CacheControl)
	w.Header().Set("ETag", etag(info))

	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func (m *FileManager) serveAPI(w http.ResponseWriter, r *http.Request, api string) {
	w.Header().Set("Cache-Control", "no-store")

	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	if write && m.ReadOnly {
		writeJSON(w, http.StatusForbidden, apiError{Error: "file manager is read-only"})

		return
	}

	switch api {
	case "info":
		writeJSON(w, http.StatusOK, infoResponse{
			Title:    m.Title,
			ReadOnly: m.ReadOnly,
			Prefix:   m.PrefixPath,
			RawPath:  rawPath,
			MaxSize:  m.MaxUploadSize,
		})
	case "list":
		m.apiList(w, r)
	case "stat":
		m.apiStat(w, r)
	case "file":
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			m.serveFile(w, r, rel(r.URL.Query().Get("path")), r.URL.Query().Has("download"))
		case http.MethodPut:
			m.apiWrite(w, r)
		case http.MethodDelete:
			m.apiDelete(w, r)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, apiError{Error: "method not allowed"})
		}
	case "mkdir":
		m.apiMkdir(w, r)
	case "move":
		m.apiMove(w, r)
	case "upload":
		m.apiUpload(w, r)
	default:
		writeJSON(w, http.StatusNotFound, apiError{Error: "unknown endpoint"})
	}
}

func (m *FileManager) apiStat(w http.ResponseWriter, r *http.Request) {
	name := rel(r.URL.Query().Get("path"))

	info, err := m.root.Stat(name)
	if err != nil {
		writeFSError(w, err, true)

		return
	}

	e := entry{
		Name:    info.Name(),
		Path:    toPublic(name),
		Dir:     info.IsDir(),
		ModTime: info.ModTime(),
	}

	if name == "." {
		e.Name = ""
	}

	if !e.Dir {
		e.Size = info.Size()
		e.Type = contentType(name)
		w.Header().Set("ETag", etag(info))
	}

	writeJSON(w, http.StatusOK, e)
}

func (m *FileManager) apiList(w http.ResponseWriter, r *http.Request) {
	name := rel(r.URL.Query().Get("path"))

	info, err := m.root.Stat(name)
	if err != nil {
		writeFSError(w, err, true)

		return
	}

	if !info.IsDir() {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "not a directory"})

		return
	}

	f, err := m.root.Open(name)
	if err != nil {
		writeFSError(w, err, true)

		return
	}
	defer func() { _ = f.Close() }()

	dirEntries, err := f.ReadDir(-1)
	if err != nil {
		writeFSError(w, err, true)

		return
	}

	entries := make([]entry, 0, len(dirEntries))
	for _, d := range dirEntries {
		info, err := d.Info()
		if err != nil {
			continue
		}

		childName := path.Join(filepath.ToSlash(name), d.Name())

		isDir := info.IsDir()
		if info.Mode()&fs.ModeSymlink != 0 {
			// follow the link only when it stays inside the root
			if target, err := m.root.Stat(childName); err == nil {
				info = target
				isDir = target.IsDir()
			}
		}

		e := entry{
			Name:    d.Name(),
			Path:    toPublic(childName),
			Dir:     isDir,
			ModTime: info.ModTime(),
		}

		if !isDir {
			e.Size = info.Size()
			e.Type = contentType(d.Name())
		}

		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}

		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"path":    toPublic(name),
		"entries": entries,
	})
}

func (m *FileManager) apiWrite(w http.ResponseWriter, r *http.Request) {
	name := rel(r.URL.Query().Get("path"))
	if name == "." {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "path is required"})

		return
	}

	info, err := m.root.Stat(name)
	switch {
	case err == nil:
		if info.IsDir() {
			writeJSON(w, http.StatusConflict, apiError{Error: "a folder exists at this path"})

			return
		}

		if r.URL.Query().Has("create") {
			writeJSON(w, http.StatusConflict, apiError{Error: "file already exists"})

			return
		}

		if match := r.Header.Get("If-Match"); match != "" && match != etag(info) {
			writeJSON(w, http.StatusPreconditionFailed, apiError{Error: "file changed since it was opened"})

			return
		}
	case errors.Is(err, fs.ErrNotExist):
		if r.Header.Get("If-Match") != "" {
			writeJSON(w, http.StatusPreconditionFailed, apiError{Error: "file no longer exists"})

			return
		}
	default:
		writeFSError(w, err, true)

		return
	}

	if err := m.writeFile(name, http.MaxBytesReader(w, r.Body, m.MaxUploadSize)); err != nil {
		writeFSError(w, err, true)

		return
	}

	info, err = m.root.Stat(name)
	if err != nil {
		writeFSError(w, err, true)

		return
	}

	w.Header().Set("ETag", etag(info))
	writeJSON(w, http.StatusOK, entry{
		Name:    info.Name(),
		Path:    toPublic(name),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Type:    contentType(name),
	})
}

// writeFile writes through a temporary file so readers never see a partial file.
func (m *FileManager) writeFile(name string, body io.Reader) error {
	dir := path.Dir(name)
	if err := m.root.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp := path.Join(dir, "."+path.Base(name)+".tmp-"+strconv.FormatInt(time.Now().UnixNano(), 36))

	f, err := m.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		_ = m.root.Remove(tmp)

		return err
	}

	if err := f.Close(); err != nil {
		_ = m.root.Remove(tmp)

		return err
	}

	if info, err := m.root.Stat(name); err == nil {
		_ = m.root.Chmod(tmp, info.Mode().Perm())
	}

	if err := m.root.Rename(tmp, name); err != nil {
		_ = m.root.Remove(tmp)

		return err
	}

	return nil
}

func (m *FileManager) apiDelete(w http.ResponseWriter, r *http.Request) {
	name := rel(r.URL.Query().Get("path"))
	if name == "." {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "cannot delete the root folder"})

		return
	}

	if _, err := m.root.Lstat(name); err != nil {
		writeFSError(w, err, true)

		return
	}

	if err := m.root.RemoveAll(name); err != nil {
		writeFSError(w, err, true)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (m *FileManager) apiMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, apiError{Error: "method not allowed"})

		return
	}

	var req pathRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid body"})

		return
	}

	name := rel(req.Path)
	if name == "." {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "path is required"})

		return
	}

	if _, err := m.root.Lstat(name); err == nil {
		writeJSON(w, http.StatusConflict, apiError{Error: "already exists"})

		return
	}

	if err := m.root.MkdirAll(name, 0o755); err != nil {
		writeFSError(w, err, true)

		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"path": toPublic(name)})
}

func (m *FileManager) apiMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, apiError{Error: "method not allowed"})

		return
	}

	var req moveRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid body"})

		return
	}

	from, to := rel(req.From), rel(req.To)
	if from == "." || to == "." {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "cannot move the root folder"})

		return
	}

	if from == to {
		writeJSON(w, http.StatusOK, map[string]string{"path": toPublic(to)})

		return
	}

	if strings.HasPrefix(to+"/", from+"/") {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "cannot move a folder into itself"})

		return
	}

	fromInfo, err := m.root.Lstat(from)
	if err != nil {
		writeFSError(w, err, true)

		return
	}

	if toInfo, err := m.root.Lstat(to); err == nil {
		if !req.Overwrite || toInfo.IsDir() || fromInfo.IsDir() {
			writeJSON(w, http.StatusConflict, apiError{Error: "destination already exists"})

			return
		}
	}

	if err := m.root.MkdirAll(path.Dir(to), 0o755); err != nil {
		writeFSError(w, err, true)

		return
	}

	if err := m.root.Rename(from, to); err != nil {
		writeFSError(w, err, true)

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"path": toPublic(to)})
}

func (m *FileManager) apiUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, apiError{Error: "method not allowed"})

		return
	}

	dir := rel(r.URL.Query().Get("path"))
	overwrite := r.URL.Query().Has("overwrite")

	r.Body = http.MaxBytesReader(w, r.Body, m.MaxUploadSize)

	reader, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "expected multipart form"})

		return
	}

	var saved []string

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			writeFSError(w, err, true)

			return
		}

		fileName := rawFileName(part)
		if fileName == "" {
			_ = part.Close()

			continue
		}

		// browsers send the relative path for folder uploads
		name := rel(path.Join(filepath.ToSlash(dir), fileName))
		if name == "." {
			_ = part.Close()

			continue
		}

		if _, err := m.root.Lstat(name); err == nil && !overwrite {
			_ = part.Close()
			writeJSON(w, http.StatusConflict, apiError{Error: toPublic(name) + " already exists"})

			return
		}

		err = m.writeFile(name, part)
		_ = part.Close()

		if err != nil {
			writeFSError(w, err, true)

			return
		}

		saved = append(saved, toPublic(name))
	}

	writeJSON(w, http.StatusOK, map[string]any{"saved": saved})
}

// rawFileName keeps the relative folder path sent for folder uploads;
// Part.FileName strips it to the base name.
func rawFileName(part *multipart.Part) string {
	_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil {
		return ""
	}

	return params["filename"]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeFSError(w http.ResponseWriter, err error, asJSON bool) {
	status := http.StatusInternalServerError
	msg := "internal error"

	var maxErr *http.MaxBytesError

	switch {
	case errors.Is(err, fs.ErrNotExist):
		status, msg = http.StatusNotFound, "not found"
	case errors.Is(err, fs.ErrPermission):
		status, msg = http.StatusForbidden, "permission denied"
	case errors.Is(err, fs.ErrExist):
		status, msg = http.StatusConflict, "already exists"
	case errors.As(err, &maxErr):
		status, msg = http.StatusRequestEntityTooLarge, "request body too large"
	case strings.Contains(err.Error(), "path escapes"):
		status, msg = http.StatusBadRequest, "path escapes the managed folder"
	}

	if !asJSON {
		http.Error(w, msg, status)

		return
	}

	writeJSON(w, status, apiError{Error: msg})
}
