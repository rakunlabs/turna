package auth

import (
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/ldap"
)

func primaryRuntime(m *Auth) *ldap.Ldap {
	sources := m.ldapSources()
	if len(sources) == 0 {
		return nil
	}

	return sources[0].runtime
}

func TestLDAPRuntimeAddressOverride(t *testing.T) {
	t.Parallel()

	for _, override := range []string{"", "ldaps://ldap.site-b.example.com:636", "ldap://10.20.0.15:389"} {
		t.Run(override, func(t *testing.T) {
			cache := NewCache(nil)
			m := &Auth{cache: cache, LDAP: LDAPStatic{Addr: override}}

			if primaryRuntime(m) != nil {
				t.Fatal("address override must not enable LDAP without a stored config")
			}

			for version, storedAddr := range []string{"ldap://shared:389", "ldaps://updated:636"} {
				sn := &Snapshot{
					Version: uint64(version + 1),
					LDAP: []LDAPSettings{{
						Addr:       storedAddr,
						UserBaseDN: "ou=people,dc=example,dc=com",
					}},
				}
				cache.snap.Store(sn)
				runtime := primaryRuntime(m)
				want := storedAddr
				if override != "" {
					want = override
				}
				if runtime == nil || runtime.Addr != want {
					t.Fatalf("runtime = %+v, want address %q", runtime, want)
				}
				if runtime.UserBaseDN != sn.LDAP[0].UserBaseDN {
					t.Fatal("runtime must retain shared LDAP settings")
				}
				if sn.LDAP[0].Addr != storedAddr {
					t.Fatal("override mutated the shared snapshot")
				}
				if primaryRuntime(m) != runtime {
					t.Fatal("runtime should be reused within the same snapshot version")
				}
			}

			cache.snap.Store(&Snapshot{Version: 3})
			if primaryRuntime(m) != nil {
				t.Fatal("removing the stored config must disable LDAP even with an override")
			}
		})
	}
}
