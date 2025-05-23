package info

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

type Info struct {
	// Cookie to show.
	Cookie string `cfg:"cookie"`
	// Session cookie.
	Session bool `cfg:"session"`
	// SessionMiddleware to use, default is "session".
	SessionMiddleware string `cfg:"session_middleware"`
	// SessionValueName to get from session, default is "token".
	SessionValueName string `cfg:"session_value_name"`
	// Base64 decode when reading cookie.
	Base64 bool `cfg:"base64"`
	// Raw cookie to show
	Raw bool `cfg:"raw"`

	session *session.Session
}

func (m *Info) Init() error {
	m.session = session.GlobalRegistry.Get(m.SessionMiddleware)
	if m.session == nil {
		return errors.New("session middleware not found")
	}

	return nil
}

func (s *Info) Middleware() func(http.Handler) http.Handler {
	if s.SessionValueName == "" {
		s.SessionValueName = "token"
	}

	if s.SessionMiddleware == "" {
		s.SessionMiddleware = "session"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var retValue []byte

			if s.Session {
				if vGet, err := s.session.GetStore().Get(r, s.Cookie); !vGet.IsNew && err == nil {
					// add the access token to the request
					vStr, _ := vGet.Values[s.SessionValueName].(string)
					retValue = []byte(vStr)
				}
			} else {
				cookie, err := r.Cookie(s.Cookie)
				if err != nil {
					httputil.HandleError(w, httputil.NewError(fmt.Sprintf("cookie %s not found", s.Cookie), err, http.StatusNotFound))

					return
				}

				retValue = []byte(cookie.Value)
			}

			if s.Base64 {
				var err error
				retValue, err = base64.StdEncoding.DecodeString(string(retValue))
				if err != nil {
					httputil.HandleError(w, httputil.NewError("base64 decode error", err, http.StatusBadRequest))

					return
				}
			}

			if s.Raw {
				httputil.Blob(w, http.StatusOK, "text/plain", retValue)

				return
			}

			httputil.JSONBlob(w, http.StatusOK, json.RawMessage(retValue))
		})
	}
}
