package store

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/ada/utils/securecookie"
	"github.com/rakunlabs/ada/utils/sessions"
	storeredis "github.com/rakunlabs/cache/store/redis"
	"github.com/redis/go-redis/v9"
)

type Redis struct {
	Address  string `cfg:"address"`
	Username string `cfg:"username"`
	Password string `cfg:"password" log:"-"`
	// Cluster forces Redis Cluster mode for this address. When false, cluster
	// mode is detected automatically.
	Cluster bool      `cfg:"cluster"`
	TLS     TLSConfig `cfg:"tls"`

	KeyPrefix string `cfg:"key_prefix"`
	// SessionKey signs the session ID cookie. If empty, a random key is generated.
	SessionKey string `cfg:"session_key" log:"-"`
	// Compat selects the storage format. Empty is the current format, "v1" is
	// the v0.8.x format, "mixed" reads both and writes the current one.
	Compat string `cfg:"compat"`
}

type RedisStore struct {
	client    redis.UniversalClient
	keyPrefix string
	codec     *securecookie.Codec
	options   sessions.Options
	compat    string
}

func (r Redis) Store(ctx context.Context, opts sessions.Options) (*RedisStore, error) {
	// Validate before dialing so a bad value fails fast at startup.
	if !ValidCompat(r.Compat) {
		return nil, fmt.Errorf("redis store compat %q is unknown, expected one of %q, %q, %q", r.Compat, CompatNone, CompatV1, CompatMixed)
	}

	client, err := storeredis.NewClient(ctx, r.connConfig())
	if err != nil {
		return nil, err
	}

	if r.KeyPrefix == "" {
		r.KeyPrefix = "session_"
	}

	sessionKey := []byte(r.SessionKey)
	if len(sessionKey) == 0 {
		sessionKey = securecookie.GenerateRandomKey(32)
	}

	codec := securecookie.New(sessionKey, nil)
	codec.SetMaxAge(opts.MaxAge)

	return &RedisStore{
		client:    client,
		keyPrefix: r.KeyPrefix,
		codec:     codec,
		options:   opts,
		compat:    r.Compat,
	}, nil
}

// connConfig keeps the single-address session config while sharing the
// connection and cluster detection logic with the other Redis stores.
func (r Redis) connConfig() storeredis.ConnConfig {
	var address []string
	if strings.TrimSpace(r.Address) != "" {
		address = []string{r.Address}
	}

	return storeredis.ConnConfig{
		Address:  address,
		Username: r.Username,
		Password: r.Password,
		Cluster:  r.Cluster,
		TLS:      r.TLS,
	}
}

func (s *RedisStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return s.New(r, name)
}

func (s *RedisStore) New(r *http.Request, name string) (*sessions.Session, error) {
	session := newSession(s, name, s.options)
	cookie, err := r.Cookie(name)
	if err != nil || cookie.Value == "" {
		return session, nil
	}

	sessionID, err := s.sessionID(name, cookie.Value)
	if err != nil {
		return session, err
	}

	// In v1 the ID is kept even when nothing is stored yet, so Save reuses it
	// instead of rotating the cookie. An older turna sharing this cookie would
	// otherwise adopt the rotated value and both sides would keep invalidating
	// each other's session.
	if s.compat == CompatV1 {
		session.ID = sessionID
	}

	values, err := s.load(r.Context(), sessionID)
	if err != nil {
		return session, err
	}

	session.ID = sessionID
	session.Values = values
	session.IsNew = false

	return session, nil
}

// sessionID resolves the session ID carried by the cookie for the configured
// compatibility mode. A legacy cookie holds the raw ID and has no signature.
func (s *RedisStore) sessionID(name, value string) (string, error) {
	if s.compat == CompatV1 {
		return value, nil
	}

	var sessionID string
	if err := s.codec.Decode(name, value, &sessionID); err != nil {
		if s.compat == CompatMixed {
			return value, nil
		}

		return "", err
	}

	return sessionID, nil
}

// cookieValue renders the cookie payload for the session ID. The v1 format
// stores the raw ID so an older turna can read it.
func (s *RedisStore) cookieValue(name, sessionID string) (string, error) {
	if s.compat == CompatV1 {
		return sessionID, nil
	}

	return s.codec.Encode(name, sessionID)
}

func (s *RedisStore) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	if session.Options.MaxAge < 0 {
		if session.ID != "" {
			_ = s.client.Del(r.Context(), s.redisKey(session.ID)).Err()
		}

		setSessionCookie(w, session.Name(), "", session.Options)

		return nil
	}

	if session.ID == "" {
		sessionID, err := generateRandomKey()
		if err != nil {
			return fmt.Errorf("failed to generate session ID: %w", err)
		}
		session.ID = sessionID
	}

	if err := s.save(r.Context(), session.ID, session.Values, session.Options.MaxAge); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	cookieValue, err := s.cookieValue(session.Name(), session.ID)
	if err != nil {
		return err
	}

	setSessionCookie(w, session.Name(), cookieValue, session.Options)

	return nil
}

func (s *RedisStore) redisKey(sessionID string) string {
	return s.keyPrefix + sessionID
}

func (s *RedisStore) load(ctx context.Context, sessionID string) (map[string]any, error) {
	data, err := s.client.Get(ctx, s.redisKey(sessionID)).Bytes()
	if err != nil {
		return nil, err
	}

	switch s.compat {
	case CompatV1:
		return decodeLegacyValues(data)
	case CompatMixed:
		return decodeAnyValues(data)
	default:
		return decodeJSONValues(data)
	}
}

func (s *RedisStore) save(ctx context.Context, sessionID string, values map[string]any, maxAge int) error {
	var (
		data []byte
		err  error
	)

	if s.compat == CompatV1 {
		data, err = encodeLegacyValues(values)
	} else {
		data, err = json.Marshal(values)
	}

	if err != nil {
		return err
	}

	var expiration time.Duration
	if maxAge > 0 {
		expiration = time.Duration(maxAge) * time.Second
	}

	return s.client.Set(ctx, s.redisKey(sessionID), data, expiration).Err()
}

// TLSConfig contains options for TLS authentication.
type TLSConfig = storeredis.TLSConfig

func generateRandomKey() (string, error) {
	k := make([]byte, 64)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return "", err
	}

	// add timestamp to key
	timestamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	return timestamp + "_" + strings.TrimRight(base32.StdEncoding.EncodeToString(k), "="), nil
}
