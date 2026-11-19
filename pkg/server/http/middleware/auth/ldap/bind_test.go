package ldap

import (
	"net"
	"testing"

	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/go-ldap/ldap/v3"
)

// fakeBindServer answers one bind request with success and reports the DN
// and password the client sent.
func fakeBindServer(t *testing.T, server net.Conn) <-chan [2]string {
	t.Helper()

	got := make(chan [2]string, 1)
	go func() {
		defer server.Close()

		packet, err := ber.ReadPacket(server)
		if err != nil {
			close(got)

			return
		}

		msgID := packet.Children[0].Value
		req := packet.Children[1]
		got <- [2]string{req.Children[1].Value.(string), string(req.Children[2].Data.Bytes())}

		resp := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
		resp.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, msgID, ""))
		res := ber.Encode(ber.ClassApplication, ber.TypeConstructed, ldap.ApplicationBindResponse, nil, "")
		res.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, int64(0), ""))
		res.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
		res.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
		resp.AppendChild(res)
		_, _ = server.Write(resp.Bytes())
	}()

	return got
}

func TestLDAPBindModes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		bind     LdapBind
		wantDN   string
		wantPass string
	}{
		{name: "default simple", bind: LdapBind{Simple: Simple{Username: "cn=svc,dc=example", Password: "secret"}}, wantDN: "cn=svc,dc=example", wantPass: "secret"},
		{name: "empty dn with password", bind: LdapBind{Mode: BindSimple, Simple: Simple{Password: "any"}}, wantPass: "any"},
		{name: "anonymous ignores credentials", bind: LdapBind{Mode: BindAnonymous, Simple: Simple{Username: "cn=svc", Password: "secret"}}},
		{name: "unauthenticated", bind: LdapBind{Mode: BindUnauthenticated, Simple: Simple{Username: "cn=svc,dc=example", Password: "secret"}}, wantDN: "cn=svc,dc=example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, server := net.Pipe()
			got := fakeBindServer(t, server)

			conn := ldap.NewConn(client, false)
			conn.Start()
			defer conn.Close()

			l := &Ldap{Bind: tc.bind}
			if err := l.bind(conn); err != nil {
				t.Fatalf("bind() error = %v", err)
			}

			sent := <-got
			if sent[0] != tc.wantDN || sent[1] != tc.wantPass {
				t.Fatalf("sent dn=%q password=%q, want dn=%q password=%q", sent[0], sent[1], tc.wantDN, tc.wantPass)
			}
		})
	}
}

func TestLDAPBindModeErrors(t *testing.T) {
	t.Parallel()

	for _, bind := range []LdapBind{
		{Mode: "kerberos"},
		{Mode: BindUnauthenticated},
		{Mode: BindSimple, Simple: Simple{Username: "cn=svc"}},
	} {
		if err := (&Ldap{Bind: bind}).bind(nil); err == nil {
			t.Fatalf("bind(%+v) should fail before contacting the server", bind)
		}
	}
}
