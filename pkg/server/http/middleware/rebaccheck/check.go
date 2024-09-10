package rebaccheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	"github.com/worldline-go/klient"
)

type RebacCheck struct {
	CheckAPI string         `cfg:"check_api"`
	Public   []data.Request `cfg:"public"`

	InsecureSkipVerify bool           `cfg:"insecure_skip_verify"`
	client             *klient.Client `cfg:"-"`
}

func (m *RebacCheck) Middleware() (func(http.Handler) http.Handler, error) {
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
				httputil.HandleError(w, httputil.NewError("", nil, http.StatusForbidden))
				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}