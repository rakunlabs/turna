package timeout

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// slow waits for d or the request deadline, like a proxied upstream.
func slow(d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(d):
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
			http.Error(w, "upstream canceled", http.StatusBadGateway)
		}
	})
}

func serve(t *testing.T, m *Timeout, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mw(h).ServeHTTP(rec, req)

	return rec
}

func TestTimeout(t *testing.T) {
	m := &Timeout{
		Duration: 20 * time.Millisecond,
		Rules: []Rule{
			{Prefix: []string{"/slow", "/export"}, Duration: time.Second},
			{Prefix: []string{"/stream"}, Duration: 0},
		},
	}

	tests := []struct {
		path string
		want int
	}{
		{"/api", http.StatusGatewayTimeout},
		{"/slow/x", http.StatusNoContent},
		{"/export", http.StatusNoContent},
		{"/stream", http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := serve(t, m, slow(50*time.Millisecond), httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestTimeoutFastResponse(t *testing.T) {
	rec := serve(t, &Timeout{Duration: time.Second}, slow(0), httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTimeoutAfterHeaders(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		<-r.Context().Done()
	})

	rec := serve(t, &Timeout{Duration: 10 * time.Millisecond}, h, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "partial" {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
}

func TestTimeoutGRPC(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/svc/Method", nil)
	req.Header.Set("Content-Type", "application/grpc")

	rec := serve(t, &Timeout{Duration: 10 * time.Millisecond}, slow(time.Second), req)
	if rec.Code != http.StatusOK || rec.Header().Get("Grpc-Status") != "4" {
		t.Fatalf("status = %d grpc-status = %q", rec.Code, rec.Header().Get("Grpc-Status"))
	}
}

func TestTimeoutConfig(t *testing.T) {
	for _, m := range []Timeout{
		{},
		{Duration: -time.Second},
		{Duration: time.Second, Rules: []Rule{{Duration: time.Second}}},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}
