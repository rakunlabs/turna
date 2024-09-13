package view

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"

	httputil2 "github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type PageUI struct {
	Handlers map[string]*httputil.ReverseProxy
}

func (m *View) SetPageUI(pages []Page) error {
	if m.pageUI.Handlers == nil {
		m.pageUI.Handlers = make(map[string]*httputil.ReverseProxy)
	}

	for _, page := range pages {
		if page.URL == "" {
			continue
		}

		apiURL, err := url.Parse(page.URL)
		if err != nil {
			return err
		}

		proxy := &httputil.ReverseProxy{
			Rewrite: func(r *httputil.ProxyRequest) {
				if r.Out.URL.Path == "/" || r.Out.URL.Path == "" {
					httputil2.RewriteRequestURLTarget(r.Out, apiURL)
				} else {
					r.SetURL(apiURL)
				}

				for k, v := range page.Header.Request.SetHeader {
					r.Out.Header.Set(k, v)
				}

				for k, v := range page.Header.Request.AddHeader {
					r.Out.Header.Add(k, v)
				}

				for _, k := range page.Header.Request.RemoveHeader {
					r.Out.Header.Del(k)
				}

				if page.Host {
					r.Out.Host = r.In.Host // if desired
				}
			},
			ModifyResponse: func(r *http.Response) error {
				for k, v := range page.Header.Response.SetHeader {
					r.Header.Set(k, v)
				}

				for k, v := range page.Header.Response.AddHeader {
					r.Header.Add(k, v)
				}

				for _, k := range page.Header.Response.RemoveHeader {
					r.Header.Del(k)
				}

				return nil
			},
		}

		m.pageUI.Handlers[page.Path] = proxy
	}

	return nil
}

func (m *View) Page(w http.ResponseWriter, r *http.Request, name string) {
	if h, ok := m.pageUI.Handlers[name]; ok {
		http.StripPrefix(path.Join(m.PrefixPath, "page", name), h).ServeHTTP(w, r)

		return
	}

	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("view [" + name + "] - 404 page not found"))
}
