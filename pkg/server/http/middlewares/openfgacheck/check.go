package openfgacheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth/claims"
	"github.com/worldline-go/klient"

	"github.com/rakunlabs/turna/pkg/server/model"
)

type OpenFGACheck struct {
	OpenFGACheckAPI string    `cfg:"openfga_check_api"`
	OpenFGAUserAPI  string    `cfg:"openfga_user_api"`
	OpenFGAModelID  string    `cfg:"openfga_model_id"`
	Operation       Operation `cfg:"operation"`

	InsecureSkipVerify bool           `cfg:"insecure_skip_verify"`
	client             *klient.Client `cfg:"-"`
}

type Operation struct {
	Parse Parse `cfg:"parse"`
}

type Parse struct {
	// Enable to enable the Parse check.
	Enable bool `cfg:"enable"`
	// APINameRgx to extract name of the api.
	APINameRgx         string `cfg:"api_name_rgx"`
	APINameReplacement string `cfg:"api_name_replacement"`
	ObjectName         string `cfg:"object_name"`
	// Method match with relation.
	Method Method `cfg:"method"`
	// DefaultUserClaim claim to extract user alias from token.
	DefaultUserClaim  string            `cfg:"default_user_claim"`
	ProviderUserClaim map[string]string `cfg:"provider_user_claim"`
}

type Method struct {
	HEAD    string `cfg:"head"`
	OPTIONS string `cfg:"options"`
	CONNECT string `cfg:"connect"`
	TRACE   string `cfg:"trace"`
	GET     string `cfg:"get"`

	POST   string `cfg:"post"`
	PATCH  string `cfg:"patch"`
	PUT    string `cfg:"put"`
	DELETE string `cfg:"delete"`
}

func (m *Method) Value(method string) (string, error) {
	switch strings.ToUpper(method) {
	case http.MethodHead:
		return m.HEAD, nil
	case http.MethodOptions:
		return m.OPTIONS, nil
	case http.MethodConnect:
		return m.CONNECT, nil
	case http.MethodTrace:
		return m.TRACE, nil
	case http.MethodGet:
		return m.GET, nil
	case http.MethodPost:
		return m.POST, nil
	case http.MethodPatch:
		return m.PATCH, nil
	case http.MethodPut:
		return m.PUT, nil
	case http.MethodDelete:
		return m.DELETE, nil
	}

	return "", fmt.Errorf("unknown method %s", method)
}

func (m *OpenFGACheck) Middleware(_ context.Context, _ string) (echo.MiddlewareFunc, error) {
	// set auth client
	client, err := klient.New(
		klient.WithDisableBaseURLCheck(true),
		klient.WithDisableRetry(true),
		klient.WithDisableEnvValues(true),
		klient.WithInsecureSkipVerify(m.InsecureSkipVerify),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create klient: %w", err)
	}

	m.client = client

	var pathRgx *regexp.Regexp
	if m.Operation.Parse.Enable {
		if m.Operation.Parse.APINameRgx == "" {
			return nil, fmt.Errorf("api name regex is required")
		}

		pathRgx, err = regexp.Compile(m.Operation.Parse.APINameRgx)
		if err != nil {
			return nil, fmt.Errorf("cannot compile api name regex: %w", err)
		}
	}

	m.Operation.Parse.ObjectName = "api"

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()
			if m.Operation.Parse.Enable {
				relation, err := m.Operation.Parse.Method.Value(c.Request().Method)
				if err != nil {
					return c.JSON(http.StatusMethodNotAllowed, model.MetaData{Error: err.Error()})
				}

				claimValue, ok := c.Get("claims").(*claims.Custom)
				if !ok {
					return c.JSON(http.StatusUnauthorized, model.MetaData{Error: "claims not found"})
				}

				// log.Debug().Interface("claims", claimValue).Msg("claims")

				var userClaim string

				provider, _ := c.Get("provider").(string)
				if provider != "" {
					providerClaimCheck := m.Operation.Parse.ProviderUserClaim[provider]
					if providerClaimCheck != "" {
						userClaim, ok = claimValue.Map[providerClaimCheck].(string)
						if !ok {
							return c.JSON(http.StatusUnauthorized, model.MetaData{Error: "provider user claim not found"})
						}
					}
				}

				if userClaim == "" {
					userClaim, ok = claimValue.Map[m.Operation.Parse.DefaultUserClaim].(string)
					if !ok {
						return c.JSON(http.StatusUnauthorized, model.MetaData{Error: "user claim not found"})
					}
				}

				// userClaim should be one word
				userClaim = strings.Split(userClaim, " ")[0]

				// get userID with userClaim
				var userID string
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.OpenFGAUserAPI, nil)
				if err != nil {
					return c.JSON(http.StatusInternalServerError, model.MetaData{Error: err.Error()})
				}

				query := req.URL.Query()
				query.Set("alias", userClaim)
				req.URL.RawQuery = query.Encode()

				if err := m.client.Do(req, func(r *http.Response) error {
					if r.StatusCode == http.StatusNotFound {
						return nil
					}

					if r.StatusCode != http.StatusOK {
						return fmt.Errorf("openfga check failed: %s", r.Status)
					}

					var resp UserResponse
					if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
						return fmt.Errorf("cannot decode openfga response: %w", err)
					}

					userID = resp.ID

					return nil
				}); err != nil {
					return c.JSON(http.StatusInternalServerError, model.MetaData{Error: err.Error()})
				}

				if userID == "" {
					return c.JSON(http.StatusUnauthorized, model.MetaData{Error: "user not found"})
				}

				// path regex
				apiName := pathRgx.ReplaceAllString(c.Request().URL.Path, m.Operation.Parse.APINameReplacement)

				// send request to openfga to check
				check, err := json.Marshal(Check{
					TupleKey: TupleKey{
						User:     "user:" + userID,
						Relation: relation,
						Object:   m.Operation.Parse.ObjectName + ":" + apiName,
					},
					AuthorizationModelID: m.OpenFGAModelID,
				})
				if err != nil {
					return c.JSON(http.StatusInternalServerError, model.MetaData{Error: err.Error()})
				}

				req, err = http.NewRequestWithContext(ctx, http.MethodPost, m.OpenFGACheckAPI, bytes.NewReader(check))
				if err != nil {
					return c.JSON(http.StatusInternalServerError, model.MetaData{Error: err.Error()})
				}

				var resp CheckResponse
				if err := m.client.Do(req, func(r *http.Response) error {
					if r.StatusCode != http.StatusOK {
						return fmt.Errorf("openfga check failed: %s", r.Status)
					}

					if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
						return fmt.Errorf("cannot decode openfga response: %w", err)
					}

					return nil
				}); err != nil {
					return c.JSON(http.StatusInternalServerError, model.MetaData{Error: err.Error()})
				}

				if !resp.Allowed {
					return c.JSON(http.StatusForbidden, model.MetaData{Error: "forbidden"})
				}
			}

			return next(c)
		}
	}, nil
}
