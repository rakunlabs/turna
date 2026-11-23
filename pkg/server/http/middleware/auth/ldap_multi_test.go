package auth

import (
	"errors"
	"net"
	"testing"

	ber "github.com/go-asn1-ber/asn1-ber"
	goldap "github.com/go-ldap/ldap/v3"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
)

// fakeDirectory is a minimal LDAP server that only answers bind requests:
// success when the DN/password pair is known, invalid credentials otherwise.
type fakeDirectory struct {
	addr  string
	users map[string]string
}

func newFakeDirectory(t *testing.T, users map[string]string) *fakeDirectory {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	d := &fakeDirectory{addr: "ldap://" + ln.Addr().String(), users: users}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serve(conn)
		}
	}()

	return d
}

func (d *fakeDirectory) serve(conn net.Conn) {
	defer conn.Close()

	for {
		packet, err := ber.ReadPacket(conn)
		if err != nil {
			return
		}

		msgID := packet.Children[0].Value
		req := packet.Children[1]
		if req.Tag != goldap.ApplicationBindRequest {
			return
		}

		dn := req.Children[1].Value.(string)
		password := string(req.Children[2].Data.Bytes())

		code := int64(goldap.LDAPResultInvalidCredentials)
		if want, ok := d.users[dn]; ok && want == password {
			code = goldap.LDAPResultSuccess
		}

		resp := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
		resp.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, msgID, ""))
		res := ber.Encode(ber.ClassApplication, ber.TypeConstructed, goldap.ApplicationBindResponse, nil, "")
		res.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, code, ""))
		res.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
		res.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
		resp.AppendChild(res)
		if _, err := conn.Write(resp.Bytes()); err != nil {
			return
		}
	}
}

func multiLDAPAuth(t *testing.T, cfgs ...LDAPSettings) *Auth {
	t.Helper()

	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{Version: 1, LDAP: cfgs})

	return &Auth{cache: cache}
}

func TestLDAPCheckPasswordMultipleSources(t *testing.T) {
	t.Parallel()

	a := newFakeDirectory(t, map[string]string{"uid=alice,ou=a": "pa"})
	b := newFakeDirectory(t, map[string]string{"uid=bob,ou=b": "pb", "uid=alice,ou=b": "other"})

	m := multiLDAPAuth(t,
		LDAPSettings{ID: "a", Addr: a.addr, UserBaseDN: "ou=a"},
		LDAPSettings{ID: "b", Addr: b.addr, UserBaseDN: "ou=b"},
	)

	tests := []struct {
		name       string
		user       *data.User
		username   string
		password   string
		wantSource string
		wantOK     bool
	}{
		{name: "new user found in first", username: "alice", password: "pa", wantSource: "a", wantOK: true},
		{name: "new user falls through to second", username: "bob", password: "pb", wantSource: "b", wantOK: true},
		{name: "new user unknown everywhere", username: "carol", password: "x"},
		{
			name:     "pinned user only asks its source",
			user:     &data.User{Details: map[string]any{ldapSourceKey: "a"}},
			username: "alice", password: "other", wantSource: "a",
		},
		{
			name:     "pinned to second source",
			user:     &data.User{Details: map[string]any{ldapSourceKey: "b"}},
			username: "alice", password: "other", wantSource: "b", wantOK: true,
		},
		{
			name:     "legacy user keeps its DN",
			user:     &data.User{Details: map[string]any{ldapDNKey: "UID=alice,OU=a"}},
			username: "alice", password: "other",
		},
		{
			name:     "removed source falls back",
			user:     &data.User{Details: map[string]any{ldapSourceKey: "gone"}},
			username: "bob", password: "pb", wantSource: "b", wantOK: true,
		},
	}

	for _, tt := range tests {
		source, ok, err := m.LdapCheckPassword(tt.user, tt.username, tt.password)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", tt.name, err)
		}
		if ok != tt.wantOK || (ok && source != tt.wantSource) {
			t.Fatalf("%s: got source=%q ok=%v, want source=%q ok=%v", tt.name, source, ok, tt.wantSource, tt.wantOK)
		}
	}
}

func TestLDAPCheckPasswordDisableRegister(t *testing.T) {
	t.Parallel()

	a := newFakeDirectory(t, map[string]string{"uid=alice,ou=a": "pa"})
	b := newFakeDirectory(t, map[string]string{"uid=alice,ou=b": "pb"})

	m := multiLDAPAuth(t,
		LDAPSettings{ID: "a", Addr: a.addr, UserBaseDN: "ou=a", DisableRegister: true},
		LDAPSettings{ID: "b", Addr: b.addr, UserBaseDN: "ou=b"},
	)

	// unknown users skip a config that may not create them
	if _, ok, err := m.LdapCheckPassword(nil, "alice", "pa"); ok || err != nil {
		t.Fatalf("new user must not log in through a no-register config: ok=%v err=%v", ok, err)
	}
	if source, ok, _ := m.LdapCheckPassword(nil, "alice", "pb"); !ok || source != "b" {
		t.Fatalf("got source=%q ok=%v, want b", source, ok)
	}

	// existing users of that config still log in
	user := &data.User{Details: map[string]any{ldapSourceKey: "a"}}
	if source, ok, _ := m.LdapCheckPassword(user, "alice", "pa"); !ok || source != "a" {
		t.Fatalf("got source=%q ok=%v, want a", source, ok)
	}
}

func TestLDAPCheckPasswordReplacedDirectory(t *testing.T) {
	t.Parallel()

	// the old directory (ou=old) was removed and replaced by a new one with
	// another base DN; users synced from it must still log in
	newDir := newFakeDirectory(t, map[string]string{"uid=alice,dc=new": "pa"})
	other := newFakeDirectory(t, map[string]string{"uid=bob,dc=other": "pb"})

	m := multiLDAPAuth(t,
		LDAPSettings{ID: "other", Addr: other.addr, UserBaseDN: "dc=other"},
		LDAPSettings{ID: "new", Addr: newDir.addr, UserBaseDN: "dc=new"},
	)

	orphan := &data.User{Details: map[string]any{ldapSourceKey: "old", ldapDNKey: "uid=alice,dc=old"}}
	if source, ok, err := m.LdapCheckPassword(orphan, "alice", "pa"); err != nil || !ok || source != "new" {
		t.Fatalf("orphaned user: got source=%q ok=%v err=%v, want new", source, ok, err)
	}

	legacy := &data.User{Details: map[string]any{ldapDNKey: "uid=alice,dc=old"}}
	if source, ok, err := m.LdapCheckPassword(legacy, "alice", "pa"); err != nil || !ok || source != "new" {
		t.Fatalf("legacy user: got source=%q ok=%v err=%v, want new", source, ok, err)
	}

	// while a config still serves the stored DN, other directories stay out
	guarded := &data.User{Details: map[string]any{ldapDNKey: "uid=bob,dc=other"}}
	if _, ok, _ := m.LdapCheckPassword(guarded, "bob", "pa"); ok {
		t.Fatal("a user whose DN is still served must not be taken over by another directory")
	}
}

func TestLDAPUserKey(t *testing.T) {
	t.Parallel()

	m := multiLDAPAuth(t,
		LDAPSettings{ID: "a", Addr: "ldap://a:389"},
		LDAPSettings{ID: "b", Key: "shared", Addr: "ldap://b:389"},
		LDAPSettings{ID: "c", Key: "shared", Addr: "ldap://c:389"},
	)

	for _, tt := range []struct{ stored, want string }{
		{stored: "a", want: "a"},
		{stored: "shared", want: "shared"},
		// a config id from before keys resolves to the config's key
		{stored: "c", want: "shared"},
		{stored: "gone", want: ""},
		{stored: "", want: ""},
	} {
		user := &data.User{Details: map[string]any{ldapSourceKey: tt.stored}}
		if got := m.ldapUserKey(user); got != tt.want {
			t.Fatalf("stored %q: got %q, want %q", tt.stored, got, tt.want)
		}
	}
}

func TestLDAPCheckPasswordUnreachableSource(t *testing.T) {
	t.Parallel()

	b := newFakeDirectory(t, map[string]string{"uid=bob,ou=b": "pb"})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	down := "ldap://" + ln.Addr().String()
	_ = ln.Close()

	m := multiLDAPAuth(t,
		LDAPSettings{ID: "down", Addr: down, UserBaseDN: "ou=a"},
		LDAPSettings{ID: "b", Addr: b.addr, UserBaseDN: "ou=b"},
	)

	// a reachable source still accepts its users
	if source, ok, err := m.LdapCheckPassword(nil, "bob", "pb"); err != nil || !ok || source != "b" {
		t.Fatalf("got source=%q ok=%v err=%v", source, ok, err)
	}

	// a miss is not reported as a wrong password while a source is down
	if _, ok, err := m.LdapCheckPassword(nil, "bob", "wrong"); ok || err == nil {
		t.Fatalf("expected an error, got ok=%v err=%v", ok, err)
	}
}

func TestLDAPSourcesOrderAndOverride(t *testing.T) {
	t.Parallel()

	cache := NewCache(nil)
	m := &Auth{cache: cache, LDAP: LDAPStatic{Addr: "ldap://override:389"}}

	cache.snap.Store(&Snapshot{Version: 1, LDAP: []LDAPSettings{
		{ID: "a", Addr: "ldap://a:389"},
		{ID: "b", Addr: "ldap://b:389"},
	}})

	sources := m.ldapSources()
	if len(sources) != 2 || sources[0].runtime.Addr != "ldap://override:389" || sources[1].runtime.Addr != "ldap://b:389" {
		t.Fatalf("override must only apply to the primary: %+v", sources)
	}

	// an unrelated snapshot bump keeps both runtimes
	cache.snap.Store(&Snapshot{Version: 2, LDAP: []LDAPSettings{
		{ID: "a", Addr: "ldap://a:389"},
		{ID: "b", Addr: "ldap://b:389"},
	}})
	if again := m.ldapSources(); again[0] != sources[0] || again[1] != sources[1] {
		t.Fatal("unchanged configs must reuse their runtimes")
	}
}

func TestLDAPInstanceConfigFilter(t *testing.T) {
	t.Parallel()

	b := newFakeDirectory(t, map[string]string{"uid=alice,ou=b": "pb"})

	cfgs := []LDAPSettings{
		{ID: "a", Addr: "ldap://stored-a:389", UserBaseDN: "ou=a"},
		{ID: "b", Addr: "ldap://stored-b:389", UserBaseDN: "ou=b"},
	}

	for _, static := range []LDAPStatic{
		{Configs: []string{"b"}, Addrs: map[string]string{"b": b.addr}},
		{DisableConfigs: []string{"a"}, Addr: b.addr},
	} {
		cache := NewCache(nil)
		cache.snap.Store(&Snapshot{Version: 1, LDAP: cfgs})
		m := &Auth{cache: cache, LDAP: static}

		sources := m.ldapSources()
		if len(sources) != 1 || sources[0].id != "b" || sources[0].runtime.Addr != b.addr {
			t.Fatalf("%+v: only b with its override must remain: %+v", static, sources)
		}

		// a single source is asked directly, even for a legacy DN elsewhere
		if source, ok, err := m.LdapCheckPassword(nil, "alice", "pb"); err != nil || !ok || source != "b" {
			t.Fatalf("got source=%q ok=%v err=%v", source, ok, err)
		}

		// users of a config disabled here are not adopted by another one
		user := &data.User{Details: map[string]any{ldapSourceKey: "a"}}
		if _, ok, err := m.LdapCheckPassword(user, "alice", "pb"); ok || !errors.Is(err, errLDAPSourceExcluded) {
			t.Fatalf("user of a disabled config: ok=%v err=%v", ok, err)
		}
	}
}

func TestLDAPSharedKey(t *testing.T) {
	t.Parallel()

	// finops and finops-gcp are separate directories with the same people
	finops := newFakeDirectory(t, map[string]string{"uid=alice,ou=finops": "pa"})
	gcp := newFakeDirectory(t, map[string]string{"uid=alice,ou=gcp": "pa", "uid=bob,ou=gcp": "pb"})

	m := multiLDAPAuth(t,
		LDAPSettings{ID: "finops", Key: "finops", Addr: finops.addr, UserBaseDN: "ou=finops"},
		LDAPSettings{ID: "finops-gcp", Key: "finops", Addr: gcp.addr, UserBaseDN: "ou=gcp", DisableSync: true},
	)

	// either directory accepts a user of the key; the key is returned, not the id
	for _, stored := range []string{"finops", "finops-gcp"} {
		user := &data.User{Details: map[string]any{ldapSourceKey: stored}}
		if source, ok, err := m.LdapCheckPassword(user, "bob", "pb"); err != nil || !ok || source != "finops" {
			t.Fatalf("stored %q: source=%q ok=%v err=%v", stored, source, ok, err)
		}
	}

	// a wrong password keeps the key
	user := &data.User{Details: map[string]any{ldapSourceKey: "finops"}}
	if source, ok, err := m.LdapCheckPassword(user, "alice", "wrong"); err != nil || ok || source != "finops" {
		t.Fatalf("wrong password: source=%q ok=%v err=%v", source, ok, err)
	}

	// one config per key syncs: the one with sync enabled
	targets := ldapSyncTargets(m.ldapSources(), true)
	if len(targets) != 1 || targets[0].id != "finops" {
		t.Fatalf("sync targets: %+v", targets)
	}
}

func TestLDAPSharedKeyDisabledHere(t *testing.T) {
	t.Parallel()

	gcp := newFakeDirectory(t, map[string]string{"uid=alice,ou=gcp": "pa"})

	cfgs := []LDAPSettings{
		{ID: "finops", Key: "finops", Addr: "ldap://unreachable:389", UserBaseDN: "ou=finops"},
		{ID: "finops-gcp", Key: "finops", Addr: gcp.addr, UserBaseDN: "ou=gcp"},
	}

	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{Version: 1, LDAP: cfgs})
	m := &Auth{cache: cache, LDAP: LDAPStatic{DisableConfigs: []string{"finops"}}}

	// one config of the key is still enabled here, so the user logs in
	user := &data.User{Details: map[string]any{ldapSourceKey: "finops"}}
	if source, ok, err := m.LdapCheckPassword(user, "alice", "pa"); err != nil || !ok || source != "finops" {
		t.Fatalf("source=%q ok=%v err=%v", source, ok, err)
	}

	// a user still holding the id of the disabled config logs in through the
	// enabled one too
	legacy := &data.User{Details: map[string]any{ldapSourceKey: "finops"}}
	if source, ok, err := m.LdapCheckPassword(legacy, "alice", "pa"); err != nil || !ok || source != "finops" {
		t.Fatalf("legacy id: source=%q ok=%v err=%v", source, ok, err)
	}

	// the disabled directory is never asked: a wrong password is a clean
	// mismatch, not an error from the unreachable finops address
	if _, ok, err := m.LdapCheckPassword(user, "alice", "wrong"); ok || err != nil {
		t.Fatalf("disabled config was contacted: ok=%v err=%v", ok, err)
	}

	// with every config of the key disabled the user is refused
	m = &Auth{cache: cache, LDAP: LDAPStatic{Configs: []string{"none"}}}
	cache.snap.Store(&Snapshot{Version: 2, LDAP: append(cfgs, LDAPSettings{ID: "none", Addr: gcp.addr})})
	if _, ok, err := m.LdapCheckPassword(user, "alice", "pa"); ok || !errors.Is(err, errLDAPSourceExcluded) {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestLDAPOwns(t *testing.T) {
	t.Parallel()

	if !ldapOwns("", "a", true) || ldapOwns("", "b", false) {
		t.Fatal("legacy users belong to the primary only")
	}
	if ldapOwns("b", "a", true) || !ldapOwns("b", "b", false) {
		t.Fatal("owned users belong to their source only")
	}
}
