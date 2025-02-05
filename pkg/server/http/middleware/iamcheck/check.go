package iamcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/worldline-go/klient"
	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
)

type IamCheck struct {
	CheckAPI  string          `cfg:"check_api"`
	Public    []data.Resource `cfg:"public"`
	Responses []Response      `cfg:"responses"`

	InsecureSkipVerify bool           `cfg:"insecure_skip_verify"`
	client             *klient.Client `cfg:"-"`
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
	// set cehck client
	client, err := klient.NewPlain(
		klient.WithInsecureSkipVerify(m.InsecureSkipVerify),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create klient: %w", err)
	}

	m.client = client

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// check if the path is public
			for _, resource := range m.Public {
				// check hosts
				matchedHost := false
				if len(resource.Hosts) == 0 {
					matchedHost = true
				} else {
					for _, host := range resource.Hosts {
						if v, _ := doublestar.Match(host, r.Host); v {
							matchedHost = true
							break
						}
					}
				}

				if matchedHost {
					// check path
					if v, _ := doublestar.Match(resource.Path, r.URL.Path); v {
						if len(resource.Methods) == 0 || slices.ContainsFunc(resource.Methods, func(cmp string) bool {
							return strings.EqualFold(cmp, r.Method)
						}) {
							next.ServeHTTP(w, r)
							return
						}
					}
				}
			}

			xUser := r.Header.Get("X-User")
			if xUser == "" {
				httputil.HandleError(w, httputil.NewError("", nil, http.StatusUnauthorized))
				return
			}

			ctx := r.Context()

			body := data.CheckRequest{
				Alias:  xUser,
				Path:   r.URL.Path,
				Method: r.Method,
				Host:   r.Host,
			}

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

			if !resp.Allowed {
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

				httputil.HandleError(w, httputil.NewError("", nil, http.StatusForbidden))
				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}
