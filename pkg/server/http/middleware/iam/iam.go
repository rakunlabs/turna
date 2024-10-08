package iam

import (
	"context"
	"net/http"
	"sync"

	"github.com/rakunlabs/into"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data/badger"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/ldap"
)

type Iam struct {
	PrefixPath string    `cfg:"prefix_path"`
	Ldap       ldap.Ldap `cfg:"ldap"`
	Database   Database  `cfg:"database"`

	db        data.Database    `cfg:"-"`
	swaggerFS http.HandlerFunc `cfg:"-"`
	uiFS      http.HandlerFunc `cfg:"-"`

	syncM sync.Mutex `cfg:"-"`
}

type Database struct {
	Badger Badger `cfg:"badger"`
}

type Badger struct {
	Path string `cfg:"path"`
	// redirect to write request to write api
	WriteAPI string `cfg:"write_api"`
}

func (m *Iam) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
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

	mux := m.MuxSet(m.PrefixPath)

	// new database
	db, err := badger.New(m.Database.Badger.Path)
	if err != nil {
		return nil, err
	}

	into.ShutdownAdd(db.Close, "rebac db")

	m.db = db

	if m.Ldap.Addr != "" {
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

	return func(next http.Handler) http.Handler {
		mux.NotFound(next.ServeHTTP)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mux.ServeHTTP(w, r)
		})
	}, nil
}
