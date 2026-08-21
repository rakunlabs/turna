package session

import "testing"

func TestSafeRedirectPath(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty", value: "", want: "/"},
		{name: "root", value: "/", want: "/"},
		{name: "path", value: "/account", want: "/account"},
		{name: "query and fragment", value: "/account?tab=keys#active", want: "/account?tab=keys#active"},
		{name: "about blank", value: "about:blank", want: "/"},
		{name: "absolute URL", value: "https://example.com/account", want: "/"},
		{name: "protocol relative", value: "//example.com/account", want: "/"},
		{name: "backslash host", value: `/\example.com/account`, want: "/"},
		{name: "encoded backslash host", value: `/%5Cexample.com/account`, want: "/"},
		{name: "encoded slash host", value: `/%2Fexample.com/account`, want: "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeRedirectPath(tt.value); got != tt.want {
				t.Fatalf("safeRedirectPath(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
