package auth

import (
	"reflect"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

func TestNormalizeResources(t *testing.T) {
	tests := []struct {
		name  string
		input []data.Resource
		want  []data.Resource
	}{
		{
			name:  "legacy path moves to paths",
			input: []data.Resource{{Path: "/api/**", Methods: []string{"GET"}}},
			want:  []data.Resource{{Paths: []string{"/api/**"}, Methods: []string{"GET"}}},
		},
		{
			name:  "legacy path appends to existing paths",
			input: []data.Resource{{Path: "/old/**", Paths: []string{"/new/**"}}},
			want:  []data.Resource{{Paths: []string{"/new/**", "/old/**"}}},
		},
		{
			name:  "duplicate is not appended twice",
			input: []data.Resource{{Path: "/api/**", Paths: []string{"/api/**"}}},
			want:  []data.Resource{{Paths: []string{"/api/**"}}},
		},
		{
			name:  "resource without legacy path is untouched",
			input: []data.Resource{{Paths: []string{"/api/**"}}},
			want:  []data.Resource{{Paths: []string{"/api/**"}}},
		},
		{
			name: "excluded is normalized recursively",
			input: []data.Resource{{
				Path: "/api/**",
				Excluded: []data.Resource{{
					Path:     "/api/private/**",
					Excluded: []data.Resource{{Path: "/api/private/public/**"}},
				}},
			}},
			want: []data.Resource{{
				Paths: []string{"/api/**"},
				Excluded: []data.Resource{{
					Paths:    []string{"/api/private/**"},
					Excluded: []data.Resource{{Paths: []string{"/api/private/public/**"}}},
				}},
			}},
		},
		{
			name:  "display path is not appended on round trip",
			input: []data.Resource{{Path: "/api/** /other/**", Paths: []string{"/api/**", "/other/**"}}},
			want:  []data.Resource{{Paths: []string{"/api/**", "/other/**"}}},
		},
		{
			name:  "nil stays nil",
			input: nil,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeResources(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("normalizeResources() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPermissionDisplayPath(t *testing.T) {
	permission := &data.Permission{Resources: []data.Resource{{
		Paths:    []string{"/api/**", "/other/**"},
		Excluded: []data.Resource{{Paths: []string{"/api/private/**", "/other/private/**"}}},
	}}}
	snapshot := &Snapshot{}
	display := snapshot.extendPermission(false, permission)
	if got := display.Resources[0].Path; got != "/api/** /other/**" {
		t.Fatalf("display Path = %q", got)
	}
	if got := display.Resources[0].Excluded[0].Path; got != "/api/private/** /other/private/**" {
		t.Fatalf("excluded display Path = %q", got)
	}
	if permission.Resources[0].Path != "" || permission.Resources[0].Excluded[0].Path != "" {
		t.Fatal("display mutated cached resources")
	}
	display.Resources = normalizeResources(display.Resources)
	if !reflect.DeepEqual(display.Resources, permission.Resources) {
		t.Fatal("read/write round trip changed resources")
	}
}

func TestPermissionMatchingIgnoresLegacyPath(t *testing.T) {
	permission := &data.Permission{Resources: []data.Resource{{
		Path: "/legacy/**", Paths: []string{"/api/**"}, Methods: []string{"GET"},
		Excluded: []data.Resource{{Path: "/api/**", Paths: []string{"/api/private/**"}, Methods: []string{"GET"}}},
	}}}
	cfg := data.CheckConfig{NoHostCheck: true}
	if checkAccess(cfg, permission, "", "/legacy/users", "GET") {
		t.Fatal("legacy Path granted access")
	}
	if !checkAccess(cfg, permission, "", "/api/users", "GET") {
		t.Fatal("legacy excluded Path denied access")
	}
	if checkAccess(cfg, permission, "", "/api/private/keys", "GET") {
		t.Fatal("excluded Paths did not deny access")
	}
	if permissionMatchesRequest(permission, "GET", "/legacy/users") {
		t.Fatal("permission filter matched legacy Path")
	}
}

// a legacy resource must keep matching the same requests after normalization.
func TestNormalizeResourcesKeepsAccessDecision(t *testing.T) {
	cfg := data.CheckConfig{NoHostCheck: true}

	legacy := &data.Permission{
		Resources: []data.Resource{{
			Path:     "/api/**",
			Methods:  []string{"GET"},
			Excluded: []data.Resource{{Path: "/api/private/**", Methods: []string{"*"}}},
		}},
	}

	normalized := &data.Permission{Resources: normalizeResources(legacy.Resources)}

	cases := []struct {
		path   string
		method string
		want   bool
	}{
		{"/api/users", "GET", true},
		{"/api/users", "POST", false},
		{"/api/private/keys", "GET", false},
		{"/other", "GET", false},
	}

	for _, c := range cases {
		if got := checkAccess(cfg, normalized, "", c.path, c.method); got != c.want {
			t.Errorf("checkAccess(%s %s) = %v, want %v", c.method, c.path, got, c.want)
		}
	}
}
