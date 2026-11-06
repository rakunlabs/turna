package login

import (
	"bytes"
	"embed"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

//go:embed files/*
var filesFS embed.FS

func (m *Login) SetFiles() error {
	f, err := fs.Sub(filesFS, "files")
	if err != nil {
		return err
	}

	fStatus, err := f.Open("status-iframe.html")
	if err != nil {
		return err
	}
	defer fStatus.Close()

	data, err := io.ReadAll(fStatus)
	if err != nil {
		return err
	}

	// html/template escapes the cookie name for the JavaScript string
	m.statusTemplate, err = template.New("status").Parse(string(data))

	return err
}

func (m *Login) StatusHandler(w http.ResponseWriter, r *http.Request) {
	cookieName := ""
	pathParsed := strings.Split(r.URL.Path, "/")
	if len(pathParsed) > 1 {
		cookieName = pathParsed[len(pathParsed)-1]
	} else {
		cookieName = pathParsed[0]
	}

	if cookieName == "" || !validCookieName(cookieName) {
		cookieName = m.SuccessCookie.CookieName
	}

	var data bytes.Buffer
	if err := m.statusTemplate.Execute(&data, map[string]any{
		"cookie": cookieName,
	}); err != nil {
		httputil.HandleError(w, httputil.NewError("render error", err, http.StatusInternalServerError))

		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data.Bytes())
}

// validCookieName reports whether name is a plain cookie token.
func validCookieName(name string) bool {
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-', c == '.':
		default:
			return false
		}
	}

	return true
}
