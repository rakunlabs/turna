package url

import (
	"fmt"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
)

type URL struct {
	// Scheme modification
	Scheme string `cfg:"scheme"`

	// Host modification
	Host string `cfg:"host"`

	// Path modifications
	Path        string `cfg:"path"`
	PathPrefix  string `cfg:"path_prefix"`
	PathSuffix  string `cfg:"path_suffix"`
	StripPrefix string `cfg:"strip_prefix"`

	// Path replacement with regex
	PathReplace     string `cfg:"path_replace"`
	PathReplaceWith string `cfg:"path_replace_with"`

	// Query parameter modifications
	AddQuery    map[string]string `cfg:"add_query"`
	RemoveQuery []string          `cfg:"remove_query"`
	SetQuery    map[string]string `cfg:"set_query"`

	// Fragment modification
	Fragment string `cfg:"fragment"`

	// Port modification
	Port string `cfg:"port"`

	// compiled regex for path replacement
	pathRegex *regexp.Regexp
}

func (m *URL) Middleware() (func(http.Handler) http.Handler, error) {
	err := m.compileRegex()
	if err != nil {
		return nil, err
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			modifiedURL := *r.URL

			m.modifyScheme(&modifiedURL)
			m.modifyHost(&modifiedURL)
			m.modifyPath(&modifiedURL)
			m.modifyQuery(&modifiedURL)
			m.modifyFragment(&modifiedURL)

			r.URL = &modifiedURL

			if m.Host != "" {
				r.Host = modifiedURL.Host
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

func (m *URL) compileRegex() error {
	if m.PathReplace == "" {
		return nil
	}

	var err error

	m.pathRegex, err = regexp.Compile(m.PathReplace)
	if err != nil {
		return fmt.Errorf("failed to compile path regex: %w", err)
	}

	return nil
}

func (m *URL) modifyScheme(u *neturl.URL) {
	if m.Scheme != "" {
		u.Scheme = m.Scheme
	}
}

func (m *URL) modifyHost(u *neturl.URL) {
	if m.Host != "" {
		u.Host = m.Host
	}

	if m.Port != "" && u.Host != "" {
		host := m.removeExistingPort(u.Host)

		u.Host = host + ":" + m.Port
	}
}

func (m *URL) removeExistingPort(host string) string {
	if colonIndex := strings.LastIndex(host, ":"); colonIndex != -1 && strings.Contains(host[colonIndex:], ":") {
		return host[:colonIndex]
	}

	return host
}

func (m *URL) modifyPath(u *neturl.URL) {
	if m.Path != "" {
		u.Path = m.Path

		return
	}

	path := u.Path
	path = m.applyStripPrefix(path)
	path = m.applyPathPrefix(path)
	path = m.applyPathSuffix(path)
	path = m.applyRegexReplace(path)

	u.Path = path
}

func (m *URL) applyStripPrefix(path string) string {
	if m.StripPrefix != "" && strings.HasPrefix(path, m.StripPrefix) {
		path = strings.TrimPrefix(path, m.StripPrefix)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	}

	return path
}

func (m *URL) applyPathPrefix(path string) string {
	if m.PathPrefix == "" {
		return path
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	prefix := m.PathPrefix
	if !strings.HasSuffix(prefix, "/") && !strings.HasPrefix(path, "/") {
		prefix += "/"
	}

	return prefix + strings.TrimPrefix(path, "/")
}

func (m *URL) applyPathSuffix(path string) string {
	if m.PathSuffix != "" {
		path += m.PathSuffix
	}

	return path
}

func (m *URL) applyRegexReplace(path string) string {
	if m.pathRegex != nil && m.PathReplaceWith != "" {
		return m.pathRegex.ReplaceAllString(path, m.PathReplaceWith)
	}

	return path
}

func (m *URL) modifyQuery(u *neturl.URL) {
	if len(m.AddQuery) == 0 && len(m.RemoveQuery) == 0 && len(m.SetQuery) == 0 {
		return
	}

	query := u.Query()

	for _, key := range m.RemoveQuery {
		query.Del(key)
	}

	for key, value := range m.AddQuery {
		query.Add(key, value)
	}

	for key, value := range m.SetQuery {
		query.Set(key, value)
	}

	u.RawQuery = query.Encode()
}

func (m *URL) modifyFragment(u *neturl.URL) {
	if m.Fragment != "" {
		u.Fragment = m.Fragment
	}
}
