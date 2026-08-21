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
