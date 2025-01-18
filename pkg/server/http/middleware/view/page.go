package view

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/worldline-go/cache"
	"github.com/worldline-go/klient"

	httputil2 "github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/model"
)

type PageUI struct {
	Handlers cache.Cacher[cacheKey, *httputil.ReverseProxy]

	m sync.Mutex
}

func (m *View) GetPageUI(ctx context.Context, page *Page) (*httputil.ReverseProxy, error) {
	m.pageUI.m.Lock()
	defer m.pageUI.m.Unlock()

	h, ok, err := m.pageUI.Handlers.Get(ctx, cacheKey{
		Name: page.Path,
		Addr: page.URL,
	})
	if err != nil {
		return nil, err
	}

	if ok {
		return h, nil
	}

	apiURL, err := url.Parse(page.URL)
	if err != nil {
		return nil, err
	}

	c, err := klient.NewPlain()
	if err != nil {
		return nil, err
	}

	proxy := &httputil.ReverseProxy{
		Transport: c.HTTP.Transport,
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

	m.pageUI.Handlers.Set(ctx, cacheKey{
		Name: page.Path,
		Addr: page.URL,
	}, proxy)

	return proxy, nil
}

func findPageAddrList(nameList []string, pages []Page) (*Page, string) {
	var name string

	for i := range nameList {
		name += nameList[i]
		if page := findPage(name, pages); page != nil {
			return page, name
		}

		name += "/"
	}

	return nil, ""
}

func findPage(name string, pages []Page) *Page {
	for i := range pages {
		if strings.Trim(pages[i].Path, "/") == name {
			return &pages[i]
		}
	}

	return nil
}

func findPageInGroup(oldName []string, name []string, groups []Group) (*Page, []string) {
	for _, g := range groups {
		if g.Name != getIndex(name, 0) {
			continue
		}

		lookGroup := append(oldName, g.Name)

		for _, s := range g.Services {
			if s.Name != getIndex(name, 1) {
				continue
			}

			serviceGroup := append(lookGroup, s.Name)

			if getIndex(name, 2) != "" {
				if page := findPage(name[2], s.Page); page != nil {
					return page, append(serviceGroup, name[2])
				}
			}
		}

		if page, v := findPageInGroup(lookGroup, name[1:], g.Groups); page != nil {
			return page, v
		}
	}

	return nil, nil
}

func (m *View) Page(w http.ResponseWriter, r *http.Request, name string) {
	info, err := m.GetInfo(r.Context())
	if err != nil {
		httputil2.JSON(w, http.StatusBadRequest, model.MetaData{Message: err.Error()})

		return
	}

	nameSplit := strings.Split(name, "/")

	page, namePath := findPageAddrList(nameSplit, info.Page)
	if page != nil {
		name = namePath
	} else {
		var nameL []string
		page, nameL = findPageInGroup(nil, nameSplit, info.Groups)
		name = strings.Join(nameL, "/")
	}

	if page == nil {
		httputil2.JSON(w, http.StatusNotFound, model.MetaData{Message: name + " not found any page"})

		return
	}

	h, err := m.GetPageUI(r.Context(), page)
	if err != nil {
		httputil2.JSON(w, http.StatusBadRequest, model.MetaData{Message: err.Error()})

		return
	}

	http.StripPrefix(path.Join(m.PrefixPath, "page", name), h).ServeHTTP(w, r)
}
