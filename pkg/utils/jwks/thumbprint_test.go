package jwks

import (
	"encoding/json"
	"testing"
)

func TestThumbprintRFC7638(t *testing.T) {
	// RFC 7638 §3.1 example
	raw := json.RawMessage(`{"kty":"RSA","n":"0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw","e":"AQAB","alg":"RS256","kid":"2011-04-29"}`)

	jwk, err := ParsePublicJWK(raw)
	if err != nil {
		t.Fatal(err)
	}
	if jwk.Thumbprint != "NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs" {
		t.Fatalf("thumbprint = %s", jwk.Thumbprint)
	}

	if _, err := ParsePublicJWK(json.RawMessage(`{"kty":"EC","crv":"P-256","x":"a","y":"b","d":"c"}`)); err == nil {
		t.Fatal("private key members must be refused")
	}
	if _, err := ParsePublicJWK(json.RawMessage(`{"kty":"oct","k":"c2VjcmV0"}`)); err == nil {
		t.Fatal("symmetric keys must be refused")
	}
}
