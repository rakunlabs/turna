package iamforwardauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/rakunlabs/ok"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

type IamForwardAuth struct {
	CheckAPI string `cfg:"check_api"`

	// AuthMiddleware performs checks directly against an auth middleware in
	// the same process. It takes precedence over CheckAPI.
	AuthMiddleware string `cfg:"auth_middleware"`

	MethodHeader string `cfg:"method_header"`
	HostHeader   string `cfg:"host_header"`
	URIHeader    string `cfg:"uri_header"`

	// PassHeaders is a list of header names to copy from the incoming
	// request to the outgoing response on a successful auth check.
	PassHeaders []string `cfg:"pass_headers"`

	InsecureSkipVerify bool       `cfg:"insecure_skip_verify"`
	client             *ok.Client `cfg:"-"`
}

func (m *IamForwardAuth) Middleware() (func(http.Handler) http.Handler, error) {
	if m.AuthMiddleware == "" {
		if m.CheckAPI == "" {
			return nil, fmt.Errorf("check_api or auth_middleware is required")
		}

		client, err := ok.New(
			ok.WithDisableRetry(true),
			ok.WithInsecureSkipVerify(m.InsecureSkipVerify),
		)
		if err != nil {
			return nil, fmt.Errorf("cannot create ok client: %w", err)
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

			if m.AuthMiddleware != "" {
				checker, ok := session.IssuerRegistry.Get(m.AuthMiddleware).(session.InfAccessChecker)
				if !ok {
					httputil.HandleError(w, httputil.NewError(
						fmt.Sprintf("auth middleware %q does not support access checks", m.AuthMiddleware),
						nil,
						http.StatusInternalServerError,
					))
					return
				}

				var err error
				allowed, err = checker.AccessAllowed(ctx, xUser, host, path, method)
				if err != nil {
					httputil.HandleError(w, httputil.NewErrorAs(err))
					return
				}
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
