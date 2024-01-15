package store

import (
	"context"
	"crypto/tls"

	"github.com/gorilla/sessions"
	"github.com/rbcervilla/redisstore/v9"
	"github.com/redis/go-redis/v9"
	"github.com/twmb/tlscfg"
)

type Redis struct {
	Address  string    `cfg:"address"`
	Username string    `cfg:"username"`
	Password string    `cfg:"password"`
	TLS      TLSConfig `cfg:"tls"`

	KeyPrefix string `cfg:"key_prefix"`
}

func (r Redis) Store(ctx context.Context, sessionKey string, opts sessions.Options) (*redisstore.RedisStore, error) {
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

	rStore, err := redisstore.NewRedisStore(ctx, client)
	if err != nil {
		return nil, err
	}

	if r.KeyPrefix == "" {
		r.KeyPrefix = "session_"
	}

	rStore.KeyPrefix(r.KeyPrefix)

	rStore.Options(opts)

	rStore.KeyGen(func() (string, error) {
		return sessionKey, nil
	})

	return rStore, nil
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
