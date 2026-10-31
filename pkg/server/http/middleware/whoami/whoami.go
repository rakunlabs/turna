package whoami

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

const (
	defaultMaxBodySize = 1 << 20  // 1MB
	defaultMaxDataSize = 10 << 20 // 10MB
	defaultMaxWait     = 30 * time.Second
)

// Whoami answers with information about the server and the incoming request,
// similar to traefik/whoami. It does not call the next middleware.
type Whoami struct {
	// Name is printed as "Name" in responses.
	Name string `cfg:"name"`
	// Env allows printing the process environment with ?env=true.
	Env bool `cfg:"env"`
	// MaxBodySize is the maximum request body echoed back, default is 1MB.
	MaxBodySize int64 `cfg:"max_body_size"`
	// MaxDataSize is the maximum size served by /data, default is 10MB.
	MaxDataSize int64 `cfg:"max_data_size"`
	// MaxWait is the maximum ?wait duration, default is 30s.
	MaxWait time.Duration `cfg:"max_wait"`
}

type apiResponse struct {
	Name       string              `json:"name,omitempty"`
	Hostname   string              `json:"hostname"`
	IP         []string            `json:"ip"`
	Headers    map[string][]string `json:"headers"`
	URL        string              `json:"url"`
	Host       string              `json:"host"`
	Method     string              `json:"method"`
	Proto      string              `json:"proto"`
	RemoteAddr string              `json:"remoteAddr"`
	TLS        *tlsInfo            `json:"tls,omitempty"`
	Environ    []string            `json:"environ,omitempty"`
}

type tlsInfo struct {
	Version     string   `json:"version"`
	CipherSuite string   `json:"cipherSuite"`
	ServerName  string   `json:"serverName,omitempty"`
	Protocol    string   `json:"protocol,omitempty"`
	PeerSubject []string `json:"peerSubject,omitempty"`
}

func (m *Whoami) Middleware() (func(http.Handler) http.Handler, error) {
	if m.MaxBodySize == 0 {
		m.MaxBodySize = defaultMaxBodySize
	}

	if m.MaxDataSize == 0 {
		m.MaxDataSize = defaultMaxDataSize
	}

	if m.MaxWait == 0 {
		m.MaxWait = defaultMaxWait
	}

	if m.MaxBodySize < 0 || m.MaxDataSize < 0 || m.MaxWait < 0 {
		return nil, errors.New("whoami limits must not be negative")
	}

	var health atomic.Int64
	health.Store(http.StatusOK)

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !m.wait(w, r) {
				return
			}

			switch path.Base(r.URL.Path) {
			case "api":
				m.api(w, r)
			case "health":
				m.health(w, r, &health)
			case "data":
				m.data(w, r)
			case "bench":
				w.Header().Set("Connection", "keep-alive")
				_ = httputil.Blob(w, http.StatusOK, httputil.MIMETextPlainCharsetUTF8, []byte("1"))
			default:
				m.text(w, r)
			}
		})
	}, nil
}

func (m *Whoami) wait(w http.ResponseWriter, r *http.Request) bool {
	v := r.URL.Query().Get("wait")
	if v == "" {
		return true
	}

	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		httputil.HandleError(w, httputil.NewError("invalid wait duration", err, http.StatusBadRequest))

		return false
	}

	d = min(d, m.MaxWait)

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-r.Context().Done():
		return false
	}
}

func (m *Whoami) showEnv(r *http.Request) bool {
	if !m.Env {
		return false
	}

	ok, _ := strconv.ParseBool(r.URL.Query().Get("env"))

	return ok
}

func (m *Whoami) text(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, m.MaxBodySize))
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot read body", err, http.StatusBadRequest))

		return
	}

	var buf bytes.Buffer

	if m.Name != "" {
		fmt.Fprintln(&buf, "Name:", m.Name)
	}

	hostname, _ := os.Hostname()
	fmt.Fprintln(&buf, "Hostname:", hostname)

	for _, ip := range ips() {
		fmt.Fprintln(&buf, "IP:", ip)
	}

	fmt.Fprintln(&buf, "RemoteAddr:", r.RemoteAddr)

	if info := newTLSInfo(r.TLS); info != nil {
		fmt.Fprintln(&buf, "TLS:", info.Version, info.CipherSuite)

		if info.ServerName != "" {
			fmt.Fprintln(&buf, "TLS-ServerName:", info.ServerName)
		}

		for _, subject := range info.PeerSubject {
			fmt.Fprintln(&buf, "TLS-Peer:", subject)
		}
	}

	fmt.Fprintf(&buf, "%s %s %s\n", r.Method, r.URL.RequestURI(), r.Proto)
	fmt.Fprintf(&buf, "Host: %s\n", r.Host)

	keys := make([]string, 0, len(r.Header))
	for k := range r.Header {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	for _, k := range keys {
		for _, v := range r.Header[k] {
			fmt.Fprintf(&buf, "%s: %s\n", k, v)
		}
	}

	buf.WriteString("\n")

	if len(body) > 0 {
		buf.Write(body)
		buf.WriteString("\n")
	}

	if m.showEnv(r) {
		for _, env := range os.Environ() {
			fmt.Fprintln(&buf, env)
		}
	}

	_ = httputil.Blob(w, http.StatusOK, httputil.MIMETextPlainCharsetUTF8, buf.Bytes())
}

func (m *Whoami) api(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()

	resp := apiResponse{
		Name:       m.Name,
		Hostname:   hostname,
		IP:         ips(),
		Headers:    r.Header,
		URL:        r.URL.RequestURI(),
		Host:       r.Host,
		Method:     r.Method,
		Proto:      r.Proto,
		RemoteAddr: r.RemoteAddr,
		TLS:        newTLSInfo(r.TLS),
	}

	if m.showEnv(r) {
		resp.Environ = os.Environ()
	}

	_ = httputil.JSON(w, http.StatusOK, resp)
}

func (m *Whoami) health(w http.ResponseWriter, r *http.Request, health *atomic.Int64) {
	switch r.Method {
	case http.MethodPost, http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(r.Body, 16))
		if err != nil {
			httputil.HandleError(w, httputil.NewError("cannot read body", err, http.StatusBadRequest))

			return
		}

		code, err := strconv.Atoi(strings.TrimSpace(string(body)))
		// informational codes are not final responses
		if err != nil || code < 200 || code > 599 {
			httputil.HandleError(w, httputil.NewError("body must be a status code", err, http.StatusBadRequest))

			return
		}

		health.Store(int64(code))
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(int(health.Load()))
	}
}

func (m *Whoami) data(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	size := int64(1)
	if v := query.Get("size"); v != "" {
		var err error
		size, err = strconv.ParseInt(v, 10, 64)
		if err != nil || size < 0 {
			httputil.HandleError(w, httputil.NewError("invalid size", err, http.StatusBadRequest))

			return
		}
	}

	unit := int64(1)
	switch strings.ToUpper(query.Get("unit")) {
	case "", "B":
	case "KB":
		unit = 1 << 10
	case "MB":
		unit = 1 << 20
	case "GB":
		unit = 1 << 30
	default:
		httputil.HandleError(w, httputil.NewError("invalid unit, use B, KB, MB or GB", nil, http.StatusBadRequest))

		return
	}

	if size > m.MaxDataSize/unit {
		httputil.HandleError(w, httputil.NewError(fmt.Sprintf("size exceeds max data size of %d bytes", m.MaxDataSize), nil, http.StatusBadRequest))

		return
	}

	total := size * unit

	if ok, _ := strconv.ParseBool(query.Get("attachment")); ok {
		w.Header().Set("Content-Disposition", `attachment; filename="data.txt"`)
	}

	w.Header().Set(httputil.HeaderContentType, httputil.MIMETextPlainCharsetUTF8)
	w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
	w.WriteHeader(http.StatusOK)

	if r.Method == http.MethodHead {
		return
	}

	chunk := bytes.Repeat([]byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyz\n"), 512)

	for total > 0 {
		n := min(total, int64(len(chunk)))
		if _, err := w.Write(chunk[:n]); err != nil {
			return
		}

		total -= n
	}
}

func newTLSInfo(state *tls.ConnectionState) *tlsInfo {
	if state == nil {
		return nil
	}

	info := &tlsInfo{
		Version:     tls.VersionName(state.Version),
		CipherSuite: tls.CipherSuiteName(state.CipherSuite),
		ServerName:  state.ServerName,
		Protocol:    state.NegotiatedProtocol,
	}

	for _, cert := range state.PeerCertificates {
		info.PeerSubject = append(info.PeerSubject, cert.Subject.String())
	}

	return info
}

func ips() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}

	result := make([]string, 0, len(addrs))

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			result = append(result, ipNet.IP.String())
		}
	}

	return result
}
