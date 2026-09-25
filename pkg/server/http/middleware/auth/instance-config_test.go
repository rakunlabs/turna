package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/rakunlabs/gofret"
)

func TestStaticCodeStoreConfigDecodes(t *testing.T) {
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(`
cache:
  code_store:
    active: redis
    redis:
      address: ["redis:6379"]
      password: s3cret
      tls:
        enabled: true
        ca_file: /ca.pem
ldap:
  addr: ldap://local:389
  disable_sync: true
`), &raw); err != nil {
		t.Fatal(err)
	}

	var m Auth
	// same decoder as internal/config.Decode (imported there, cycle here)
	if err := gofret.New().ToInto(raw, &m); err != nil {
		t.Fatal(err)
	}

	cs := m.Cache.CodeStore
	if cs.Active != "redis" || len(cs.Redis.Address) != 1 || cs.Redis.Password != "s3cret" ||
		!cs.Redis.TLS.Enabled || cs.Redis.TLS.CAFile != "/ca.pem" {
		t.Fatalf("decoded code store = %+v", cs)
	}
}

func TestStaticCodeStoreOverridesStoredSetting(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{Cache: CacheSettings{CodeStore: CodeStoreSettings{Active: "database"}}})

	m := &Auth{cache: cache}
	if m.codeStorePinned() || m.codeStoreSettings().Active != "database" {
		t.Fatalf("unpinned instance must use the stored setting")
	}

	m.Cache.CodeStore = CodeStoreSettings{Active: " Memory "}
	if !m.codeStorePinned() || m.codeStoreSettings().Active != "memory" {
		t.Fatalf("pinned instance must use the static config, got %+v", m.codeStoreSettings())
	}

	store, err := m.codeStoreRuntime(t.Context())
	if err != nil || store == nil {
		t.Fatalf("codeStoreRuntime() = %v, %v", store, err)
	}

	m.Cache.CodeStore = CodeStoreSettings{Active: "bogus"}
	if _, err := m.codeStoreRuntime(t.Context()); err == nil || !strings.Contains(err.Error(), "static config") {
		t.Fatalf("invalid static store error = %v", err)
	}
}

func TestInstanceConfigAPIHidesSecrets(t *testing.T) {
	m := &Auth{
		Cache: CacheStatic{CodeStore: CodeStoreSettings{
			Active: "redis",
			Redis:  CodeStoreRedisSettings{Address: []string{"redis:6379"}, Password: "s3cret"},
		}},
		LDAP: LDAPStatic{Addr: "ldap://local:389", DisableSync: true},
	}

	w := httptest.NewRecorder()
	m.InstanceConfigAPI(w, httptest.NewRequest(http.MethodGet, "/auth/v1/instance-config", nil))

	body := w.Body.String()
	if strings.Contains(body, "s3cret") {
		t.Fatalf("password leaked: %s", body)
	}

	var res struct {
		Payload InstanceConfigPayloadForTest `json:"payload"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	cs := res.Payload.Cache.CodeStore
	if !cs.Pinned || cs.Active != "redis" || !cs.Redis.PasswordSet || cs.Redis.Address[0] != "redis:6379" {
		t.Fatalf("code store = %+v", cs)
	}
	if res.Payload.LDAP.Addr != "ldap://local:389" || !res.Payload.LDAP.DisableSync {
		t.Fatalf("ldap = %+v", res.Payload.LDAP)
	}
}

type InstanceConfigPayloadForTest struct {
	Cache struct {
		CodeStore struct {
			Pinned bool   `json:"pinned"`
			Active string `json:"active"`
			Redis  struct {
				Address     []string `json:"address"`
				PasswordSet bool     `json:"password_set"`
			} `json:"redis"`
		} `json:"code_store"`
	} `json:"cache"`
	LDAP struct {
		Addr        string `json:"addr"`
		DisableSync bool   `json:"disable_sync"`
	} `json:"ldap"`
}
