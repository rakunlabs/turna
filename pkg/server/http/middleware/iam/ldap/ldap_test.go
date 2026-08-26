package ldap

import "testing"

func TestLDAPUserFilterEscapesValues(t *testing.T) {
	t.Parallel()

	got := ldapUserFilter([]string{"*)(objectClass=*"})
	want := `(|(uid=\2a\29\28objectClass=\2a)(mail=\2a\29\28objectClass=\2a))`
	if got != want {
		t.Fatalf("ldapUserFilter() = %q, want %q", got, want)
	}
}

func TestLDAPUserDNEscapesRDNValue(t *testing.T) {
	t.Parallel()

	got := ldapUserDN(`alice,ou=admins`, `ou=users,dc=example,dc=com`)
	want := `uid=alice\,ou=admins,ou=users,dc=example,dc=com`
	if got != want {
		t.Fatalf("ldapUserDN() = %q, want %q", got, want)
	}
}

func TestLDAPGroupMemberFilterEscapesUserDN(t *testing.T) {
	t.Parallel()

	got := ldapGroupMemberFilter("(objectClass=groupOfUniqueNames)", `uid=alice*)(objectClass=*,ou=people,dc=example`)
	want := `(&(objectClass=groupOfUniqueNames)(uniqueMember=uid=alice\2a\29\28objectClass=\2a,ou=people,dc=example))`
	if got != want {
		t.Fatalf("ldapGroupMemberFilter() = %q, want %q", got, want)
	}
}
