package server

import (
	"fmt"
	"maps"
	"slices"

	"github.com/rakunlabs/turna/pkg/server/http"
	"github.com/rakunlabs/turna/pkg/server/tcp"
	"github.com/rakunlabs/turna/pkg/server/udp"
)

// LayerStatic is the layer of the configuration file.
const LayerStatic = "static"

// mergeLayers combines layers into one configuration. TLS settings other than
// the certificate store come from the static layer.
func mergeLayers(layers map[string]*Server) (*Server, error) {
	out := &Server{
		EntryPoints: make(map[string]EntryPoint),
		HTTP: http.HTTP{
			Routers:     make(map[string]http.Router),
			Middlewares: make(map[string]http.HTTPMiddleware),
		},
		TCP: tcp.TCP{
			Routers:     make(map[string]tcp.Router),
			Middlewares: make(map[string]tcp.TCPMiddleware),
		},
		UDP: udp.UDP{
			Routers:     make(map[string]udp.Router),
			Middlewares: make(map[string]udp.UDPMiddleware),
		},
	}

	if static, ok := layers[LayerStatic]; ok {
		out.LoadValue = static.LoadValue
		out.HTTP.TLS = static.HTTP.TLS
		out.HTTP.TLS.Store = nil
	}

	out.HTTP.TLS.Store = make(map[string][]http.Certificate)

	// stable order: static first, then by name
	names := slices.Sorted(maps.Keys(layers))
	if i := slices.Index(names, LayerStatic); i > 0 {
		names = append([]string{LayerStatic}, slices.Delete(names, i, i+1)...)
	}

	for _, name := range names {
		l := layers[name]

		if err := mergeMap(out.EntryPoints, l.EntryPoints, name, "entrypoint"); err != nil {
			return nil, err
		}
		if err := mergeMap(out.HTTP.Routers, l.HTTP.Routers, name, "http router"); err != nil {
			return nil, err
		}
		if err := mergeMap(out.HTTP.Middlewares, l.HTTP.Middlewares, name, "http middleware"); err != nil {
			return nil, err
		}
		if err := mergeMap(out.TCP.Routers, l.TCP.Routers, name, "tcp router"); err != nil {
			return nil, err
		}
		if err := mergeMap(out.TCP.Middlewares, l.TCP.Middlewares, name, "tcp middleware"); err != nil {
			return nil, err
		}
		if err := mergeMap(out.UDP.Routers, l.UDP.Routers, name, "udp router"); err != nil {
			return nil, err
		}
		if err := mergeMap(out.UDP.Middlewares, l.UDP.Middlewares, name, "udp middleware"); err != nil {
			return nil, err
		}

		// certificates of the same host from several layers are all served,
		// the first one wins for the host
		for host, certs := range l.HTTP.TLS.Store {
			out.HTTP.TLS.Store[host] = append(out.HTTP.TLS.Store[host], certs...)
		}
	}

	return out, nil
}

func mergeMap[V any](dst, src map[string]V, layer, kind string) error {
	for k, v := range src {
		if _, ok := dst[k]; ok {
			return fmt.Errorf("layer %s: %s %q is already defined", layer, kind, k)
		}

		dst[k] = v
	}

	return nil
}
