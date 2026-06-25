package loader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	okclient "github.com/rakunlabs/ok"
)

// httpClient is a small wrapper over the ok HTTP client used by loads.
//
// Clients are created lazily and cached by their TLS verification mode so
// they can be reused across multiple http sources in a single Load call.
type httpClient struct {
	secure   *okclient.Client
	insecure *okclient.Client
}

func (c *httpClient) get(insecure bool) (*okclient.Client, error) {
	if insecure {
		if c.insecure == nil {
			client, err := okclient.New(okclient.WithInsecureSkipVerify(true))
			if err != nil {
				return nil, fmt.Errorf("failed to create http client: %w", err)
			}

			c.insecure = client
		}

		return c.insecure, nil
	}

	if c.secure == nil {
		client, err := okclient.New()
		if err != nil {
			return nil, fmt.Errorf("failed to create http client: %w", err)
		}

		c.secure = client
	}

	return c.secure, nil
}

// loadRaw fetches the configured URL and returns the body and Content-Type.
func (c *httpClient) loadRaw(ctx context.Context, cfg *ConfigHTTP) ([]byte, string, error) {
	client, err := c.get(cfg.InsecureSkipVerify)
	if err != nil {
		return nil, "", err
	}

	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	method := cfg.Method
	if method == "" {
		method = http.MethodGet
	}

	reqURL, err := buildURL(cfg.URL, cfg.Query)
	if err != nil {
		return nil, "", err
	}

	var body io.Reader
	if cfg.Body != "" {
		body = strings.NewReader(cfg.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	var (
		data        []byte
		contentType string
	)

	if err := client.Do(req, func(r *http.Response) error {
		if r.StatusCode < http.StatusOK || r.StatusCode >= http.StatusMultipleChoices {
			return okclient.ErrResponse(r)
		}

		b, err := io.ReadAll(r.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}

		data = b
		contentType = r.Header.Get("Content-Type")

		return nil
	}); err != nil {
		return nil, "", fmt.Errorf("failed to do http request %s: %w", cfg.URL, err)
	}

	return data, contentType, nil
}

// buildURL merges the given query parameters into rawURL.
func buildURL(rawURL string, query map[string]string) (string, error) {
	if len(query) == 0 {
		return rawURL, nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse url %s: %w", rawURL, err)
	}

	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}

	u.RawQuery = q.Encode()

	return u.String(), nil
}
