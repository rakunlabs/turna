package basicauth

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	goauth "github.com/abbot/go-http-auth"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// minBcryptCost is the lowest bcrypt cost accepted.
const minBcryptCost = 10

// dummyHash is compared for unknown users so the response time does not
// reveal whether a username exists.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("turna-basic-auth-dummy"), minBcryptCost)

var errWeakHash = errors.New("legacy hash (APR1, md5crypt or {SHA}); use bcrypt or argon2id, or set allow_legacy_hashes")

type verifier func(password string) bool

func isBcrypt(hash string) bool {
	for _, prefix := range []string{"$2a$", "$2b$", "$2y$", "$2x$"} {
		if strings.HasPrefix(hash, prefix) {
			return true
		}
	}

	return false
}

func isLegacy(hash string) bool {
	return strings.HasPrefix(hash, "$apr1$") || strings.HasPrefix(hash, "$1$") || strings.HasPrefix(hash, "{SHA}")
}

// newVerifier parses a stored hash once at startup.
func newVerifier(hash string, allowLegacy bool) (verifier, error) {
	switch {
	case isBcrypt(hash):
		cost, err := bcrypt.Cost([]byte(hash))
		if err != nil {
			return nil, fmt.Errorf("invalid bcrypt hash: %w", err)
		}
		if cost < minBcryptCost {
			return nil, fmt.Errorf("bcrypt cost %d is below %d", cost, minBcryptCost)
		}

		h := []byte(hash)

		return func(password string) bool {
			return bcrypt.CompareHashAndPassword(h, []byte(password)) == nil
		}, nil
	case strings.HasPrefix(hash, "$argon2id$"):
		params, err := parseArgon2id(hash)
		if err != nil {
			return nil, err
		}

		return params.verify, nil
	case isLegacy(hash):
		if !allowLegacy {
			return nil, errWeakHash
		}

		return func(password string) bool {
			return goauth.CheckSecret(password, hash)
		}, nil
	}

	return nil, errors.New("unsupported hash format; plain text passwords are not accepted")
}

type argon2idHash struct {
	time    uint32
	memory  uint32
	threads uint8
	salt    []byte
	key     []byte
}

// parseArgon2id parses the PHC string format
// $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash> (unpadded base64).
func parseArgon2id(hash string) (*argon2idHash, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, errors.New("invalid argon2id hash")
	}

	if parts[2] != "v=19" {
		return nil, fmt.Errorf("unsupported argon2id version %q", parts[2])
	}

	h := &argon2idHash{}
	for _, kv := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, errors.New("invalid argon2id parameters")
		}

		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid argon2id parameter %q: %w", k, err)
		}

		switch k {
		case "m":
			h.memory = uint32(n)
		case "t":
			h.time = uint32(n)
		case "p":
			if n > 255 {
				return nil, errors.New("argon2id parallelism must be at most 255")
			}
			h.threads = uint8(n)
		}
	}

	if h.memory < 19*1024 || h.time < 1 || h.threads < 1 {
		return nil, errors.New("argon2id parameters below m=19456,t=1,p=1")
	}

	var err error
	if h.salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return nil, fmt.Errorf("invalid argon2id salt: %w", err)
	}
	if h.key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return nil, fmt.Errorf("invalid argon2id hash: %w", err)
	}
	if len(h.salt) < 8 || len(h.key) < 16 {
		return nil, errors.New("argon2id salt or hash too short")
	}

	return h, nil
}

func (h *argon2idHash) verify(password string) bool {
	key := argon2.IDKey([]byte(password), h.salt, h.time, h.memory, h.threads, uint32(len(h.key)))

	return subtle.ConstantTimeCompare(key, h.key) == 1
}
