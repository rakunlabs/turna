package grpcui

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/fullstorydev/grpcui/standalone"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/stripprefix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GrpcUI struct {
	// Addr is the address of the gRPC server like 'dns:///localhost:8080'.
	Addr  string        `cfg:"addr"`
	Timer time.Duration `cfg:"timer"`

	BasePath string `cfg:"basepath"`

	connection connection
}

type connection struct {
	cc        *grpc.ClientConn
	handler   http.Handler
	debouncer func(func())

	m sync.RWMutex
}

func (m *GrpcUI) Get() http.Handler {
	m.connection.m.RLock()
	defer m.connection.m.RUnlock()

	return m.connection.handler
}

func (m *GrpcUI) Start() (http.Handler, error) {
	m.connection.m.Lock()
	defer m.connection.m.Unlock()

	if m.connection.debouncer == nil {
		m.connection.debouncer = NewDebouncer(m.Timer)
	}

	if m.connection.handler != nil {
		return m.connection.handler, nil
	}

	cc, err := grpc.DialContext(context.Background(),
		m.Addr,
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	slog.Debug("grpcui connection started", "addr", m.Addr, "basepath", m.BasePath)

	h, err := standalone.HandlerViaReflection(context.Background(),
		cc,
		m.Addr,
	)
	if err != nil {
		return nil, err
	}

	m.connection.cc = cc
	m.connection.handler = h

	m.connection.debouncer(func() {
		m.connection.m.Lock()
		defer m.connection.m.Unlock()

		if m.connection.cc != nil {
			m.connection.cc.Close()
		}

		m.connection.handler = nil

		slog.Debug("grpcui connection closed", "addr", m.Addr, "basepath", m.BasePath)
	})

	return h, nil
}

func (m *GrpcUI) Middleware() func(http.Handler) http.Handler {
	sprefix := stripprefix.StripPrefix{Prefix: m.BasePath}

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := m.Get()
			if h == nil {
				var err error
				h, err = m.Start()
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)

					return
				}
			}

			var err error
			r.URL.Path, err = sprefix.Strip(r.URL.Path)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

			h.ServeHTTP(w, r)
		})
	}
}
