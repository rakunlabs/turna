package service

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// TestH2CGRPC proxies a plaintext gRPC call through an h2c server to an h2c
// upstream.
func TestH2CGRPC(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	gs := grpc.NewServer()
	hs := health.NewServer()
	hs.SetServingStatus("turna", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gs, hs)

	go func() { _ = gs.Serve(upstream) }()
	defer gs.Stop()

	h := newServiceHandler(t, context.Background(), &Service{
		H2C:          true,
		LoadBalancer: LoadBalancer{Servers: []Server{{URL: "http://" + upstream.Addr().String()}}},
	})

	front := httptest.NewUnstartedServer(h)

	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	front.Config.Protocols = &protocols
	front.Start()
	defer front.Close()

	conn, err := grpc.NewClient(front.Listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: "turna"})
	if err != nil {
		t.Fatal(err)
	}

	if res.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v", res.GetStatus())
	}
}

// TestStreamingFlushedImmediately checks that chunked responses reach the
// client before the upstream finishes.
func TestStreamingFlushedImmediately(t *testing.T) {
	release := make(chan struct{})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("first\n"))
		w.(http.Flusher).Flush()
		<-release
		_, _ = w.Write([]byte("second\n"))
	}))
	defer upstream.Close()
	defer close(release)

	h := newServiceHandler(t, context.Background(), &Service{
		LoadBalancer: LoadBalancer{Servers: []Server{{URL: upstream.URL}}},
	})

	front := httptest.NewServer(h)
	defer front.Close()

	res, err := http.Get(front.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	buf := make([]byte, 6)

	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(res.Body, buf)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil || string(buf) != "first\n" {
			t.Fatalf("got %q %v", buf, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first chunk was not flushed")
	}
}
