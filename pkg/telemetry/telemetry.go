// Package telemetry sets up the global OpenTelemetry providers with
// github.com/rakunlabs/tell and holds the shared meters of the TCP and UDP
// servers.
package telemetry

import (
	"context"
	"fmt"

	"github.com/rakunlabs/tell"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const ScopeName = "github.com/rakunlabs/turna"

// Config is the OpenTelemetry configuration. Without a collector (config or
// OTEL_EXPORTER_OTLP_ENDPOINT) the providers are noop.
type Config = tell.Config

// Start initializes the global providers, the returned function flushes and
// closes them.
func Start(ctx context.Context, cfg Config) (func() error, error) {
	collector, err := tell.New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("telemetry: %w", err)
	}

	return collector.Shutdown, nil
}

// Meter returns the meter of turna from the global provider.
func Meter() metric.Meter {
	return otel.GetMeterProvider().Meter(ScopeName)
}
