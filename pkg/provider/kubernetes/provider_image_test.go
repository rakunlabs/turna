package kubernetes

import "testing"

func TestPinnedImage(t *testing.T) {
	const digest = "sha256:0123456789abcdef"

	for _, tt := range []struct{ image, id, want string }{
		{"ghcr.io/rakunlabs/turna:latest", "docker-pullable://ghcr.io/rakunlabs/turna@" + digest, "ghcr.io/rakunlabs/turna@" + digest},
		{"localhost:5000/turna", "localhost:5000/turna@" + digest, "localhost:5000/turna@" + digest},
		{"turna@sha256:old", "turna@" + digest, "turna@" + digest},
		{"turna:v1", "", "turna:v1"},
		{"turna:v1", "sha256:onlyid", "turna:v1"},
	} {
		if got := pinnedImage(tt.image, tt.id); got != tt.want {
			t.Errorf("pinnedImage(%q, %q) = %q, want %q", tt.image, tt.id, got, tt.want)
		}
	}
}
