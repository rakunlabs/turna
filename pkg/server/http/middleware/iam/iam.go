package iam

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/rakunlabs/into"
	"github.com/rakunlabs/logi"
	"github.com/worldline-go/conn/connredis"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data/badger"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/ldap"
)

type Iam struct {
	PrefixPath string    `cfg:"prefix_path"`
	Ldap       ldap.Ldap `cfg:"ldap"`
	Database   Database  `cfg:"database"`

	Check data.CheckConfig `cfg:"check"`

	db        *badger.Badger   `cfg:"-"`
	swaggerFS http.HandlerFunc `cfg:"-"`
	uiFS      http.HandlerFunc `cfg:"-"`

	syncM sync.Mutex `cfg:"-"`
	sync  *Sync      `cfg:"-"`

	ctxService context.Context `cfg:"-"`
}

type Database struct {
	Path string `cfg:"path"`
	// WriteAPI to sync data from write enabled service
	// this makes read-only service
	WriteAPI string `cfg:"write_api"`
	// BackupPath database from backup when start
	BackupPath string `cfg:"backup_path"`
	// Memory to hold data in memory
	Memory bool `cfg:"memory"`
	// Flatten to flatten the data when start, default is true
	Flatten *bool `cfg:"flatten"`

	// Redis configuration to sync between IAM services
	Redis connredis.Config `cfg:"redis"`
	// PubSubTopic to sync between IAM services
	PubSubTopic string `cfg:"pubsub_topic"`
}

func (m *Iam) DB() *badger.Badger {
	return m.db
}

func (m *Iam) Middleware(ctx context.Context, name string) (func(http.Handler) http.Handler, error) {
	swaggerMiddleware, err := m.SwaggerMiddleware()
	if err != nil {
		return nil, err
	}
	m.swaggerFS = swaggerMiddleware(nil).ServeHTTP

	uiMiddleware, err := m.UIMiddleware()
	if err != nil {
		return nil, err
	}
	m.uiFS = uiMiddleware(nil).ServeHTTP

	m.PrefixPath = "/" + strings.Trim(m.PrefixPath, "/")

	mux := m.MuxSet(m.PrefixPath)

	// redis connection
	rConn, err := connredis.New(m.Database.Redis)
	if err != nil {
		return nil, err
	}

	// new database
	flatten := true
	if m.Database.Flatten != nil {
		flatten = *m.Database.Flatten
	}

	if m.Database.Path == "" && !m.Database.Memory {
		return nil, errors.New("database path or memory is required")
	}

	db, err := badger.New(m.Database.Path, m.Database.BackupPath, m.Database.Memory, flatten, m.Check)
	if err != nil {
		return nil, err
	}

	into.ShutdownAdd(db.Close, "iam db")

	m.db = db

	m.sync, err = NewSync(SyncConfig{
		WriteAPI:    m.Database.WriteAPI,
		PrefixPath:  m.PrefixPath,
		DB:          db,
		Redis:       rConn,
		PubSubTopic: m.Database.PubSubTopic,
	})
	if err != nil {
		return nil, err
	}

	ctx = logi.WithContext(ctx, slog.With(slog.String("middleware", "iam")))
	m.ctxService = ctx

	// first sync
	if err := m.sync.Sync(ctx, 0); err != nil {
		return nil, err
	}

	syncClose, err := m.sync.SyncStart(ctx)
	if err != nil {
		return nil, err
	}

	into.ShutdownAdd(syncClose, "iam sync")

	if m.Ldap.Addr != "" && m.Database.WriteAPI == "" {
		if !m.Ldap.DisableFirstConnect {
			if _, err := m.Ldap.ConnectWithCheck(); err != nil {
				return nil, err
			}
		}

		// start sync
		if m.Ldap.SyncDuration == 0 {
			m.Ldap.SyncDuration = ldap.DefaultLdapSyncDuration
		}

		go m.Ldap.StartSync(ctx, m.LdapSync)
	}

	GlobalRegistry.Set(name, m)

	return func(next http.Handler) http.Handler {
		mux.NotFound(next.ServeHTTP)

		return mux
	}, nil
}
