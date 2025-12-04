package login

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/rakunlabs/turna/pkg/render"
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

	m.statusContent = string(data)

	return nil
}

func (m *Login) StatusHandler(w http.ResponseWriter, r *http.Request) {
	cookieName := ""
	pathParsed := strings.Split(r.URL.Path, "/")
	if len(pathParsed) > 1 {
		cookieName = pathParsed[len(pathParsed)-1]
	} else {
		cookieName = pathParsed[0]
	}

	if cookieName == "" {
		cookieName = m.SuccessCookie.CookieName
	}

	data, err := render.ExecuteWithData(m.statusContent, map[string]interface{}{
		"cookie": cookieName,
	})
	if err != nil {
		httputil.HandleError(w, httputil.NewError("render error", err, http.StatusInternalServerError))

		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
