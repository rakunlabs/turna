package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/rakunlabs/turna/pkg/telemetry"
)

// Telemetry counts datagrams and bytes with the global OpenTelemetry meter
// provider. Place it first in the chain.
type Telemetry struct {
	// Name is the "name" attribute of the metrics, default is the middleware name.
	Name string `cfg:"name"`
}

func (m *Telemetry) Middleware(_ context.Context, name string) (func(conn net.PacketConn, addr net.Addr, data []byte) error, error) {
	meter := telemetry.Meter()

	packets, err1 := meter.Int64Counter("turna.udp.packets",
		metric.WithDescription("Number of received UDP datagrams."), metric.WithUnit("{packet}"))
	received, err2 := meter.Int64Counter("turna.udp.received",
		metric.WithDescription("Bytes received from UDP clients."), metric.WithUnit("By"))

	if err := errors.Join(err1, err2); err != nil {
		return nil, fmt.Errorf("create udp meters: %w", err)
	}

	if m.Name != "" {
		name = m.Name
	}

	attrs := metric.WithAttributes(attribute.String("name", name))

	return func(_ net.PacketConn, _ net.Addr, data []byte) error {
		ctx := context.Background()

		packets.Add(ctx, 1, attrs)
		received.Add(ctx, int64(len(data)), attrs)

		return nil
	}, nil
}
