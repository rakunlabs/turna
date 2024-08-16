package rebac

import (
	"context"
	"net/http"

	"github.com/go-ldap/ldap/v3"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data/badger"
	"github.com/worldline-go/initializer"
)

type Rebac struct {
	PrefixPath string   `cfg:"prefix_path"`
	Ldap       Ldap     `cfg:"ldap"`
	Database   Database `cfg:"database"`

	db        data.Database    `cfg:"-"`
	ldapConn  *ldap.Conn       `cfg:"-"`
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

	if m.Ldap.Addr != "" {
		ldapConn, err := m.Ldap.ConnectLdap()
		if err != nil {
			return nil, err
		}

		m.ldapConn = ldapConn
	}

	mux := m.MuxSet(m.PrefixPath)

	// new database
	db, err := badger.New(m.Database.Badger.Path)
	if err != nil {
		return nil, err
	}

	initializer.Shutdown.Add(db.Close)

	m.db = db

	return func(next http.Handler) http.Handler {
		mux.NotFound(next.ServeHTTP)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mux.ServeHTTP(w, r)
		})
	}, nil
}
