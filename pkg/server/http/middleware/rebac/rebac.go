package rebac

import (
	"context"
	"net/http"

	"github.com/rakunlabs/into"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data/badger"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/ldap"
)

type Rebac struct {
	PrefixPath string    `cfg:"prefix_path"`
	Ldap       ldap.Ldap `cfg:"ldap"`
	Database   Database  `cfg:"database"`

	db        data.Database    `cfg:"-"`
	swaggerFS http.HandlerFunc `cfg:"-"`
	uiFS      http.HandlerFunc `cfg:"-"`
}

type Database struct {
	Badger Badger `cfg:"badger"`
}

type Badger struct {
	Path string `cfg:"path"`
}

func (m *Rebac) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
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
		if _, err := m.Ldap.ConnectWithCheck(); err != nil {
			return nil, err
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
