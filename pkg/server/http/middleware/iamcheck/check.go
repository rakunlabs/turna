package iamcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/rakunlabs/ok"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

type IamCheck struct {
	CheckAPI string `cfg:"check_api"`
	// AuthMiddleware performs checks directly against an auth middleware in
	// the same process. It takes precedence over CheckAPI.
	AuthMiddleware string          `cfg:"auth_middleware"`
	Public         []data.Resource `cfg:"public"`
	Responses      []Response      `cfg:"responses"`
	ForceHost      string          `cfg:"force_host"`

	InsecureSkipVerify bool       `cfg:"insecure_skip_verify"`
	client             *ok.Client `cfg:"-"`
}

type Response struct {
	Path    string   `cfg:"path"`
	Methods []string `cfg:"methods"`

	// Message adds custom message to the response
	Message string `cfg:"message"`
	// Redirect is not empty, it will redirect to the given URL
	Redirect string `cfg:"redirect"`
}

func (m *IamCheck) Middleware() (func(http.Handler) http.Handler, error) {
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

	// fill paths on public resources
	for i := range m.Public {
		if m.Public[i].Path != "" {
			m.Public[i].Paths = append(m.Public[i].Paths, m.Public[i].Path)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hostToCheck := r.Host
			if m.ForceHost != "" {
				hostToCheck = m.ForceHost
			}

			// check if the path is public
			for _, resource := range m.Public {
				// check hosts
				matchedHost := false
				if len(resource.Hosts) == 0 {
					matchedHost = true
				} else {
					for _, host := range resource.Hosts {
						if v, _ := doublestar.Match(host, hostToCheck); v {
							matchedHost = true
							break
						}
					}
				}

				if matchedHost {
					// check path
					for _, publicPath := range resource.Paths {
						if v, _ := doublestar.Match(publicPath, r.URL.Path); v {
							if len(resource.Methods) == 0 || slices.ContainsFunc(resource.Methods, func(cmp string) bool {
								return strings.EqualFold(cmp, r.Method)
							}) {
								next.ServeHTTP(w, r)
								return
							}
						}
					}
				}
			}

			xUser := r.Header.Get("X-User")
			allowed, err := m.allowed(r.Context(), xUser, hostToCheck, r.URL.Path, r.Method)
			if err != nil {
				httputil.HandleError(w, httputil.NewErrorAs(err))
				return
			}

			if !allowed {
				for _, response := range m.Responses {
					if v, _ := doublestar.Match(response.Path, r.URL.Path); v {
						if len(response.Methods) == 0 || slices.ContainsFunc(response.Methods, func(cmp string) bool {
							return strings.EqualFold(cmp, r.Method)
						}) {
							if response.Redirect != "" {
								http.Redirect(w, r, response.Redirect, http.StatusTemporaryRedirect)
								return
							}

							httputil.HandleError(w, httputil.NewError(response.Message, nil, http.StatusForbidden))
							return
						}
					}
				}

				status := http.StatusForbidden
				if xUser == "" {
					status = http.StatusUnauthorized
				}

				httputil.HandleError(w, httputil.NewError("", nil, status))
				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

func (m *IamCheck) allowed(ctx context.Context, alias, host, path, method string) (bool, error) {
	if m.AuthMiddleware != "" {
		issuer := session.IssuerRegistry.Get(m.AuthMiddleware)
		checker, ok := issuer.(session.InfAccessChecker)
		if !ok {
			return false, httputil.NewError(
				fmt.Sprintf("auth middleware %q does not support access checks", m.AuthMiddleware),
				nil,
				http.StatusInternalServerError,
			)
		}

		return checker.AccessAllowed(ctx, alias, host, path, method)
	}

	body, err := json.Marshal(data.CheckRequest{Alias: alias, Host: host, Path: path, Method: method})
	if err != nil {
		return false, httputil.NewError("cannot marshal check request", err, http.StatusInternalServerError)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.CheckAPI, bytes.NewReader(body))
	if err != nil {
		return false, httputil.NewError("cannot create check request", err, http.StatusInternalServerError)
	}
	req.Header.Set("Content-Type", "application/json")
	if alias != "" {
		req.Header.Set("X-User", alias)
	}

	var resp data.CheckResponse
	if err := m.client.Do(req, func(r *http.Response) error {
		if r.StatusCode != http.StatusOK {
			// Older IAM check APIs require alias/id and answer 4xx for
			// anonymous checks. Preserve the historic iam_check result (401).
			if alias == "" && r.StatusCode >= http.StatusBadRequest && r.StatusCode < http.StatusInternalServerError {
				return nil
			}

			return httputil.NewError(
				fmt.Sprintf("check API %q returned %s", m.CheckAPI, r.Status),
				nil,
				http.StatusBadGateway,
			)
		}

		if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
			return httputil.NewError("check API returned an invalid response", err, http.StatusBadGateway)
		}

		return nil
	}); err != nil {
		var httpErr httputil.Error
		if errors.As(err, &httpErr) {
			return false, httpErr
		}

		return false, httputil.NewError("check API request failed", err, http.StatusBadGateway)
	}

	return resp.Allowed, nil
}
