package folder

import (
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rakunlabs/turna/pkg/render"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

const indexPage = "/index.html"

type Folder struct {
	// BasePath for better UI browser expriance and not show .. if already in base path
	BasePath string `cfg:"base_path"`
	// Path is the path to the folder
	Path string `cfg:"path"`
	// Index is automatically redirect to index.html
	Index bool `cfg:"index"`
	// StripIndexName is strip index name from url
	StripIndexName bool `cfg:"strip_index_name"`
	// IndexName is the name of the index file, default is index.html
	IndexName string `cfg:"index_name"`
	// SPA is automatically redirect to index.html
	SPA bool `cfg:"spa"`
	// SPAEnableFile is enable .* file to be served to index.html if not found, default is false
	SPAEnableFile bool `cfg:"spa_enable_file"`
	// SPAIndex is set the index.html location, default is IndexName
	SPAIndex string `cfg:"spa_index"`
	// SPAIndexRegex set spa_index from URL path regex
	SPAIndexRegex []*RegexPathStore `cfg:"spa_index_regex"`
	// Browse is enable directory browsing
	Browse bool `cfg:"browse"`
	// UTC browse time format
	UTC bool `cfg:"utc"`
	// PrefixPath for strip prefix path for real file path
	PrefixPath string `cfg:"prefix_path"`
	// FilePathRegex is regex replacement for real file path, comes after PrefixPath apply
	// File path doesn't include / suffix
	FilePathRegex []*RegexPathStore `cfg:"file_path_regex"`

	CacheRegex []*RegexCacheStore `cfg:"cache_regex"`
	// BrowseCache is cache control for browse page, default is no-cache
	BrowseCache string `cfg:"browse_cache"`

	DisableFolderSlashRedirect bool `cfg:"disable_folder_slash_redirect"`

	// ETag enables ETag generation for served files; empty disables it.
	//   - weak: W/"<modtime>-<size>", cheap and computed without reading the file.
	//   - strong: "<sha256 of content>", required for filesystems without modtime (embedded).
	ETag string `cfg:"etag"`

	fs            http.FileSystem
	etagCache     sync.Map
	customContent func(r *http.Request, name string, content io.ReadSeeker) io.ReadSeeker
}

func (f *Folder) SetCustomContent(customContent func(r *http.Request, name string, content io.ReadSeeker) io.ReadSeeker) {
	f.customContent = customContent
}

func (f *Folder) SetFs(fs http.FileSystem) {
	f.fs = fs
}

type RegexPathStore struct {
	Regex       string `cfg:"regex"`
	Replacement string `cfg:"replacement"`
	rgx         *regexp.Regexp
}

type RegexCacheStore struct {
	Regex        string `cfg:"regex"`
	CacheControl string `cfg:"cache_control"`
	rgx          *regexp.Regexp
}

func (f *Folder) Middleware() (func(http.Handler) http.Handler, error) {
	if f.IndexName == "" {
		f.IndexName = indexPage
	}

	f.IndexName = strings.Trim(f.IndexName, "/")

	if f.SPAIndex == "" {
		f.SPAIndex = f.IndexName
	}

	for i := range f.SPAIndexRegex {
		rgx, err := regexp.Compile(f.SPAIndexRegex[i].Regex)
		if err != nil {
			return nil, err
		}

		f.SPAIndexRegex[i].rgx = rgx
	}

	for i := range f.FilePathRegex {
		rgx, err := regexp.Compile(f.FilePathRegex[i].Regex)
		if err != nil {
			return nil, err
		}

		f.FilePathRegex[i].rgx = rgx
	}

	for i := range f.CacheRegex {
		rgx, err := regexp.Compile(f.CacheRegex[i].Regex)
		if err != nil {
			return nil, err
		}

		f.CacheRegex[i].rgx = rgx
	}

	switch f.ETag {
	case "", etagWeak, etagStrong:
	default:
		return nil, fmt.Errorf("unsupported etag mode %q, use %q or %q", f.ETag, etagWeak, etagStrong)
	}

	if f.BrowseCache == "" {
		f.BrowseCache = "no-cache"
	}

	if f.fs == nil {
		f.fs = http.Dir(f.Path)
	}

	if f.BasePath != "" {
		f.BasePath = "/" + strings.Trim(f.BasePath, "/") + "/"
	} else {
		f.BasePath = "/"
	}

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upath := r.URL.Path
			if !strings.HasPrefix(upath, "/") {
				upath = "/" + upath
			}

			cPath := path.Clean(upath)
			if f.PrefixPath != "" {
				prefix := strings.TrimSuffix(f.PrefixPath, "/")
				cPath = strings.TrimPrefix(cPath, prefix)
				if cPath == "" {
					cPath = "/"
				}
			}

			for _, r := range f.FilePathRegex {
				cPathOrg := cPath
				cPath = r.rgx.ReplaceAllString(cPath, r.Replacement)
				if cPath != cPathOrg {
					break
				}
			}

			if err := f.serveFile(w, r, upath, cPath); err != nil {
				httputil.HandleError(w, httputil.NewErrorAs(err))
			}
		})
	}, nil
}

// name is '/'-separated, not filepath.Separator.
func (f *Folder) serveFile(w http.ResponseWriter, r *http.Request, uPath, cPath string) error {
	// redirect .../index.html to .../
	// can't use Redirect() because that would make the path absolute,
	// which would be a problem running under StripPrefix
	if f.StripIndexName && strings.HasSuffix(uPath, f.IndexName) {
		return localRedirect(w, r, strings.TrimSuffix(uPath, f.IndexName))
	}

	file, err := f.fs.Open(cPath)
	if err != nil {
		if os.IsNotExist(err) && f.SPA {
			if f.SPAEnableFile || !strings.Contains(filepath.Base(cPath), ".") {
				for _, spaR := range f.SPAIndexRegex {
					spaFile := spaR.rgx.ReplaceAllString(uPath, spaR.Replacement)
					if spaFile != uPath {
						return f.fsFile(w, r, spaFile)
					}
				}

				return f.fsFile(w, r, f.SPAIndex)
			}
		}

		return toHTTPError(err)
	}
	defer file.Close()

	d, err := file.Stat()
	if err != nil {
		return toHTTPError(err)
	}

	// redirect to canonical path: / at end of directory url
	// r.URL.Path always begins with /
	if d.IsDir() {
		if uPath[len(uPath)-1] != '/' {
			if !f.DisableFolderSlashRedirect {
				return localRedirect(w, r, path.Base(uPath)+"/")
			}
		}
	} else {
		if uPath[len(uPath)-1] == '/' {
			return localRedirect(w, r, "../"+path.Base(uPath))
		}
	}

	filePath := cPath
	if d.IsDir() && f.Index {
		// use contents of index.html for directory, if present
		indexPath := path.Join(cPath, f.IndexName)
		ff, err := f.fs.Open(indexPath)
		if err == nil {
			defer ff.Close()
			dd, err := ff.Stat()
			if err == nil {
				d = dd
				file = ff
				filePath = indexPath
			}
		}
	}

	// Still a directory? (we didn't find an index.html file)
	if d.IsDir() {
		if f.Browse {
			return f.dirList(w, r, file)
		}

		return toHTTPError(os.ErrNotExist)
	}

	return f.fsFileInfo(w, r, filePath, d, file)
}

func (f *Folder) dirList(w http.ResponseWriter, r *http.Request, folder http.File) error {
	dirs, err := folder.Readdir(-1)
	if err != nil {
		return fmt.Errorf("Error reading directory")
	}
	folderDirs := []fs.FileInfo{}
	folderFiles := []fs.FileInfo{}
	for _, dir := range dirs {
		if dir.IsDir() {
			folderDirs = append(folderDirs, dir)
		} else {
			folderFiles = append(folderFiles, dir)
		}
	}

	query := r.URL.Query()
	sortField := query.Get("sort")
	sortDesc, _ := strconv.ParseBool(query.Get("desc"))

	sort.Slice(folderDirs, sortTable(sortField, sortDesc, folderDirs))
	sort.Slice(folderFiles, sortTable(sortField, sortDesc, folderFiles))

	dirs = append(folderDirs, folderFiles...)

	values := map[string]any{
		"basePath":  f.BasePath,
		"dirs":      dirs,
		"url":       r.URL.Path,
		"utc":       f.UTC,
		"sortField": sortField,
		"sortDesc":  sortDesc,
	}

	v, err := render.ExecuteWithData(`
{{- define "style" -}}
body {
	padding: 0;
	margin: 0;
}

table tr:nth-child(even) {
	background-color: #e5e5e5;
}
table tr:hover > td {
	background-color:#ff4000;
	color: #fff;
}
table tr:hover a, th a {
	color: inherit;
	text-decoration: none;
}
{{- end -}}
{{- define "html" -}}
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta http-equiv="X-UA-Compatible" content="IE=edge">
	<meta name="viewport" content="width=640, initial-scale=1.0">
	<title>Browser</title>
	<style>{{ execTemplate "style" "" | codec.StringToByte | minify "css" | codec.ByteToString }}</style>
</head>
<body>
<table>
	<colgroup>
		<col span="1" style="width: 50%;">
		<col span="1" style="width: 25%;">
		<col span="1" style="width: 25%;">
	</colgroup>
	<tr style="background-color: #000; color: #fff;">
		<th><a href="./?sort=title{{and (eq .sortField "title") (not .sortDesc) | ternary "&desc=1" "" }}">Title</a></th>
		<th><a href="./?sort=size{{and (eq .sortField "size") (not .sortDesc) | ternary "&desc=1" "" }}">File Size</a></th>
		<th><a href="./?sort=date{{and (eq .sortField "date") (not .sortDesc) | ternary "&desc=1" "" }}">Last Modified</a></th>
	</tr>
	{{- if ne .url .basePath }}
	<tr>
		<td>📁 <a href="../">..</a></td>
		<td>-</td>
		<td>-</td>
	</tr>
	{{- end }}
	{{- range .dirs }}
	<tr>
		<td>{{ ternary "📁" "📄" .IsDir }} <a href="./{{ .Name }}{{ ternary "/" "" .IsDir }}" {{ ternary "" "download" .IsDir }}>{{ html2.EscapeString .Name }}{{ ternary "/" "" .IsDir }}</a></td>
		<td>{{ .Size | cast.ToUint64 | humanize.Bytes }}</td>
		<td>{{ time.Format time.RFC3339 (ternary (time.UTC .ModTime) .ModTime $.utc) }}</td>
	</tr>
	{{- end }}
</body>
</html>
{{- end -}}
{{ execTemplate "html" . | codec.StringToByte | minify "html" | codec.ByteToString }}
`, values)
	if err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	if f.BrowseCache != "" {
		w.Header().Set("Cache-Control", f.BrowseCache)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(v)

	return err
}

// localRedirect gives a Moved Permanently response.
// It does not convert relative paths to absolute paths like Redirect does.
func localRedirect(w http.ResponseWriter, r *http.Request, newPath string) error {
	if q := r.URL.RawQuery; q != "" {
		newPath += "?" + q
	}

	slog.Debug("redirecting to " + newPath)

	return httputil.Redirect(w, http.StatusMovedPermanently, newPath)
}

// toHTTPError returns a non-specific HTTP error message for the given error.
func toHTTPError(err error) error {
	if os.IsNotExist(err) {
		return httputil.NewError("", nil, http.StatusNotFound)
	}
	if os.IsPermission(err) {
		return httputil.NewError("", nil, http.StatusForbidden)
	}

	return err
}

func (f *Folder) fsFile(w http.ResponseWriter, r *http.Request, file string) error {
	hFile, err := f.fs.Open(file)
	if err != nil {
		return httputil.NewError("", err, http.StatusNotFound)
	}
	defer hFile.Close()

	fi, err := hFile.Stat()
	if err != nil {
		return err
	}

	f.serveContent(w, r, file, fi.Name(), fi.ModTime(), hFile)

	return nil
}

func (f *Folder) fsFileInfo(w http.ResponseWriter, r *http.Request, filePath string, fi fs.FileInfo, file http.File) error {
	f.serveContent(w, r, filePath, fi.Name(), fi.ModTime(), file)

	return nil
}

func (f *Folder) Cache(w http.ResponseWriter, fileName string) {
	for _, r := range f.CacheRegex {
		if r.rgx.MatchString(fileName) {
			w.Header().Set("Cache-Control", r.CacheControl)

			break
		}
	}
}

func (f *Folder) ServeContent(w http.ResponseWriter, req *http.Request, name string, modtime time.Time, content io.ReadSeeker) {
	f.serveContent(w, req, "", name, modtime, content)
}

// serveContent serves the content; filePath is the key used to cache strong
// ETags, an empty filePath disables caching.
func (f *Folder) serveContent(w http.ResponseWriter, req *http.Request, filePath, name string, modtime time.Time, content io.ReadSeeker) {
	f.Cache(w, name)
	if f.customContent != nil {
		content = f.customContent(req, name, content)
	}

	if f.ETag != "" {
		etag, err := f.etag(filePath, modtime, content)
		if err != nil {
			slog.Warn("cannot generate etag", "file", filePath, "err", err.Error())
		} else if etag != "" {
			w.Header().Set("ETag", etag)
		}
	}

	http.ServeContent(w, req, name, modtime, content)
}

func sortTable(sortField string, sortDesc bool, fs []fs.FileInfo) func(i, j int) bool {
	return func(i, j int) bool {
		ret := false
		switch sortField {
		case "name":
			ret = fs[i].Name() < fs[j].Name()
		case "size":
			ret = fs[i].Size() < fs[j].Size()
		case "date":
			ret = fs[i].ModTime().Before(fs[j].ModTime())
		default:
			ret = fs[i].Name() < fs[j].Name()
		}

		if sortDesc {
			return !ret
		}

		return ret
	}
}
