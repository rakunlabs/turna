package accesslog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponseStreamedAndStatusKept(t *testing.T) {
	m := &AccessLog{Path: Path{Enabled: []Check{{URL: "/**"}}}}

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	big := strings.Repeat("x", 4096)
	flushed := false

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(big[:10]))
		w.(http.Flusher).Flush()
		flushed = true
		_, _ = w.Write([]byte(big[10:]))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !flushed || !rec.Flushed {
		t.Fatal("response not flushed through")
	}

	if rec.Code != http.StatusOK || rec.Body.String() != big {
		t.Fatalf("status = %d len = %d", rec.Code, rec.Body.Len())
	}
}
