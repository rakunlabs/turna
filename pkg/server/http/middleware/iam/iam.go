package iam

import (
	"context"
	"net"
	"net/http"
	"strings"
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
	sync  *Sync      `cfg:"-"`

	ctxService context.Context `cfg:"-"`
}

type Database struct {
	Badger Badger `cfg:"badger"`
}

type Badger struct {
	Path string `cfg:"path"`
	// WriteAPI to sync data from write enabled service
	// this makes read-only service
	WriteAPI string `cfg:"write_api"`
	// Memory to hold data in memory
	Memory bool `cfg:"memory"`
	// Flatten to flatten the data when start, default is true
	Flatten *bool `cfg:"flatten"`

	// SyncSchema is the schema of the sync service, default is http
	SyncSchema string `cfg:"sync_schema"`
	// SyncHost is the host of the sync service, default is the caller host
	SyncHost string `cfg:"sync_host"`
	// SyncHostFromInterface is for network interface to get the host, default is false
	SyncHostFromInterface bool `cfg:"sync_host_from_interface"`
	// SyncHostFromInterfaceIPPrefix is the prefix of the interface IP
	SyncHostFromInterfaceIPPrefix string `cfg:"sync_host_from_interface_ip_prefix"`
	// SyncPort is the port of the sync service, default is 8080
	SyncPort string `cfg:"sync_port"`
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

	m.PrefixPath = "/" + strings.Trim(m.PrefixPath, "/")

	mux := m.MuxSet(m.PrefixPath)

	// new database
	flatten := true
	if m.Database.Badger.Flatten != nil {
		flatten = *m.Database.Badger.Flatten
	}

	db, err := badger.New(m.Database.Badger.Path, m.Database.Badger.Memory, flatten)
	if err != nil {
		return nil, err
	}

	into.ShutdownAdd(db.Close, "iam db")

	m.db = db

	if m.Database.Badger.SyncHostFromInterface {
		addr, err := net.InterfaceAddrs()
		if err != nil {
			return nil, err
		}

		for _, a := range addr {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					ipAddr := ipnet.IP.String()
					if strings.HasPrefix(ipAddr, m.Database.Badger.SyncHostFromInterfaceIPPrefix) {
						m.Database.Badger.SyncHost = ipnet.IP.String()
						break
					}
				}
			}
		}
	}

	m.sync, err = NewSync(SyncConfig{
		WriteAPI:   m.Database.Badger.WriteAPI,
		PrefixPath: m.PrefixPath,
		DB:         db,

		SyncSchema: m.Database.Badger.SyncSchema,
		SyncHost:   m.Database.Badger.SyncHost,
		SyncPort:   m.Database.Badger.SyncPort,
	})
	if err != nil {
		return nil, err
	}

	m.sync.SyncTTL(ctx)
	// first sync
	if err := m.sync.Sync(ctx); err != nil {
		return nil, err
	}

	m.sync.SyncStart(ctx)

	if m.Ldap.Addr != "" && m.Database.Badger.WriteAPI == "" {
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

	m.ctxService = ctx

	return func(next http.Handler) http.Handler {
		mux.NotFound(next.ServeHTTP)

		return mux
	}, nil
}
