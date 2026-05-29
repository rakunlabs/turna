package store

import (
	"net/http"
	"time"

	"github.com/rakunlabs/ada/utils/sessions"
)

func newSession(store sessions.Store, name string, opts sessions.Options) *sessions.Session {
	session := sessions.NewSession(store, name)
	session.Options = cloneOptions(opts)

	return session
}

func cloneOptions(opts sessions.Options) *sessions.Options {
	cloned := opts
	if cloned.Path == "" {
		cloned.Path = "/"
	}

	return &cloned
}

func setSessionCookie(w http.ResponseWriter, name, value string, opts *sessions.Options) {
	if opts == nil {
		opts = &sessions.Options{Path: "/"}
	}

	cookie := &http.Cookie{
		Name:        name,
		Value:       value,
		Path:        opts.Path,
		Domain:      opts.Domain,
		MaxAge:      opts.MaxAge,
		Secure:      opts.Secure,
		HttpOnly:    opts.HttpOnly,
		Partitioned: opts.Partitioned,
		SameSite:    opts.SameSite,
	}
	if cookie.MaxAge < 0 {
		cookie.Expires = time.Unix(1, 0)
	}

	http.SetCookie(w, cookie)
}
