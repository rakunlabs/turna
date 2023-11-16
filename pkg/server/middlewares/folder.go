package middlewares

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/render"
)

const indexPage = "/index.html"

type Folder struct {
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

	fs http.FileSystem
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

func (f *Folder) Middleware() ([]echo.MiddlewareFunc, error) {
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

	if f.BrowseCache == "" {
		f.BrowseCache = "no-cache"
	}

	f.fs = http.Dir(f.Path)

	return []echo.MiddlewareFunc{func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			upath := c.Request().URL.Path
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

			return f.serveFile(c, upath, cPath)
		}
	}}, nil
}

// name is '/'-separated, not filepath.Separator.
func (f *Folder) serveFile(c echo.Context, uPath, cPath string) error {
	// redirect .../index.html to .../
	// can't use Redirect() because that would make the path absolute,
	// which would be a problem running under StripPrefix
	if f.StripIndexName && strings.HasSuffix(uPath, f.IndexName) {
		return localRedirect(c, strings.TrimSuffix(uPath, f.IndexName))
	}

	file, err := f.fs.Open(cPath)
	if err != nil {
		if os.IsNotExist(err) && f.SPA {
			if f.SPAEnableFile || !strings.Contains(filepath.Base(cPath), ".") {
				for _, r := range f.SPAIndexRegex {
					spaFile := r.rgx.ReplaceAllString(uPath, r.Replacement)
					if spaFile != uPath {
						return f.fsFile(c, spaFile)
					}
				}

				return f.fsFile(c, f.SPAIndex)
			}
		}

		return toHTTPError(c, err)
	}
	defer file.Close()

	d, err := file.Stat()
	if err != nil {
		return toHTTPError(c, err)
	}

	// redirect to canonical path: / at end of directory url
	// r.URL.Path always begins with /
	if d.IsDir() {
		if uPath[len(uPath)-1] != '/' {
			return localRedirect(c, path.Base(uPath)+"/")
		}
	} else {
		if uPath[len(uPath)-1] == '/' {
			return localRedirect(c, "../"+path.Base(uPath))
		}
	}

	if d.IsDir() && f.Index {
		// use contents of index.html for directory, if present
		ff, err := f.fs.Open(filepath.Join(cPath, f.IndexName))
		if err == nil {
			defer ff.Close()
			dd, err := ff.Stat()
			if err == nil {
				d = dd
				file = ff
			}
		}
	}

	// Still a directory? (we didn't find an index.html file)
	if d.IsDir() {
		if f.Browse {
			return f.dirList(c, file)
		}

		return toHTTPError(c, os.ErrNotExist)
	}

	return f.fsFileInfo(c, d, file)
}

func (f *Folder) dirList(c echo.Context, folder http.File) error {
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

	sortField := c.QueryParam("sort")
	sortDesc, _ := strconv.ParseBool(c.QueryParam("desc"))

	sort.Slice(folderDirs, sortTable(sortField, sortDesc, folderDirs))
	sort.Slice(folderFiles, sortTable(sortField, sortDesc, folderFiles))

	dirs = append(folderDirs, folderFiles...)

	values := map[string]interface{}{
		"dirs":      dirs,
		"url":       c.Request().URL.Path,
		"utc":       f.UTC,
		"sortField": sortField,
		"sortDesc":  sortDesc,
	}

	v, err := render.GlobalRender.ExecuteWithData(`
{{- define "style" -}}
body {
	padding: 0;
	margin: 0;
}

table tr:nth-child(even) {
	background-color: #e5e5e5;
}
table tr:hover > td {
	background-color: #ff6700;
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
	<tr style="background-color: #fca311">
		<th><a href="./?sort=title{{and (eq .sortField "title") (not .sortDesc) | ternary "&desc=1" "" }}">Title</a></th>
		<th><a href="./?sort=size{{and (eq .sortField "size") (not .sortDesc) | ternary "&desc=1" "" }}">File Size</a></th>
		<th><a href="./?sort=date{{and (eq .sortField "date") (not .sortDesc) | ternary "&desc=1" "" }}">Last Modified</a></th>
	</tr>
	<tr>
		<td>📁 <a href="../">..</a></td>
		<td>-</td>
		<td>-</td>
	</tr>
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
		c.Logger().Error(err)

		return fmt.Errorf("error executing template")
	}

	if f.BrowseCache != "" {
		c.Response().Header().Set("Cache-Control", f.BrowseCache)
	}

	return c.HTML(http.StatusOK, v)
}

// localRedirect gives a Moved Permanently response.
// It does not convert relative paths to absolute paths like Redirect does.
func localRedirect(c echo.Context, newPath string) error {
	if q := c.Request().URL.RawQuery; q != "" {
		newPath += "?" + q
	}

	c.Logger().Debug("redirecting to", newPath)

	return c.Redirect(http.StatusMovedPermanently, newPath)
}

// toHTTPError returns a non-specific HTTP error message for the given error.
func toHTTPError(c echo.Context, err error) error {
	if os.IsNotExist(err) {
		return echo.ErrNotFound
	}
	if os.IsPermission(err) {
		return echo.ErrForbidden
	}

	// Default:
	return err
}

func (f *Folder) fsFile(c echo.Context, file string) error {
	hFile, err := f.fs.Open(file)
	if err != nil {
		return echo.ErrNotFound
	}
	defer hFile.Close()

	fi, err := hFile.Stat()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	f.ServeContent(c.Response(), c.Request(), fi.Name(), fi.ModTime(), hFile)

	return nil
}

func (f *Folder) fsFileInfo(c echo.Context, fi fs.FileInfo, file http.File) error {
	f.ServeContent(c.Response(), c.Request(), fi.Name(), fi.ModTime(), file)

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
	f.Cache(w, name)
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
