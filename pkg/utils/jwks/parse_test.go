package jwks

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestParseEC(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	point, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	enc := base64.RawURLEncoding.EncodeToString
	k := rawJWK{KID: "k", Kty: "EC", Crv: "P-256", X: enc(point[1:33]), Y: enc(point[33:])}

	pub, err := parseEC(k)
	if err != nil {
		t.Fatal(err)
	}

	if !pub.Equal(&key.PublicKey) {
		t.Fatal("parsed key differs")
	}

	short := rawJWK{KID: "k", Kty: "EC", Crv: "P-256", X: enc([]byte{1}), Y: enc([]byte{2})}
	if _, err := parseEC(short); err == nil {
		t.Fatal("point not on the curve accepted")
	}

	bad := k
	bad.Y = enc(point[1:33])
	if _, err := parseEC(bad); err == nil {
		t.Fatal("point not on the curve accepted")
	}
}
