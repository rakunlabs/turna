package url

import (
	"fmt"
	"log/slog"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type URL struct {
	Rules []RuleCheck `cfg:"rules"`
}

type RuleCheck struct {
	AlwaysMatch bool     `cfg:"always_match"`
	HostMatch   []string `cfg:"host_match"`
	PathMatch   []string `cfg:"path_match"`

	Modify Rule `cfg:"modify"`
}

type Rule struct {
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

type SchemeModify struct {
	Scheme     string `cfg:"scheme"`
	HostSuffix string `cfg:"host_suffix"`
}

func (m *URL) Middleware() (func(http.Handler) http.Handler, error) {
	for i := range m.Rules {
		if err := m.Rules[i].Modify.compileRegex(); err != nil {
			return nil, err
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rule := m.findMatchingRule(r)
			if rule == nil {
				next.ServeHTTP(w, r)

				return
			}

			modifiedURL := *r.URL

			rule.modifyScheme(&modifiedURL)
			rule.modifyHost(&modifiedURL)
			rule.modifyPath(&modifiedURL)
			rule.modifyQuery(&modifiedURL)
			rule.modifyFragment(&modifiedURL)

			r.URL = &modifiedURL

			if rule.Host != "" {
				r.Host = modifiedURL.Host
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

func (m *URL) findMatchingRule(r *http.Request) *Rule {
	for i := range m.Rules {
		rule := &m.Rules[i]

		if rule.matches(r) {
			return &rule.Modify
		}
	}

	return nil
}

func (m *RuleCheck) matches(r *http.Request) bool {
	if m.AlwaysMatch {
		return true
	}

	matched := false

	if len(m.HostMatch) > 0 {
		matched = m.hostMatches(r)
	}

	if len(m.PathMatch) > 0 {
		matched = m.pathMatches(r)
	}

	return matched
}

func (m *RuleCheck) hostMatches(r *http.Request) bool {
	for _, pattern := range m.HostMatch {
		ok, err := doublestar.Match(pattern, r.Host)
		if err != nil {
			slog.Info("error matching host pattern", "pattern", pattern, "host", r.Host, "error", err)

			continue
		}

		if ok {
			return true
		}
	}

	return false
}

func (m *RuleCheck) pathMatches(r *http.Request) bool {
	for _, pattern := range m.PathMatch {
		ok, err := doublestar.Match(pattern, r.URL.Path)
		if err != nil {
			slog.Info("error matching path pattern", "pattern", pattern, "path", r.URL.Path, "error", err)

			continue
		}

		if ok {
			return true
		}
	}

	return false
}

func (m *Rule) compileRegex() error {
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

func (m *Rule) modifyScheme(u *neturl.URL) {
	if m.Scheme != "" {
		u.Scheme = m.Scheme
	}
}

func (m *Rule) modifyHost(u *neturl.URL) {
	if m.Host != "" {
		u.Host = m.Host
	}

	if m.Port != "" && u.Host != "" {
		host := m.removeExistingPort(u.Host)

		u.Host = host + ":" + m.Port
	}
}

func (m *Rule) removeExistingPort(host string) string {
	if colonIndex := strings.LastIndex(host, ":"); colonIndex != -1 && strings.Contains(host[colonIndex:], ":") {
		return host[:colonIndex]
	}

	return host
}

func (m *Rule) modifyPath(u *neturl.URL) {
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

func (m *Rule) applyStripPrefix(path string) string {
	if m.StripPrefix != "" && strings.HasPrefix(path, m.StripPrefix) {
		path = strings.TrimPrefix(path, m.StripPrefix)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	}

	return path
}

func (m *Rule) applyPathPrefix(path string) string {
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

func (m *Rule) applyPathSuffix(path string) string {
	if m.PathSuffix != "" {
		path += m.PathSuffix
	}

	return path
}

func (m *Rule) applyRegexReplace(path string) string {
	if m.pathRegex != nil && m.PathReplaceWith != "" {
		return m.pathRegex.ReplaceAllString(path, m.PathReplaceWith)
	}

	return path
}

func (m *Rule) modifyQuery(u *neturl.URL) {
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

func (m *Rule) modifyFragment(u *neturl.URL) {
	if m.Fragment != "" {
		u.Fragment = m.Fragment
	}
}
