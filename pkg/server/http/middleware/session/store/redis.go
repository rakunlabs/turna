package store

import (
	"context"
	"crypto/rand"
	"crypto/tls"
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
	"github.com/redis/go-redis/v9"
	"github.com/twmb/tlscfg"
)

type Redis struct {
	Address  string    `cfg:"address"`
	Username string    `cfg:"username"`
	Password string    `cfg:"password"`
	TLS      TLSConfig `cfg:"tls"`

	KeyPrefix string `cfg:"key_prefix"`
	// SessionKey signs the session ID cookie. If empty, a random key is generated.
	SessionKey string `cfg:"session_key"`
}

type RedisStore struct {
	client    *redis.Client
	keyPrefix string
	codec     *securecookie.Codec
	options   sessions.Options
}

func (r Redis) Store(ctx context.Context, opts sessions.Options) (*RedisStore, error) {
	tlsConfig, err := r.TLS.Generate()
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(&redis.Options{
		Addr:      r.Address,
		Username:  r.Username,
		Password:  r.Password,
		TLSConfig: tlsConfig,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()

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
	}, nil
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

	var sessionID string
	if err := s.codec.Decode(name, cookie.Value, &sessionID); err != nil {
		return session, err
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

	cookieValue, err := s.codec.Encode(session.Name(), session.ID)
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

	values := make(map[string]any)
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}

	return values, nil
}

func (s *RedisStore) save(ctx context.Context, sessionID string, values map[string]any, maxAge int) error {
	data, err := json.Marshal(values)
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
type TLSConfig struct {
	// Enabled is whether TLS is enabled.
	Enabled bool `cfg:"enabled"`
	// CertFile is the path to the client's TLS certificate.
	// Should be use with KeyFile.
	CertFile string `cfg:"cert_file"`
	// KeyFile is the path to the client's TLS key.
	// Should be use with CertFile.
	KeyFile string `cfg:"key_file"`
	// CAFile is the path to the CA certificate.
	// If empty, the server's root CA set will be used.
	CAFile string `cfg:"ca_file"`
}

// Generate returns a tls.Config based on the TLSConfig.
//
// If the TLSConfig is empty, nil is returned.
func (t TLSConfig) Generate() (*tls.Config, error) {
	if !t.Enabled {
		return nil, nil
	}

	opts := []tlscfg.Opt{}

	// load client cert
	if t.CertFile != "" && t.KeyFile != "" {
		opts = append(opts, tlscfg.WithDiskKeyPair(t.CertFile, t.KeyFile))
	}

	// load CA cert
	opts = append(opts, tlscfg.WithSystemCertPool())
	if t.CAFile != "" {
		opts = append(opts, tlscfg.WithDiskCA(t.CAFile, tlscfg.ForClient))
	}

	return tlscfg.New(opts...)
}

func generateRandomKey() (string, error) {
	k := make([]byte, 64)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return "", err
	}

	// add timestamp to key
	timestamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	return timestamp + "_" + strings.TrimRight(base32.StdEncoding.EncodeToString(k), "="), nil
}
