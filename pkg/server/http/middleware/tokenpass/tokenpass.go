package tokenpass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
	"github.com/rytsh/mugo/fstore"
	"github.com/rytsh/mugo/templatex"
	"github.com/worldline-go/klient"
	"github.com/worldline-go/logz"
	"gopkg.in/yaml.v3"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/model"
)

type TokenPass struct {
	SecretKey     string `cfg:"secret_key"`
	SigningMethod string `cfg:"signing_method"`
	// Payload with go template as claims
	Payload string `cfg:"payload"`

	// Redirect URL with go template
	RedirectURL      string `cfg:"redirect_url"`
	RedirectWithCode bool   `cfg:"redirect_with_code"`
	Method           string `cfg:"method"`
	EnableBody       bool   `cfg:"enable_body"`
	BodyRaw          bool   `cfg:"body_raw"`
	BodyTemplate     string `cfg:"body"`

	AdditionalValues   interface{} `cfg:"additional_values"`
	DefaultExpDuration string      `cfg:"default_exp_duration"`

	InsecureSkipVerify bool              `cfg:"insecure_skip_verify"`
	EnableRetry        bool              `cfg:"enable_retry"`
	Headers            map[string]string `cfg:"headers"`

	DebugToken   bool `cfg:"debug_token"`
	DebugPayload bool `cfg:"debug_payload"`

	tpl *templatex.Template
}

func (m *TokenPass) data(r *http.Request, body []byte) (map[string]interface{}, error) {
	var bodyMap interface{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &bodyMap); err != nil {
			return nil, err
		}
		bodyMap = body
	}

	// get all data for the template
	return map[string]interface{}{
		"body":         bodyMap,
		"body_raw":     body,
		"method":       r.Method,
		"headers":      r.Header,
		"query_params": r.URL.Query(),
		"cookies":      r.Cookies(),
		"path":         r.URL.Path,
		"values":       m.AdditionalValues,
	}, nil
}

func (m *TokenPass) render(data map[string]interface{}, content string) ([]byte, error) {
	var buf bytes.Buffer
	if err := m.tpl.Execute(
		templatex.WithIO(&buf),
		templatex.WithContent(content),
		templatex.WithData(data),
	); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (m *TokenPass) Middleware() (func(http.Handler) http.Handler, error) {
	defaultExpDuration, err := time.ParseDuration(m.DefaultExpDuration)
	if err != nil {
		return nil, err
	}

	m.tpl = templatex.New(templatex.WithAddFuncsTpl(
		fstore.FuncMapTpl(
			fstore.WithLog(logz.AdapterKV{Log: log.Logger}),
		),
	))

	jwtMethod := jwt.GetSigningMethod(m.SigningMethod)
	if jwtMethod == nil {
		jwtMethod = jwt.SigningMethodHS256
	}

	client, err := klient.New(
		klient.WithDisableBaseURLCheck(true),
		klient.WithInsecureSkipVerify(m.InsecureSkipVerify),
		klient.WithDisableRetry(!m.EnableRetry),
		klient.WithDisableEnvValues(true),
		klient.WithLogger(slog.Default()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create klient client: %w", err)
	}

	if m.Method == "" {
		m.Method = http.MethodGet
	} else {
		m.Method = strings.ToUpper(m.Method)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// render Payload
			body, err := io.ReadAll(r.Body)
			if err != nil {
				httputil.JSON(w, http.StatusBadRequest, model.MetaData{Message: err.Error()})

				return
			}

			data, err := m.data(r, body)
			if err != nil {
				httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

				return
			}

			payload, err := m.render(data, m.Payload)
			if err != nil {
				httputil.JSON(w, http.StatusBadRequest, model.MetaData{Message: err.Error()})

				return
			}

			if m.DebugPayload {
				slog.Debug(fmt.Sprintf("payload: %q", payload))
			}

			claims := jwt.MapClaims{}
			if err := yaml.Unmarshal(payload, &claims); err != nil {
				httputil.JSON(w, http.StatusBadRequest, model.MetaData{Message: err.Error()})

				return
			}

			if _, ok := claims["exp"]; !ok && defaultExpDuration > 0 {
				claims["exp"] = time.Now().Add(defaultExpDuration).Unix()
			}

			token := jwt.NewWithClaims(jwtMethod, claims)
			tokenString, err := token.SignedString([]byte(m.SecretKey))
			if err != nil {
				httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: fmt.Sprintf("secretKey %s", err.Error())})

				return
			}

			if m.DebugToken {
				slog.Debug(fmt.Sprintf("token: %q", tokenString))
			}

			data["token"] = tokenString

			redirectURL, err := m.render(data, m.RedirectURL)
			if err != nil {
				httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

				return
			}

			if m.RedirectWithCode {
				httputil.Redirect(w, http.StatusTemporaryRedirect, string(redirectURL))

				return
			}

			// //////////////////////////
			// call directly
			var requestBody io.Reader
			if m.EnableBody {
				if m.BodyRaw {
					requestBody = bytes.NewReader(body)
				} else {
					bodyRendered, err := m.render(data, m.BodyTemplate)
					if err != nil {
						httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

						return
					}

					requestBody = bytes.NewReader(bodyRendered)
				}
			}

			request, err := http.NewRequestWithContext(r.Context(), m.Method, string(redirectURL), requestBody)
			if err != nil {
				httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

				return
			}

			for k, v := range m.Headers {
				request.Header.Set(k, v)
			}

			var retStatus int
			var retBody []byte
			var retHeaders http.Header

			if err := client.Do(request, func(r *http.Response) error {
				retStatus = r.StatusCode
				retBody, err = io.ReadAll(r.Body)
				if err != nil {
					return err
				}

				retHeaders = r.Header

				return nil
			}); err != nil {
				httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

				return
			}

			header := w.Header()
			for k, v := range retHeaders {
				header[k] = v
			}

			w.WriteHeader(retStatus)
			w.Write(retBody)
		})
	}, nil
}
