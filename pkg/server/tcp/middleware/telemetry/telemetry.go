package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
	"github.com/rakunlabs/turna/pkg/telemetry"
)

// Telemetry records OpenTelemetry metrics of connections with the global
// meter provider.
type Telemetry struct {
	// Name is the "name" attribute of the metrics, default is the middleware name.
	Name string `cfg:"name"`
}

type meters struct {
	connections metric.Int64Counter
	active      metric.Int64UpDownCounter
	duration    metric.Float64Histogram
	received    metric.Int64Counter
	sent        metric.Int64Counter
}

func newMeters() (*meters, error) {
	meter := telemetry.Meter()

	var (
		m   meters
		err error
		e   error
	)

	m.connections, e = meter.Int64Counter("turna.tcp.connections",
		metric.WithDescription("Number of handled TCP connections."), metric.WithUnit("{connection}"))
	err = errors.Join(err, e)

	m.active, e = meter.Int64UpDownCounter("turna.tcp.active_connections",
		metric.WithDescription("Number of open TCP connections."), metric.WithUnit("{connection}"))
	err = errors.Join(err, e)

	m.duration, e = meter.Float64Histogram("turna.tcp.connection.duration",
		metric.WithDescription("Duration of TCP connections."), metric.WithUnit("s"))
	err = errors.Join(err, e)

	m.received, e = meter.Int64Counter("turna.tcp.received",
		metric.WithDescription("Bytes received from clients."), metric.WithUnit("By"))
	err = errors.Join(err, e)

	m.sent, e = meter.Int64Counter("turna.tcp.sent",
		metric.WithDescription("Bytes sent to clients."), metric.WithUnit("By"))
	err = errors.Join(err, e)

	if err != nil {
		return nil, fmt.Errorf("create tcp meters: %w", err)
	}

	return &m, nil
}

func (m *Telemetry) Middleware(_ context.Context, name string) (tcpmw.Middleware, error) {
	meters, err := newMeters()
	if err != nil {
		return nil, err
	}

	if m.Name != "" {
		name = m.Name
	}

	base := attribute.String("name", name)
	baseSet := metric.WithAttributes(base)

	return func(next tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			ctx := context.Background()
			start := time.Now()

			meters.active.Add(ctx, 1, baseSet)
			defer meters.active.Add(ctx, -1, baseSet)

			c := tcpmw.NewCountingConn(conn)

			err := next(c)

			result := "ok"
			if err != nil {
				result = "error"
				if errors.Is(err, tcpmw.ErrReject) {
					result = "rejected"
				}
			}

			attrs := metric.WithAttributes(base, attribute.String("result", result))

			meters.connections.Add(ctx, 1, attrs)
			meters.duration.Record(ctx, time.Since(start).Seconds(), attrs)
			meters.received.Add(ctx, c.BytesRead(), baseSet)
			meters.sent.Add(ctx, c.BytesWritten(), baseSet)

			return err
		}
	}, nil
}
