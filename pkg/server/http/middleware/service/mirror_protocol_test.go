package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMirrorMiddlewareH2C(t *testing.T) {
	type received struct {
		proto      int
		path, body string
	}
	requests := make(chan received, 1)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- received{proto: r.ProtoMajor, path: r.URL.Path, body: string(body)}
		w.WriteHeader(http.StatusNoContent)
	}))
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	upstream.Config.Protocols = &protocols
	upstream.Start()
	defer upstream.Close()
	m := &Mirror{H2C: true, Timeout: time.Second, Servers: []MirrorServer{{Endpoints: []string{upstream.URL}}}}
	mw, err := m.MirrorMiddleware()
	if err != nil {
		t.Fatal(err)
	}
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(body)
	}))
	req := httptest.NewRequest(http.MethodPost, "http://example.com/pkg.Service/Call", strings.NewReader("payload"))
	req.Header.Set("Content-Type", "application/grpc")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "payload" {
		t.Fatalf("primary response = %d %q", w.Code, w.Body.String())
	}
	select {
	case got := <-requests:
		if got.proto != 2 || got.path != "/pkg.Service/Call" || got.body != "payload" {
			t.Fatalf("mirrored request = %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("h2c upstream received no mirrored request")
	}
}

func TestNilMirrorMiddleware(t *testing.T) {
	var m *Mirror
	mw, err := m.MirrorMiddleware()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusNoContent {
		t.Fatal("nil mirror did not pass through")
	}
}
