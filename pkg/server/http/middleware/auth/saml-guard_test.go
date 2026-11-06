package auth

import "testing"

func TestCheckSAMLAssertions(t *testing.T) {
	const ns = `xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"`

	for name, tt := range map[string]struct {
		doc string
		ok  bool
	}{
		"valid":          {`<samlp:Response ` + ns + `><saml:Assertion><saml:Issuer>idp</saml:Issuer><saml:Subject><saml:NameID>u</saml:NameID></saml:Subject></saml:Assertion></samlp:Response>`, true},
		"no issuer":      {`<samlp:Response ` + ns + `><saml:Issuer>idp</saml:Issuer><saml:Assertion><saml:Subject><saml:NameID>u</saml:NameID></saml:Subject></saml:Assertion></samlp:Response>`, false},
		"foreign nameid": {`<samlp:Response ` + ns + ` xmlns:x="urn:evil"><saml:Assertion><saml:Issuer>idp</saml:Issuer><saml:Subject><x:NameID>admin</x:NameID></saml:Subject></saml:Assertion></samlp:Response>`, false},
		"encrypted only": {`<samlp:Response ` + ns + `><saml:EncryptedAssertion>...</saml:EncryptedAssertion></samlp:Response>`, true},
	} {
		if err := checkSAMLAssertions([]byte(tt.doc)); (err == nil) != tt.ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
