package template

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestTemplateConcurrentAndStatus(t *testing.T) {
	m, err := (&Template{Template: `{{ .body.n }}`}).Middleware()
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			h := m(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(w, `{"n":%d}`, i)
			}))

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d", rec.Code)
			}

			if got := rec.Body.String(); got != fmt.Sprint(i) {
				t.Errorf("body = %q, want %d", got, i)
			}
		})
	}
	wg.Wait()
}
