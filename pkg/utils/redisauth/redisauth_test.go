package redisauth

import (
	"strings"
	"testing"

	storeredis "github.com/rakunlabs/cache/store/redis"
)

func TestApplyGCPIAMDisabled(t *testing.T) {
	cfg := storeredis.ConnConfig{Password: "pass"}
	if err := ApplyGCPIAM(&cfg, false); err != nil {
		t.Fatal(err)
	}
	if cfg.CredentialsProvider != nil {
		t.Fatal("credentials provider set while disabled")
	}
}

func TestApplyGCPIAMRejectsStaticCredentials(t *testing.T) {
	for _, cfg := range []storeredis.ConnConfig{{Username: "user"}, {Password: "pass"}} {
		err := ApplyGCPIAM(&cfg, true)
		if err == nil || !strings.Contains(err.Error(), "gcp_iam") {
			t.Fatalf("err = %v, want gcp_iam error", err)
		}
	}
}
