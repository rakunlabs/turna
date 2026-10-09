package kubernetes

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/rakunlabs/turna/pkg/server"
	"github.com/rakunlabs/turna/pkg/server/cert"
	"github.com/rakunlabs/turna/pkg/server/registry"
)

// backend describes a local server exposed as a Service.
type backend struct {
	name     string
	addr     string
	protocol corev1.Protocol
	port     int32 // service port
}

func backendObjects(bs ...backend) ([]*corev1.Service, []*discoveryv1.EndpointSlice) {
	var (
		svcs   []*corev1.Service
		slices []*discoveryv1.EndpointSlice
	)

	for _, b := range bs {
		host, p, _ := net.SplitHostPort(b.addr)
		port, _ := strconv.Atoi(p)

		protocol := b.protocol
		if protocol == "" {
			protocol = corev1.ProtocolTCP
		}

		svcs = append(svcs, &corev1.Service{
			ObjectMeta: objMeta("app", b.name),
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "p", Port: b.port, Protocol: protocol}}},
		})

		slices = append(slices, &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "app", Name: b.name + "-x",
				Labels: map[string]string{discoveryv1.LabelServiceName: b.name},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports:       []discoveryv1.EndpointPort{{Name: ptr("p"), Port: ptr(int32(port)), Protocol: &protocol}},
			Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{host}, Conditions: discoveryv1.EndpointConditions{Ready: ptr(true)}}},
		})
	}

	return svcs, slices
}

func backendRef(name string, port int32) gwv1.BackendRef {
	return gwv1.BackendRef{BackendObjectReference: gwv1.BackendObjectReference{Name: gwv1.ObjectName(name), Port: ptr(gwv1.PortNumber(port))}}
}

// applyTranslate translates a snapshot and applies it to a fresh manager.
func applyTranslate(t *testing.T, cfg *Config, snap *Snapshot) *Result {
	t.Helper()

	prev := registry.DrainTimeout
	registry.DrainTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	m := server.NewManager(ctx, wg)

	t.Cleanup(func() {
		m.Shutdown()
		cancel()
		wg.Wait()
		registry.DrainTimeout = prev
	})

	res := Translate(cfg, snap)
	if err := m.SetLayer(Layer, res.Server); err != nil {
		t.Fatalf("apply: %v", err)
	}

	return res
}

// echoTCP answers each connection with prefix + first line.
func echoTCP(t *testing.T, prefix string) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			go func() {
				defer func() { _ = c.Close() }()

				line, _ := bufio.NewReader(c).ReadString('\n')
				_, _ = io.WriteString(c, prefix+line)
			}()
		}
	}()

	return l.Addr().String()
}

// tlsEcho is a TLS server presenting a certificate for host.
func tlsEcho(t *testing.T, host, prefix string) string {
	t.Helper()

	generated, err := cert.GenerateCertificate(cert.WithDNSNames(host))
	if err != nil {
		t.Fatal(err)
	}

	pair, err := tls.X509KeyPair(generated.Certificate, generated.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			go func() {
				defer func() { _ = c.Close() }()

				line, _ := bufio.NewReader(c).ReadString('\n')
				_, _ = io.WriteString(c, prefix+line)
			}()
		}
	}()

	return l.Addr().String()
}

func roundTrip(t *testing.T, c net.Conn) string {
	t.Helper()

	_ = c.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := io.WriteString(c, "hi\n"); err != nil {
		return "write: " + err.Error()
	}

	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return "read: " + err.Error()
	}

	return strings.TrimSpace(line)
}

func tlsDial(t *testing.T, addr, sni string) (*tls.Conn, error) {
	t.Helper()

	d := &net.Dialer{Timeout: 2 * time.Second}

	return tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: sni, InsecureSkipVerify: true}) //nolint:gosec // test
}

func peerName(c *tls.Conn) string {
	certs := c.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return ""
	}

	return lastDNS(certs[0])
}

// lastDNS skips the "localhost" name added to generated certificates.
func lastDNS(c *x509.Certificate) string {
	if len(c.DNSNames) > 0 {
		return c.DNSNames[len(c.DNSNames)-1]
	}

	return ""
}

func TestStreamTLSMixed(t *testing.T) {
	passAddr := tlsEcho(t, "pass.example.com", "pass:")
	plainAddr := echoTCP(t, "plain:")

	generated, err := cert.GenerateCertificate(cert.WithDNSNames("term.example.com"))
	if err != nil {
		t.Fatal(err)
	}

	secret := &corev1.Secret{
		ObjectMeta: objMeta("app", "term-tls"),
		Data:       map[string][]byte{corev1.TLSCertKey: generated.Certificate, corev1.TLSPrivateKeyKey: generated.PrivateKey},
	}

	svcs, slices := backendObjects(
		backend{name: "pass", addr: passAddr, port: 443},
		backend{name: "plain", addr: plainAddr, port: 3000},
	)

	gw := gatewayObj("app", "gw",
		gwv1.Listener{Name: "pass", Port: 443, Protocol: gwv1.TLSProtocolType, Hostname: ptr(gwv1.Hostname("pass.example.com")),
			TLS: &gwv1.ListenerTLSConfig{Mode: ptr(gwv1.TLSModePassthrough)}},
		gwv1.Listener{Name: "term", Port: 443, Protocol: gwv1.TLSProtocolType, Hostname: ptr(gwv1.Hostname("term.example.com")),
			TLS: &gwv1.ListenerTLSConfig{Mode: ptr(gwv1.TLSModeTerminate), CertificateRefs: []gwv1.SecretObjectReference{{Name: "term-tls"}}}},
	)

	tlsRoute := func(name, host, svc string, port int32) *gwv1.TLSRoute {
		return &gwv1.TLSRoute{
			ObjectMeta: objMeta("app", name),
			Spec: gwv1.TLSRouteSpec{
				CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: []gwv1.ParentReference{{Name: "gw"}}},
				Hostnames:       []gwv1.Hostname{gwv1.Hostname(host)},
				Rules:           []gwv1.TLSRouteRule{{BackendRefs: []gwv1.BackendRef{backendRef(svc, port)}}},
			},
		}
	}

	local := freePort(t)

	res := applyTranslate(t, &Config{Address: "127.0.0.1", PortOffset: local - 443}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gw},
		TLSRoutes:      []*gwv1.TLSRoute{tlsRoute("pass", "pass.example.com", "pass", 443), tlsRoute("term", "term.example.com", "plain", 3000)},
		Services:       svcs,
		EndpointSlices: slices,
		Secrets:        []*corev1.Secret{secret},
	})

	st := res.Gateways[nsName{"app", "gw"}]
	for _, l := range st.Listeners {
		if c := condition(t, l.Conditions, string(gwv1.ListenerConditionProgrammed)); c.Status != metav1.ConditionTrue || l.AttachedRoutes != 1 {
			t.Fatalf("listener %s: %v attached=%d", l.Name, c, l.AttachedRoutes)
		}
	}

	addr := "127.0.0.1:" + strconv.Itoa(local)

	// passthrough: the backend certificate is presented
	c, err := tlsDial(t, addr, "pass.example.com")
	if err != nil {
		t.Fatal(err)
	}

	if peerName(c) != "pass.example.com" || roundTrip(t, c) != "pass:hi" {
		t.Fatalf("passthrough peer=%q", peerName(c))
	}
	_ = c.Close()

	// terminate: the listener certificate is presented, backend gets plain text
	c, err = tlsDial(t, addr, "term.example.com")
	if err != nil {
		t.Fatal(err)
	}

	if peerName(c) != "term.example.com" || roundTrip(t, c) != "plain:hi" {
		t.Fatalf("terminate peer=%q", peerName(c))
	}
	_ = c.Close()

	// unknown SNI is rejected
	if c, err := tlsDial(t, addr, "other.example.com"); err == nil {
		_ = c.Close()
		t.Fatal("unknown sni accepted")
	}
}

func TestStreamTCPAndUDP(t *testing.T) {
	tcpAddr := echoTCP(t, "tcp:")

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udpConn.Close() })

	go func() {
		buf := make([]byte, 1024)
		for {
			n, a, err := udpConn.ReadFrom(buf)
			if err != nil {
				return
			}

			_, _ = udpConn.WriteTo(append([]byte("udp:"), buf[:n]...), a)
		}
	}()

	svcs, slices := backendObjects(
		backend{name: "tcp", addr: tcpAddr, port: 3000},
		backend{name: "udp", addr: udpConn.LocalAddr().String(), port: 53, protocol: corev1.ProtocolUDP},
	)

	// TCP and UDP listeners on the same port number
	gw := gatewayObj("app", "gw",
		gwv1.Listener{Name: "tcp", Port: 5300, Protocol: gwv1.TCPProtocolType},
		gwv1.Listener{Name: "udp", Port: 5300, Protocol: gwv1.UDPProtocolType},
		gwv1.Listener{Name: "idle", Port: 5301, Protocol: gwv1.TCPProtocolType},
	)

	refs := []gwv1.ParentReference{{Name: "gw", SectionName: ptr(gwv1.SectionName("tcp"))}}

	older := &gwv1.TCPRoute{
		ObjectMeta: objMeta("app", "tcp"),
		Spec: gwv1.TCPRouteSpec{
			CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: refs},
			Rules:           []gwv1.TCPRouteRule{{BackendRefs: []gwv1.BackendRef{backendRef("tcp", 3000)}}},
		},
	}
	older.CreationTimestamp = metav1.NewTime(time.Now().Add(-time.Hour))

	newer := older.DeepCopy()
	newer.Name = "tcp-newer"
	newer.CreationTimestamp = metav1.NewTime(time.Now())
	newer.Spec.Rules[0].BackendRefs = []gwv1.BackendRef{backendRef("missing", 1)}

	udpRoute := &gwv1.UDPRoute{
		ObjectMeta: objMeta("app", "udp"),
		Spec: gwv1.UDPRouteSpec{
			CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: []gwv1.ParentReference{{Name: "gw", SectionName: ptr(gwv1.SectionName("udp"))}}},
			Rules:           []gwv1.UDPRouteRule{{BackendRefs: []gwv1.BackendRef{backendRef("udp", 53)}}},
		},
	}

	local := freePort(t)

	res := applyTranslate(t, &Config{Address: "127.0.0.1", PortOffset: local - 5300}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gw},
		TCPRoutes:      []*gwv1.TCPRoute{newer, older},
		UDPRoutes:      []*gwv1.UDPRoute{udpRoute},
		Services:       svcs,
		EndpointSlices: slices,
	})

	if l := res.Gateways[nsName{"app", "gw"}].Listeners[0]; l.AttachedRoutes != 2 {
		t.Fatalf("tcp attached = %d", l.AttachedRoutes)
	}

	newerStatus := res.Routes[routeKey{Kind: kindTCPRoute, nsName: nsName{"app", "tcp-newer"}}]
	if c := condition(t, newerStatus[0].Conditions, string(gwv1.RouteConditionResolvedRefs)); c.Reason != string(gwv1.RouteReasonBackendNotFound) {
		t.Fatalf("newer route resolved = %v", c)
	}

	addr := "127.0.0.1:" + strconv.Itoa(local)

	// the oldest route receives TCP traffic
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if got := roundTrip(t, c); got != "tcp:hi" {
		t.Fatalf("tcp got %q", got)
	}
	_ = c.Close()

	// UDP on the same port number
	u, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = u.Close() }()

	_ = u.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = u.Write([]byte("ping"))

	buf := make([]byte, 64)
	n, err := u.Read(buf)
	if err != nil || string(buf[:n]) != "udp:ping" {
		t.Fatalf("udp got %q %v", buf[:n], err)
	}

	// a TCP listener without routes closes connections
	c, err = net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(local+1), 2*time.Second)
	if err == nil {
		if got := roundTrip(t, c); !strings.HasPrefix(got, "read:") && !strings.HasPrefix(got, "write:") {
			t.Fatalf("idle listener answered %q", got)
		}
		_ = c.Close()
	}
}

func TestStreamRouteKindsOnListeners(t *testing.T) {
	gw := gatewayObj("app", "gw",
		gwv1.Listener{Name: "http", Port: 80, Protocol: gwv1.HTTPProtocolType},
		gwv1.Listener{Name: "tls", Port: 443, Protocol: gwv1.TLSProtocolType, TLS: &gwv1.ListenerTLSConfig{Mode: ptr(gwv1.TLSModePassthrough)},
			AllowedRoutes: &gwv1.AllowedRoutes{Kinds: []gwv1.RouteGroupKind{{Kind: kindTCPRoute}, {Kind: kindTLSRoute}}}},
	)

	tcpOnHTTP := &gwv1.TCPRoute{
		ObjectMeta: objMeta("app", "tcp"),
		Spec:       gwv1.TCPRouteSpec{CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: []gwv1.ParentReference{{Name: "gw", SectionName: ptr(gwv1.SectionName("http"))}}}},
	}

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gw},
		TCPRoutes:      []*gwv1.TCPRoute{tcpOnHTTP},
	})

	c := condition(t, res.Routes[routeKey{Kind: kindTCPRoute, nsName: nsName{"app", "tcp"}}][0].Conditions, string(gwv1.RouteConditionAccepted))
	if c.Reason != string(gwv1.RouteReasonNotAllowedByListeners) {
		t.Fatalf("tcp on http listener = %v", c)
	}

	tlsListener := res.Gateways[nsName{"app", "gw"}].Listeners[1]
	if len(tlsListener.SupportedKinds) != 1 || tlsListener.SupportedKinds[0].Kind != kindTLSRoute {
		t.Fatalf("supported kinds = %v", tlsListener.SupportedKinds)
	}

	if c := condition(t, tlsListener.Conditions, string(gwv1.ListenerConditionResolvedRefs)); c.Reason != string(gwv1.ListenerReasonInvalidRouteKinds) {
		t.Fatalf("resolved = %v", c)
	}

	httpKinds := res.Gateways[nsName{"app", "gw"}].Listeners[0].SupportedKinds
	if len(httpKinds) != 2 {
		t.Fatalf("http supported kinds = %v", httpKinds)
	}
}

func TestGRPCRoute(t *testing.T) {
	start := func(name string) string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}

		gs := grpc.NewServer()
		hs := health.NewServer()
		hs.SetServingStatus(name, healthpb.HealthCheckResponse_SERVING)
		healthpb.RegisterHealthServer(gs, hs)

		go func() { _ = gs.Serve(l) }()
		t.Cleanup(gs.Stop)

		return l.Addr().String()
	}

	v1 := start("v1")
	v2 := start("v2")

	svcs, slices := backendObjects(backend{name: "v1", addr: v1, port: 8080}, backend{name: "v2", addr: v2, port: 8080})

	route := &gwv1.GRPCRoute{
		ObjectMeta: objMeta("app", "grpc"),
		Spec: gwv1.GRPCRouteSpec{
			CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: []gwv1.ParentReference{{Name: "gw"}}},
			Rules: []gwv1.GRPCRouteRule{
				{
					Matches: []gwv1.GRPCRouteMatch{{
						Method:  &gwv1.GRPCMethodMatch{Service: ptr("grpc.health.v1.Health"), Method: ptr("Check")},
						Headers: []gwv1.GRPCHeaderMatch{{Name: "version", Value: "two"}},
					}},
					BackendRefs: []gwv1.GRPCBackendRef{{BackendRef: backendRef("v2", 8080)}},
				},
				{
					Matches:     []gwv1.GRPCRouteMatch{{Method: &gwv1.GRPCMethodMatch{Service: ptr("grpc.health.v1.Health"), Method: ptr("Check")}}},
					BackendRefs: []gwv1.GRPCBackendRef{{BackendRef: backendRef("v1", 8080)}},
				},
			},
		},
	}

	// an HTTPRoute on the same listener keeps serving plain HTTP
	httpRoute := routeObj("app", "web", "gw", nil, backendRule("/", "v1", 8080))

	local := freePort(t)

	applyTranslate(t, &Config{Address: "127.0.0.1", PortOffset: local - 80}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
		GRPCRoutes:     []*gwv1.GRPCRoute{route},
		HTTPRoutes:     []*gwv1.HTTPRoute{httpRoute},
		Services:       svcs,
		EndpointSlices: slices,
	})

	conn, err := grpc.NewClient("127.0.0.1:"+strconv.Itoa(local), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	client := healthpb.NewHealthClient(conn)

	check := func(service string, md ...string) (healthpb.HealthCheckResponse_ServingStatus, codes.Code) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if len(md) > 0 {
			ctx = metadataContext(ctx, md...)
		}

		res, err := client.Check(ctx, &healthpb.HealthCheckRequest{Service: service})
		if err != nil {
			return 0, status.Code(err)
		}

		return res.GetStatus(), codes.OK
	}

	// v1 knows "v1", v2 knows "v2"; unknown services answer NotFound
	if s, code := check("v1"); code != codes.OK || s != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("default route: %v %v", s, code)
	}

	if s, code := check("v2", "version", "two"); code != codes.OK || s != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("header route: %v %v", s, code)
	}

	if _, code := check("v2"); code != codes.NotFound {
		t.Fatalf("v2 service on v1 backend: %v", code)
	}

	// unmatched method is Unimplemented
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream, err := client.Watch(ctx, &healthpb.HealthCheckRequest{Service: "v1"})
	if err == nil {
		_, err = stream.Recv()
	}

	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("unmatched method: %v", err)
	}

	// plain HTTP on the same port reaches the HTTPRoute
	res, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://127.0.0.1:" + strconv.Itoa(local) + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	if res.ProtoMajor != 1 {
		t.Fatalf("http proto = %s", res.Proto)
	}
}

func metadataContext(ctx context.Context, kv ...string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, kv...)
}
