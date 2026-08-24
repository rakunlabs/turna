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

// DefaultConnectTimeout bounds dialing and reflection for a single request so
// an unreachable target cannot pin a handler goroutine forever.
var DefaultConnectTimeout = 10 * time.Second

type GrpcUI struct {
	// Addr is the address of the gRPC server like 'dns:///localhost:8080'.
	Addr  string        `cfg:"addr"`
	Timer time.Duration `cfg:"timer"`
	// ConnectTimeout caps how long the first request may wait while the gRPC
	// target is dialed and queried through reflection.
	ConnectTimeout time.Duration `cfg:"connect_timeout"`

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

func (m *GrpcUI) Start(ctx context.Context) (http.Handler, error) {
	if h := m.Get(); h != nil {
		return h, nil
	}

	timeout := m.ConnectTimeout
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}

	// The dial and the reflection queries happen outside of the connection
	// lock: holding it here would block every other request on this target,
	// and they are bound to ctx so a dead target fails instead of hanging.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// NewClient connects lazily, the reflection calls below establish the
	// connection and honour ctx. The handler does not retain ctx.
	cc, err := grpc.NewClient(m.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	h, err := standalone.HandlerViaReflection(ctx, cc, m.Addr)
	if err != nil {
		cc.Close()

		return nil, err
	}

	slog.Debug("grpcui connection started", "addr", m.Addr, "basepath", m.BasePath)

	m.connection.m.Lock()
	defer m.connection.m.Unlock()

	// Another request may have connected first, keep that one and drop ours.
	if m.connection.handler != nil {
		cc.Close()

		return m.connection.handler, nil
	}

	if m.connection.debouncer == nil {
		m.connection.debouncer = NewDebouncer(m.Timer)
	}

	m.connection.cc = cc
	m.connection.handler = h

	m.connection.debouncer(func() {
		m.connection.m.Lock()
		defer m.connection.m.Unlock()

		if m.connection.cc != nil {
			m.connection.cc.Close()
			m.connection.cc = nil
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
				h, err = m.Start(r.Context())
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)

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
