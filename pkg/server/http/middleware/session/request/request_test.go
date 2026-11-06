package request

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthParamsInBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("credentials leaked into query: %q", r.URL.RawQuery)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.PostForm.Get("client_id") != "app" || r.PostForm.Get("client_secret") != "s3cret" {
			t.Errorf("form = %v", r.PostForm)
		}
		if _, _, ok := r.BasicAuth(); ok {
			t.Error("unexpected basic auth with params style")
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	_, err := (&Auth{}).RefreshToken(context.Background(), RefreshTokenConfig{
		RefreshToken: "rt",
		AuthRequestConfig: AuthRequestConfig{
			TokenURL:        srv.URL,
			ClientID:        "app",
			ClientSecret:    "s3cret",
			AuthHeaderStyle: AuthHeaderStyleParams,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
