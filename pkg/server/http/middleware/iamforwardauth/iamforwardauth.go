package iamforwardauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/worldline-go/klient"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

type IamForwardAuth struct {
	CheckAPI string `cfg:"check_api"`

	// IamMiddleware is the name of a registered IAM middleware instance.
	// When set, the check is performed directly in-process via the IAM
	// database instead of making an HTTP request to CheckAPI.
	IamMiddleware *string `cfg:"iam_middleware"`

	MethodHeader string `cfg:"method_header"`
	HostHeader   string `cfg:"host_header"`
	URIHeader    string `cfg:"uri_header"`

	// PassHeaders is a list of header names to copy from the incoming
	// request to the outgoing response on a successful auth check.
	PassHeaders []string `cfg:"pass_headers"`

	InsecureSkipVerify bool           `cfg:"insecure_skip_verify"`
	client             *klient.Client `cfg:"-"`
	iamInstance        *iam.Iam       `cfg:"-"`
}

func (m *IamForwardAuth) Init() error {
	if m.IamMiddleware != nil {
		m.iamInstance = iam.GlobalRegistry.Get(*m.IamMiddleware)
		if m.iamInstance == nil {
			return fmt.Errorf("iam middleware %q not found in registry", *m.IamMiddleware)
		}
	}

	return nil
}

func (m *IamForwardAuth) Middleware() (func(http.Handler) http.Handler, error) {
	if m.IamMiddleware == nil {
		client, err := klient.NewPlain(
			klient.WithInsecureSkipVerify(m.InsecureSkipVerify),
		)
		if err != nil {
			return nil, fmt.Errorf("cannot create klient: %w", err)
		}

		m.client = client
	}

	// Set default header names.
	if m.MethodHeader == "" {
		m.MethodHeader = "X-Forwarded-Method"
	}
	if m.HostHeader == "" {
		m.HostHeader = "X-Forwarded-Host"
	}
	if m.URIHeader == "" {
		m.URIHeader = "X-Forwarded-Uri"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract the original request details from forwarded headers.
			// These headers are mandatory — the middleware must be behind a
			// reverse proxy that sets them.
			method := r.Header.Get(m.MethodHeader)
			if method == "" {
				httputil.HandleError(w, httputil.NewError("missing header: "+m.MethodHeader, nil, http.StatusUnauthorized))
				return
			}

			host := r.Header.Get(m.HostHeader)
			if host == "" {
				httputil.HandleError(w, httputil.NewError("missing header: "+m.HostHeader, nil, http.StatusUnauthorized))
				return
			}

			uri := r.Header.Get(m.URIHeader)
			if uri == "" {
				httputil.HandleError(w, httputil.NewError("missing header: "+m.URIHeader, nil, http.StatusUnauthorized))
				return
			}

			// Parse the URI to extract only the path component,
			// in case it contains query parameters.
			path := uri
			if parsedURL, err := url.ParseRequestURI(uri); err == nil {
				path = parsedURL.Path
			}

			// Read user identity from X-User header.
			xUser := r.Header.Get("X-User")
			if xUser == "" {
				httputil.HandleError(w, httputil.NewError("", nil, http.StatusUnauthorized))
				return
			}

			ctx := r.Context()

			body := data.CheckRequest{
				Alias:  xUser,
				Path:   path,
				Method: method,
				Host:   host,
			}

			var allowed bool

			if m.iamInstance != nil {
				// Direct in-process check via IAM database.
				resp, err := m.iamInstance.DB().Check(body)
				if err != nil {
					httputil.HandleError(w, httputil.NewError("check failed", err, http.StatusInternalServerError))
					return
				}

				allowed = resp.Allowed
			} else {
				// HTTP-based check via CheckAPI.
				jsonBody, err := json.Marshal(body)
				if err != nil {
					httputil.HandleError(w, httputil.NewError("Cannot marshal request", err, http.StatusInternalServerError))
					return
				}

				req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.CheckAPI, bytes.NewReader(jsonBody))
				if err != nil {
					httputil.HandleError(w, httputil.NewError("Cannot create request", err, http.StatusInternalServerError))
					return
				}

				var resp data.CheckResponse
				if err := m.client.Do(req, func(r *http.Response) error {
					if r.StatusCode != http.StatusOK {
						response := httputil.Response{}
						if err := json.NewDecoder(r.Body).Decode(&response); err != nil {
							return httputil.NewError("Cannot decode response", err, http.StatusInternalServerError)
						}

						return httputil.NewError(response.Msg, nil, http.StatusInternalServerError)
					}

					if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
						return httputil.NewError("Cannot decode response", err, http.StatusInternalServerError)
					}

					return nil
				}); err != nil {
					httputil.HandleError(w, httputil.NewErrorAs(err))
					return
				}

				allowed = resp.Allowed
			}

			if !allowed {
				httputil.HandleError(w, httputil.NewError("", nil, http.StatusForbidden))
				return
			}

			// Copy configured headers from the request to the response.
			for _, h := range m.PassHeaders {
				if v := r.Header.Get(h); v != "" {
					w.Header().Set(h, v)
				}
			}

			// Forward auth succeeded - return 200 OK to the proxy.
			w.WriteHeader(http.StatusOK)
		})
	}, nil
}
