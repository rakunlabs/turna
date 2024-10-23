package iam

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/rakunlabs/into"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data/badger"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/ldap"
)

type Iam struct {
	PrefixPath string    `cfg:"prefix_path"`
	Ldap       ldap.Ldap `cfg:"ldap"`
	Database   Database  `cfg:"database"`

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
	// Memory to hold data in memory
	Memory bool `cfg:"memory"`
	// Flatten to flatten the data when start, default is true
	Flatten *bool `cfg:"flatten"`

	// TriggerBackground for sync process in background
	TriggerBackground bool `cfg:"trigger_background"`

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
	if m.Database.Flatten != nil {
		flatten = *m.Database.Flatten
	}

	if m.Database.Path == "" && !m.Database.Memory {
		return nil, fmt.Errorf("database path or memory is required")
	}

	db, err := badger.New(m.Database.Path, m.Database.Memory, flatten)
	if err != nil {
		return nil, err
	}

	into.ShutdownAdd(db.Close, "iam db")

	m.db = db

	if m.Database.SyncHostFromInterface {
		addr, err := net.InterfaceAddrs()
		if err != nil {
			return nil, err
		}

		for _, a := range addr {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					ipAddr := ipnet.IP.String()
					if strings.HasPrefix(ipAddr, m.Database.SyncHostFromInterfaceIPPrefix) {
						m.Database.SyncHost = ipnet.IP.String()
						break
					}
				}
			}
		}
	}

	m.sync, err = NewSync(SyncConfig{
		WriteAPI:          m.Database.WriteAPI,
		PrefixPath:        m.PrefixPath,
		DB:                db,
		TriggerBackground: m.Database.TriggerBackground,

		SyncSchema: m.Database.SyncSchema,
		SyncHost:   m.Database.SyncHost,
		SyncPort:   m.Database.SyncPort,
	})
	if err != nil {
		return nil, err
	}

	m.sync.SyncTTL(ctx)
	// first sync
	if err := m.sync.Sync(ctx, 0); err != nil {
		return nil, err
	}

	m.sync.SyncStart(ctx)

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

	m.ctxService = ctx

	return func(next http.Handler) http.Handler {
		mux.NotFound(next.ServeHTTP)

		return mux
	}, nil
}
