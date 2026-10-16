package docker

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// client talks to the Docker Engine API. Only the read endpoints used by the
// provider are implemented, so the provider does not depend on the Docker
// SDK.
type client struct {
	http    *http.Client
	stream  *http.Client
	base    string
	version string
}

func newClient(cfg *Config) (*client, error) {
	endpoint := cfg.endpoint()

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("endpoint %q: %w", endpoint, err)
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default transport is not *http.Transport")
	}

	transport = transport.Clone()
	transport.Proxy = nil

	c := &client{}

	switch u.Scheme {
	case "unix":
		path := u.Path
		if path == "" {
			path = u.Host
		}

		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer

			return d.DialContext(ctx, "unix", path)
		}
		c.base = "http://docker"
	case "tcp", "http", "https":
		tlsCfg := cfg.TLS
		if u.Scheme == "tcp" {
			tlsCfg = cfg.tls()
		}

		scheme := "http"
		if u.Scheme == "https" || tlsCfg != nil {
			scheme = "https"
		}

		if scheme == "https" {
			tlsConfig, err := tlsCfg.config()
			if err != nil {
				return nil, err
			}

			transport.TLSClientConfig = tlsConfig
		}

		c.base = scheme + "://" + u.Host
	default:
		return nil, fmt.Errorf("endpoint %q: unsupported scheme %q", endpoint, u.Scheme)
	}

	c.http = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	// the event stream stays open, it is closed by its context
	c.stream = &http.Client{Transport: transport}

	if cfg.APIVersion != "" {
		c.version = cfg.APIVersion
	}

	return c, nil
}

func (t *TLS) config() (*tls.Config, error) {
	out := &tls.Config{MinVersion: tls.VersionTLS12}
	if t == nil {
		return out, nil
	}

	out.InsecureSkipVerify = t.InsecureSkipVerify //nolint:gosec // opt-in

	if t.CA != "" {
		pem, err := os.ReadFile(t.CA)
		if err != nil {
			return nil, fmt.Errorf("tls ca: %w", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("tls ca %s: no certificates found", t.CA)
		}

		out.RootCAs = pool
	}

	if t.Cert != "" {
		cert, err := tls.LoadX509KeyPair(t.Cert, t.Key)
		if err != nil {
			return nil, fmt.Errorf("tls cert: %w", err)
		}

		out.Certificates = []tls.Certificate{cert}
	}

	return out, nil
}

// negotiate reads the API version of the daemon unless it is pinned.
func (c *client) negotiate(ctx context.Context) error {
	if c.version != "" {
		return nil
	}

	var v struct {
		APIVersion string `json:"ApiVersion"`
	}

	if err := c.get(ctx, "/version", nil, &v); err != nil {
		return err
	}

	if v.APIVersion == "" {
		return errors.New("daemon did not report an api version")
	}

	c.version = v.APIVersion

	return nil
}

func (c *client) url(path string, query url.Values) string {
	u := c.base
	if c.version != "" {
		u += "/v" + c.version
	}

	u += path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	return u
}

func (c *client) get(ctx context.Context, path string, query url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path, query), nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return apiError(path, resp)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode: %w", path, err)
	}

	return nil
}

func apiError(path string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	var msg struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &msg) == nil && msg.Message != "" {
		return fmt.Errorf("%s: %s: %s", path, resp.Status, msg.Message)
	}

	return fmt.Errorf("%s: %s", path, resp.Status)
}

func filters(f map[string][]string) url.Values {
	b, _ := json.Marshal(f)

	return url.Values{"filters": []string{string(b)}}
}

type apiContainer struct {
	ID         string            `json:"Id"`
	Names      []string          `json:"Names"`
	Created    int64             `json:"Created"`
	Labels     map[string]string `json:"Labels"`
	State      string            `json:"State"`
	Status     string            `json:"Status"`
	HostConfig struct {
		NetworkMode string `json:"NetworkMode"`
	} `json:"HostConfig"`
	NetworkSettings struct {
		Networks map[string]apiEndpoint `json:"Networks"`
	} `json:"NetworkSettings"`
	Ports []struct {
		PrivatePort int    `json:"PrivatePort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

type apiEndpoint struct {
	IPAddress         string `json:"IPAddress"`
	GlobalIPv6Address string `json:"GlobalIPv6Address"`
}

func (c *client) containers(ctx context.Context) ([]apiContainer, error) {
	var out []apiContainer

	err := c.get(ctx, "/containers/json", filters(map[string][]string{"status": {"running"}}), &out)

	return out, err
}

type apiService struct {
	ID        string    `json:"ID"`
	CreatedAt time.Time `json:"CreatedAt"`
	Spec      struct {
		Name         string            `json:"Name"`
		Labels       map[string]string `json:"Labels"`
		EndpointSpec *struct {
			Mode string `json:"Mode"`
		} `json:"EndpointSpec"`
	} `json:"Spec"`
	Endpoint struct {
		Ports []struct {
			Protocol   string `json:"Protocol"`
			TargetPort int    `json:"TargetPort"`
		} `json:"Ports"`
		VirtualIPs []struct {
			NetworkID string `json:"NetworkID"`
			Addr      string `json:"Addr"`
		} `json:"VirtualIPs"`
	} `json:"Endpoint"`
}

func (c *client) services(ctx context.Context) ([]apiService, error) {
	var out []apiService

	err := c.get(ctx, "/services", nil, &out)

	return out, err
}

type apiTask struct {
	ID        string `json:"ID"`
	ServiceID string `json:"ServiceID"`
	Status    struct {
		State string `json:"State"`
	} `json:"Status"`
	NetworksAttachments []struct {
		Network struct {
			ID string `json:"ID"`
		} `json:"Network"`
		Addresses []string `json:"Addresses"`
	} `json:"NetworksAttachments"`
}

func (c *client) tasks(ctx context.Context) ([]apiTask, error) {
	var out []apiTask

	err := c.get(ctx, "/tasks", filters(map[string][]string{"desired-state": {"running"}}), &out)

	return out, err
}

type apiNetwork struct {
	ID      string `json:"Id"`
	Name    string `json:"Name"`
	Ingress bool   `json:"Ingress"`
}

func (c *client) networks(ctx context.Context) ([]apiNetwork, error) {
	var out []apiNetwork

	err := c.get(ctx, "/networks", nil, &out)

	return out, err
}

type apiEvent struct {
	Type   string `json:"Type"`
	Action string `json:"Action"`
}

// events calls fn for every event until ctx is done or the stream fails.
// ready is closed once the stream is open.
func (c *client) events(ctx context.Context, types []string, ready chan<- struct{}, fn func(apiEvent)) error {
	query := filters(map[string][]string{"type": types})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/events", query), nil)
	if err != nil {
		return err
	}

	resp, err := c.stream.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return apiError("/events", resp)
	}

	close(ready)

	dec := json.NewDecoder(bufio.NewReader(resp.Body))
	for {
		var ev apiEvent
		if err := dec.Decode(&ev); err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return fmt.Errorf("event stream: %w", err)
		}

		// health checks run as exec and produce a steady stream of events
		if strings.HasPrefix(ev.Action, "exec_") {
			continue
		}

		fn(ev)
	}
}
