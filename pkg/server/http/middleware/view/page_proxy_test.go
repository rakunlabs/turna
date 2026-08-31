package view

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPageUsesHTTPProxy(t *testing.T) {
	var proxyRequestURL string
	forwardProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequestURL = r.URL.String()
		_, _ = w.Write([]byte("from proxy"))
	}))
	defer forwardProxy.Close()

	m := &View{
		PrefixPath: "/view",
		Info: Info{Page: []Page{
			{
				Name:  "application",
				Path:  "application",
				URL:   "http://application.internal:8080",
				Proxy: forwardProxy.URL,
			},
		}},
	}

	middleware, err := m.Middleware(context.Background(), "test")
	if err != nil {
		t.Fatalf("build middleware: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/view/page/application/api/users?active=true", nil)
	recorder := httptest.NewRecorder()
	middleware(nil).ServeHTTP(recorder, req)

	response := recorder.Result()
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.StatusCode, body)
	}
	if string(body) != "from proxy" {
		t.Fatalf("expected proxy response, got %q", body)
	}
	if want := "http://application.internal:8080/api/users?active=true"; proxyRequestURL != want {
		t.Fatalf("expected proxy request URL %q, got %q", want, proxyRequestURL)
	}
}

func TestPageCacheSeparatesHTTPProxies(t *testing.T) {
	m := &View{}
	if _, err := m.Middleware(context.Background(), "test"); err != nil {
		t.Fatalf("build middleware: %v", err)
	}

	page := &Page{URL: "http://application.internal:8080", Proxy: "http://proxy-one.internal:3128"}
	first, err := m.GetPageUI(context.Background(), page, "/view/page/application")
	if err != nil {
		t.Fatalf("build first page proxy: %v", err)
	}

	page.Proxy = "http://proxy-two.internal:3128"
	second, err := m.GetPageUI(context.Background(), page, "/view/page/application")
	if err != nil {
		t.Fatalf("build second page proxy: %v", err)
	}
	if first == second {
		t.Fatal("expected a new page handler when the HTTP proxy changes")
	}
}

func TestPageCacheSeparatesRewriteConfig(t *testing.T) {
	m := &View{}
	if _, err := m.Middleware(context.Background(), "test"); err != nil {
		t.Fatalf("build middleware: %v", err)
	}

	page := &Page{
		URL: "http://application.internal:8080",
		Rewrite: &Rewrite{Replace: []Replace{
			{Old: "/dashboard/api", New: "/first/api"},
		}},
	}
	first, err := m.GetPageUI(context.Background(), page, "/view/page/application")
	if err != nil {
		t.Fatalf("build first page rewrite: %v", err)
	}

	page.Rewrite.Replace[0].New = "/second/api"
	second, err := m.GetPageUI(context.Background(), page, "/view/page/application")
	if err != nil {
		t.Fatalf("build second page rewrite: %v", err)
	}
	if first == second {
		t.Fatal("expected a new page handler when the rewrite config changes")
	}
}
