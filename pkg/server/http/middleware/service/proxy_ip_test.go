package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Downstream applications must trust Turna's socket address and inspect XFF
// right-to-left. The final hop must be the peer Turna actually observed, even
// when a public caller supplies its own forwarding headers.
func TestServiceForwardedClientIP(t *testing.T) {
	for _, tt := range []struct {
		name string
		peer string
		xff  string
		want string
	}{
		{"ipv4", "203.0.113.9:54321", "", "203.0.113.9"},
		{"ipv6", "[2001:db8::9]:54321", "", "2001:db8::9"},
		{"forged prefix", "203.0.113.9:54321", "198.51.100.1", "198.51.100.1, 203.0.113.9"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Observed-XFF", r.Header.Get("X-Forwarded-For"))
				w.WriteHeader(http.StatusNoContent)
			}))
			defer upstream.Close()
			m := Service{LoadBalancer: LoadBalancer{Servers: []Server{{URL: upstream.URL}}}}
			middlewares, err := m.Middleware()
			if err != nil {
				t.Fatal(err)
			}
			var handler http.Handler = http.NotFoundHandler()
			for i := len(middlewares) - 1; i >= 0; i-- {
				handler = middlewares[i](handler)
			}
			r := httptest.NewRequest(http.MethodPost, "http://at.example.com/auth/login", nil)
			r.RemoteAddr = tt.peer
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			r.Header.Set("X-Real-IP", "198.51.100.2")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusNoContent || w.Header().Get("Observed-XFF") != tt.want {
				t.Fatalf("status = %d, XFF = %q; want 204, %q", w.Code, w.Header().Get("Observed-XFF"), tt.want)
			}
		})
	}
}
