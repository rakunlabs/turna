package auth

import (
	"slices"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/ldap"
)

func ldapTestSnapshot(users ...*data.User) *Snapshot {
	sn := &Snapshot{Users: map[string]*data.User{}, Alias: map[string]string{}}
	for _, u := range users {
		sn.Users[u.ID] = u
		sn.UserIDs = append(sn.UserIDs, u.ID)
		for _, a := range u.Alias {
			sn.Alias[a] = u.ID
		}
	}

	return sn
}

func TestLDAPUserIndexFind(t *testing.T) {
	t.Parallel()

	// synced before the mail attribute existed: only the uid is an alias
	byUID := &data.User{ID: "u1", Alias: []string{"jdoe"}, Details: map[string]any{"uid": "jdoe"}}
	byDN := &data.User{ID: "u2", Alias: []string{"old-uid"}, Details: map[string]any{ldapDNKey: "uid=renamed,ou=people,dc=example"}}
	service := &data.User{ID: "s1", Alias: []string{"svc"}, ServiceAccount: true}

	index := newLDAPUserIndex(ldapTestSnapshot(byUID, byDN, service))

	tests := []struct {
		name   string
		member string
		entry  *ldap.LdapUser
		want   string
	}{
		{
			name:   "email lookup resolves user stored under uid",
			member: "jdoe@example.com",
			entry:  &ldap.LdapUser{DN: "uid=jdoe,ou=people,dc=example", UID: "jdoe", Email: "jdoe@example.com"},
			want:   "u1",
		},
		{
			name:   "member key without entry",
			member: "jdoe",
			want:   "u1",
		},
		{
			name:   "dn match survives a uid rename",
			member: "renamed",
			entry:  &ldap.LdapUser{DN: "UID=renamed,ou=people,dc=example", UID: "renamed"},
			want:   "u2",
		},
		{
			name:   "service accounts are never matched",
			member: "svc",
			entry:  &ldap.LdapUser{UID: "svc"},
		},
		{
			name:   "unknown user",
			member: "nobody@example.com",
			entry:  &ldap.LdapUser{UID: "nobody", Email: "nobody@example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := index.find(tt.member, tt.entry)
			gotID := ""
			if got != nil {
				gotID = got.ID
			}

			if gotID != tt.want {
				t.Fatalf("find() = %q, want %q", gotID, tt.want)
			}
		})
	}
}

func TestLDAPMergeAliases(t *testing.T) {
	t.Parallel()

	other := &data.User{ID: "other", Alias: []string{"taken@example.com"}}

	tests := []struct {
		name          string
		user          *data.User
		entry         ldap.LdapUser
		want          []string
		wantConflicts []string
	}{
		{
			name:  "mail added later is appended",
			user:  &data.User{ID: "u", Alias: []string{"jdoe"}, Details: map[string]any{"uid": "jdoe", "email": ""}},
			entry: ldap.LdapUser{UID: "jdoe", Email: "jdoe@example.com"},
			want:  []string{"jdoe", "jdoe@example.com"},
		},
		{
			name:  "manual aliases are kept",
			user:  &data.User{ID: "u", Alias: []string{"jdoe", "john"}, Details: map[string]any{"uid": "jdoe"}},
			entry: ldap.LdapUser{UID: "jdoe", Email: "jdoe@example.com"},
			want:  []string{"jdoe", "john", "jdoe@example.com"},
		},
		{
			name:  "changed mail replaces the previously synced one",
			user:  &data.User{ID: "u", Alias: []string{"old@example.com", "jdoe"}, Details: map[string]any{"uid": "jdoe", "email": "old@example.com"}},
			entry: ldap.LdapUser{UID: "jdoe", Email: "new@example.com"},
			want:  []string{"jdoe", "new@example.com"},
		},
		{
			name:          "alias owned by another user is skipped",
			user:          &data.User{ID: "u", Alias: []string{"jdoe"}, Details: map[string]any{"uid": "jdoe"}},
			entry:         ldap.LdapUser{UID: "jdoe", Email: "taken@example.com"},
			want:          []string{"jdoe"},
			wantConflicts: []string{"taken@example.com"},
		},
		{
			name:  "no change",
			user:  &data.User{ID: "u", Alias: []string{"jdoe@example.com", "jdoe"}, Details: map[string]any{"uid": "jdoe", "email": "jdoe@example.com"}},
			entry: ldap.LdapUser{UID: "jdoe", Email: "jdoe@example.com"},
			want:  []string{"jdoe@example.com", "jdoe"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sn := ldapTestSnapshot(tt.user, other)

			got, conflicts := ldapMergeAliases(sn, tt.user, tt.entry)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("aliases = %v, want %v", got, tt.want)
			}
			if !slices.Equal(conflicts, tt.wantConflicts) {
				t.Fatalf("conflicts = %v, want %v", conflicts, tt.wantConflicts)
			}
		})
	}
}

func TestLDAPDetailsDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	in := map[string]any{"email": "", "custom": "keep"}
	out := ldapDetails(in, ldap.LdapUser{DN: "uid=jdoe,dc=example", UID: "jdoe", Email: "jdoe@example.com"})

	if in["email"] != "" {
		t.Fatal("input details were mutated")
	}
	if out["email"] != "jdoe@example.com" || out["custom"] != "keep" || out[ldapDNKey] != "uid=jdoe,dc=example" {
		t.Fatalf("unexpected details: %v", out)
	}
}
