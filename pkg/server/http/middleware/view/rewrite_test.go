package view

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestRewriterModifyResponse(t *testing.T) {
	t.Parallel()

	target := mustParseURL(t, "https://backend.example")
	r, err := newRewriter(&Rewrite{
		Base:          true,
		Absolute:      true,
		Origin:        true,
		Location:      true,
		Cookie:        true,
		Frame:         true,
		ForwardPrefix: true,
		ContentTypes:  []string{"text/html"},
		MaxBodySize:   1 << 20,
		Replace: []Replace{
			{Old: "APP_NAME", New: "Turna at {{prefix}}"},
			{Regex: `data-id="([0-9]+)"`, New: `data-page-id="$1"`},
		},
	}, "/view/page/admin", target)
	if err != nil {
		t.Fatal(err)
	}

	body := `<html><head><title>APP_NAME</title></head><body data-id="42">` +
		`<a href="/users">users</a>` +
		`<a href="/view/page/admin/settings">settings</a>` +
		`<img src='/logo.png' srcset="/small.png 1x, https://cdn.example/big.png 2x">` +
		`<div style="background:url('/bg.png')"></div>` +
		`<a href="https://backend.example/account">account</a>` +
		`<script>const endpoint="https:\/\/backend.example\/api"</script>` +
		`</body></html>`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                        {"text/html; charset=utf-8"},
			"Location":                            {"https://backend.example/login?next=%2Fusers"},
			"Set-Cookie":                          {"sid=abc; Path=/; Domain=backend.example; HttpOnly; SameSite=Lax"},
			"X-Frame-Options":                     {"DENY"},
			"Content-Security-Policy":             {"default-src 'self'; frame-ancestors 'none'; script-src 'self'"},
			"Content-Security-Policy-Report-Only": {"frame-ancestors https://parent.example; report-uri /csp"},
			"ETag":                                {`"old"`},
		},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}

	if err := r.modifyResponse(resp); err != nil {
		t.Fatal(err)
	}

	gotBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	got := string(gotBytes)

	wants := []string{
		`<head><base href="/view/page/admin/">`,
		`<title>Turna at /view/page/admin</title>`,
		`data-page-id="42"`,
		`href="/view/page/admin/users"`,
		`href="/view/page/admin/settings"`,
		`src='/view/page/admin/logo.png'`,
		`srcset="/view/page/admin/small.png 1x, https://cdn.example/big.png 2x"`,
		`url('/view/page/admin/bg.png')`,
		`href="/view/page/admin/account"`,
		`"\/view\/page\/admin\/api"`,
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten body does not contain %q\nbody: %s", want, got)
		}
	}

	if got := resp.Header.Get("Location"); got != "/view/page/admin/login?next=%2Fusers" {
		t.Errorf("Location = %q", got)
	}
	if got := resp.Header.Get("Set-Cookie"); got != "sid=abc; Path=/view/page/admin/; HttpOnly; SameSite=Lax" {
		t.Errorf("Set-Cookie = %q", got)
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "" {
		t.Errorf("X-Frame-Options was not removed: %q", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got != "default-src 'self'; script-src 'self'" {
		t.Errorf("Content-Security-Policy = %q", got)
	}
	if got := resp.Header.Get("Content-Security-Policy-Report-Only"); got != "report-uri /csp" {
		t.Errorf("Content-Security-Policy-Report-Only = %q", got)
	}
	if got := resp.Header.Get("ETag"); got != "" {
		t.Errorf("ETag was not removed: %q", got)
	}
	if resp.ContentLength != int64(len(gotBytes)) {
		t.Errorf("ContentLength = %d, body length = %d", resp.ContentLength, len(gotBytes))
	}

	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Accept-Encoding", "gzip, br")
	r.modifyRequest(req)
	if got := req.Header.Get("Accept-Encoding"); got != "identity" {
		t.Errorf("Accept-Encoding = %q", got)
	}
	if got := req.Header.Get("X-Forwarded-Prefix"); got != "/view/page/admin" {
		t.Errorf("X-Forwarded-Prefix = %q", got)
	}
}

func TestRewriterGzip(t *testing.T) {
	t.Parallel()

	r, err := newRewriter(&Rewrite{Absolute: true}, "/view/page/app", mustParseURL(t, "https://backend.example"))
	if err != nil {
		t.Fatal(err)
	}

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write([]byte(`<link href="/app.css">`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":     {"text/html"},
			"Content-Encoding": {"gzip"},
		},
		Body: io.NopCloser(bytes.NewReader(compressed.Bytes())),
	}
	if err := r.modifyResponse(resp); err != nil {
		t.Fatal(err)
	}

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `<link href="/view/page/app/app.css">` {
		t.Errorf("body = %q", got)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q", got)
	}
}

func TestRewriterSkipsLargeAndOtherContent(t *testing.T) {
	t.Parallel()

	r, err := newRewriter(&Rewrite{Absolute: true, MaxBodySize: 4}, "/view/page/app", mustParseURL(t, "https://backend.example"))
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "large", contentType: "text/html", body: `/large`},
		{name: "json", contentType: "application/json", body: `{"url":"/api"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {test.contentType}},
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}
			if err := r.modifyResponse(resp); err != nil {
				t.Fatal(err)
			}

			got, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.body {
				t.Errorf("body = %q, want %q", got, test.body)
			}
		})
	}
}

func TestNewRewriterRejectsInvalidReplace(t *testing.T) {
	t.Parallel()

	_, err := newRewriter(&Rewrite{Replace: []Replace{{Regex: "["}}}, "/page/app", mustParseURL(t, "https://backend.example"))
	if err == nil {
		t.Fatal("expected invalid regexp error")
	}
}

func mustParseURL(t *testing.T, value string) *url.URL {
	t.Helper()

	u, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}

	return u
}
