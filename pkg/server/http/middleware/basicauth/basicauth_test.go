package basicauth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	apr1Pass  = "$apr1$JMWtQHoL$g/5ey5x7psJM7htuB6OEy0"
	md5Pass   = "$1$abcdefgh$1s0bv7.OjNizlrMAOIQD7."
	shaPass   = "{SHA}nU4eI71bcnBGqeO0t9tXvY1u5oQ="
	bcrypt2y  = "$2y$10$kcIcZZWz2YciwMVS9UYJaurYifLlMfzZ6WHL/SoDtTvENTQMi8ii2"
	plainPass = "pass"
)

func argon2idHashFor(t *testing.T, password string, m, tm uint32, p uint8) string {
	t.Helper()

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	key := argon2.IDKey([]byte(password), salt, tm, m, p, 32)

	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", m, tm, p,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

func serve(t *testing.T, b *BasicAuth, header string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()

	mw, err := b.Middleware("test")
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	var got *http.Request
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r }))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-User", "spoofed")
	if header != "" {
		r.Header.Set("Authorization", header)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	return rec, got
}

func basicHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func TestBasicAuth(t *testing.T) {
	argonHash := argon2idHashFor(t, "s3cret:with:colons", 19*1024, 2, 1)

	b := &BasicAuth{
		Users:        []string{"admin:" + bcrypt2y, "svc:" + argonHash},
		RemoveHeader: true,
	}

	cases := map[string]struct {
		header string
		user   string
	}{
		"bcrypt":               {header: basicHeader("admin", plainPass), user: "admin"},
		"argon2id with colons": {header: basicHeader("svc", "s3cret:with:colons"), user: "svc"},
		"lowercase scheme":     {header: "basic " + base64.StdEncoding.EncodeToString([]byte("admin:pass")), user: "admin"},
		"wrong password":       {header: basicHeader("admin", "nope")},
		"unknown user":         {header: basicHeader("ghost", plainPass)},
		"missing header":       {},
		"bearer scheme":        {header: "Bearer abc"},
		"scheme prefix only":   {header: "Basicx" + base64.StdEncoding.EncodeToString([]byte("admin:pass"))},
		"no colon in cred":     {header: "Basic " + base64.StdEncoding.EncodeToString([]byte("adminpass"))},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec, got := serve(t, b, tc.header)

			if tc.user == "" {
				if rec.Code != http.StatusUnauthorized || got != nil {
					t.Fatalf("status = %d, next called = %v", rec.Code, got != nil)
				}
				if !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), `Basic realm="Restricted"`) {
					t.Errorf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
				}

				return
			}

			if got == nil {
				t.Fatalf("status = %d, want next", rec.Code)
			}
			if u := got.Header.Get("X-User"); u != tc.user {
				t.Errorf("X-User = %q, want %q", u, tc.user)
			}
			if got.Header.Get("Authorization") != "" {
				t.Error("remove_header must drop Authorization")
			}
		})
	}

	t.Run("invalid base64 is a bad request", func(t *testing.T) {
		rec, _ := serve(t, b, "Basic !!!")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestBasicAuthHashPolicy(t *testing.T) {
	lowCost, err := bcrypt.GenerateFromPassword([]byte(plainPass), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	rejected := map[string]string{
		"apr1":            apr1Pass,
		"md5crypt":        md5Pass,
		"sha1":            shaPass,
		"plain text":      plainPass,
		"low bcrypt cost": string(lowCost),
		"weak argon2id":   argon2idHashFor(t, plainPass, 1024, 1, 1),
		"broken argon2id": "$argon2id$v=19$m=65536,t=3,p=4$!!$!!",
	}
	for name, hash := range rejected {
		if _, err := (&BasicAuth{Users: []string{"u:" + hash}}).Middleware("test"); err == nil {
			t.Errorf("%s: middleware must refuse the hash", name)
		}
	}

	if _, err := (&BasicAuth{Users: []string{"no-hash"}}).Middleware("test"); err == nil {
		t.Error("entry without a hash must be refused")
	}

	t.Run("allow_legacy_hashes", func(t *testing.T) {
		for _, hash := range []string{apr1Pass, md5Pass, shaPass} {
			b := &BasicAuth{Users: []string{"old:" + hash}, AllowLegacyHashes: true}
			if _, got := serve(t, b, basicHeader("old", plainPass)); got == nil {
				t.Errorf("legacy hash %s must verify when allowed", hash[:5])
			}
			if rec, _ := serve(t, b, basicHeader("old", "nope")); rec.Code != http.StatusUnauthorized {
				t.Errorf("legacy hash %s accepted a wrong password", hash[:5])
			}
		}

		if _, err := (&BasicAuth{Users: []string{"u:" + plainPass}, AllowLegacyHashes: true}).Middleware("test"); err == nil {
			t.Error("plain text must stay refused")
		}
	})
}

func TestBasicAuthHeaderField(t *testing.T) {
	b := &BasicAuth{Users: []string{"admin:" + bcrypt2y}, HeaderField: "X-Remote-User", Realm: "Ops"}

	_, got := serve(t, b, basicHeader("admin", plainPass))
	if got == nil || got.Header.Get("X-Remote-User") != "admin" {
		t.Fatal("custom header field not set")
	}
	if got.Header.Get("Authorization") == "" {
		t.Error("Authorization must be kept without remove_header")
	}

	rec, _ := serve(t, b, "")
	if !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), `Basic realm="Ops"`) {
		t.Errorf("realm = %q", rec.Header().Get("WWW-Authenticate"))
	}
}
