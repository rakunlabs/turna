package cert

import "testing"

func TestGenerateCertificateCacheByOptions(t *testing.T) {
	a, err := GenerateCertificateCache(WithDNSNames("a.example.com"))
	if err != nil {
		t.Fatal(err)
	}

	again, err := GenerateCertificateCache(WithDNSNames("a.example.com"))
	if err != nil {
		t.Fatal(err)
	}

	b, err := GenerateCertificateCache(WithDNSNames("b.example.com"))
	if err != nil {
		t.Fatal(err)
	}

	if a != again {
		t.Error("same options generated a new certificate")
	}

	if a == b {
		t.Error("other options returned the cached certificate")
	}
}
