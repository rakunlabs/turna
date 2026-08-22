package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

func testPublicPermissionAuth() *Auth {
	cache := testCache()
	snap := cache.Snapshot()
	permission := &data.Permission{
		ID:     "perm-public",
		Name:   "public-pages",
		Public: true,
		Resources: []data.Resource{
			{
				Paths:   []string{"/public/**"},
				Methods: []string{http.MethodGet},
				Excluded: []data.Resource{
					{Paths: []string{"/public/private/**"}, Methods: []string{http.MethodGet}},
				},
			},
		},
	}

	snap.Permissions[permission.ID] = permission
	snap.PermIDs = append(snap.PermIDs, permission.ID)
	snap.PermNames[permission.Name] = permission.ID
	snap.PublicPermIDs = append(snap.PublicPermIDs, permission.ID)

	return &Auth{cache: cache}
}

func TestPublicPermissionCheck(t *testing.T) {
	m := testPublicPermissionAuth()

	tests := []struct {
		name   string
		req    data.CheckRequest
		public bool
		want   bool
	}{
		{
			name:   "anonymous public resource",
			req:    data.CheckRequest{Path: "/public/health", Method: http.MethodGet},
			public: true,
			want:   true,
		},
		{
			name:   "known user also gets public resource",
			req:    data.CheckRequest{Alias: "my-user", Path: "/public/health", Method: http.MethodGet},
			public: true,
			want:   true,
		},
		{
			name:   "unknown identity still gets public resource",
			req:    data.CheckRequest{Alias: "missing", Path: "/public/health", Method: http.MethodGet},
			public: true,
			want:   true,
		},
		{
			name: "method still applies",
			req:  data.CheckRequest{Path: "/public/health", Method: http.MethodPost},
		},
		{
			name: "excluded resource stays private",
			req:  data.CheckRequest{Path: "/public/private/report", Method: http.MethodGet},
		},
		{
			name: "unmatched resource stays private",
			req:  data.CheckRequest{Path: "/private", Method: http.MethodGet},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPublic := m.cache.CheckPublic(tt.req.Host, tt.req.Path, tt.req.Method)
			if gotPublic != tt.public {
				t.Fatalf("CheckPublic = %v, want %v", gotPublic, tt.public)
			}

			resp, err := m.cache.Check(tt.req)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if resp.Allowed != tt.want {
				t.Fatalf("Allowed = %v, want %v", resp.Allowed, tt.want)
			}
		})
	}
}

func TestCheckUserAPIPublicAnonymous(t *testing.T) {
	m := testPublicPermissionAuth()

	request := func(path string) *httptest.ResponseRecorder {
		body, err := json.Marshal(data.CheckRequestUser{Path: path, Method: http.MethodGet})
		if err != nil {
			t.Fatal(err)
		}

		r := httptest.NewRequest(http.MethodPost, "/check", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		m.CheckUserAPI(w, r)

		return w
	}

	if w := request("/public/health"); w.Code != http.StatusOK {
		t.Fatalf("public status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	if w := request("/private"); w.Code != http.StatusUnauthorized {
		t.Fatalf("private status = %d, want 401; body=%s", w.Code, w.Body.String())
	}
}
