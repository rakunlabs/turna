package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSwaggerUIHandlerServesGeneratedDocument(t *testing.T) {
	handler := (&Auth{PrefixPath: "/custom-auth"}).SwaggerUIHandler()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/custom-auth/swagger/doc.json", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var document struct {
		Info struct {
			Title string `json:"title"`
		} `json:"info"`
		BasePath string         `json:"basePath"`
		Paths    map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode swagger document: %v", err)
	}

	if document.Info.Title != "Turna Auth API" {
		t.Errorf("title = %q, want %q", document.Info.Title, "Turna Auth API")
	}
	if document.BasePath != "/custom-auth" {
		t.Errorf("basePath = %q, want %q", document.BasePath, "/custom-auth")
	}
	if _, ok := document.Paths["/oauth2/token"]; !ok {
		t.Error("generated document does not contain /oauth2/token")
	}
}
