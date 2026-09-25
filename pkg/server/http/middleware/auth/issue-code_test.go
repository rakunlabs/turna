package auth

import (
	"testing"

	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
)

// A code issued through IssueAuthorizationCode must be redeemable through the
// same store the token endpoint reads.
func TestIssueAuthorizationCodeUsesTokenEndpointStore(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{})

	m := &Auth{cache: cache, Cache: CacheStatic{CodeStore: CodeStoreSettings{Active: "memory"}}}

	codeID, err := m.IssueAuthorizationCode(t.Context(), oauth2store.Code{Alias: "u", ClientID: "c", RedirectURI: "https://app/cb"})
	if err != nil {
		t.Fatal(err)
	}

	store, err := m.codeStoreRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, ok, err := store.TakeCode(t.Context(), "code_"+codeID)
	if err != nil || !ok {
		t.Fatalf("code not in token endpoint store: ok=%v err=%v", ok, err)
	}
	code, _ := oauth2store.Decode[oauth2store.Code](raw)
	if code.Alias != "u" || code.ClientID != "c" {
		t.Fatalf("code = %+v", code)
	}
}
