package http

import (
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/rakunlabs/ada"
)

type RouterHandler interface {
	Handle(pattern string, handler http.Handler, middlewares ...func(http.Handler) http.Handler)
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// RuleRouter is a routing table keyed by host and entrypoint. It is built once
// and then treated as immutable; dynamic updates swap a whole new RuleRouter
// through a RuleSwitch.
type RuleRouter struct {
	ruleMux map[RuleSelection]RouterHandler

	entrypoint string
}

type RuleSelection struct {
	Host       string
	Entrypoint string
}

func NewRuleRouter() *RuleRouter {
	return &RuleRouter{
		ruleMux: make(map[RuleSelection]RouterHandler),
	}
}

// Serve implements the http.Handler interface with changing entrypoint selection.
func (s RuleRouter) Serve(entrypoint string) http.Handler {
	s.entrypoint = entrypoint
	return &s
}

func (s *RuleRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.serve(s.entrypoint, w, r)
}

func (s *RuleRouter) serve(entrypoint string, w http.ResponseWriter, r *http.Request) {
	found := s.match(hostSanitizer(r.Host), entrypoint)
	if found == nil {
		notFound(w)

		return
	}

	found.ServeHTTP(w, r)
}

// match selects the handler for host in order: exact host, the longest
// wildcard host ("*.example.com" matches "api.example.com" and
// "a.b.example.com"), then the host-less rule.
func (s *RuleRouter) match(host, entrypoint string) RouterHandler {
	if host != "" {
		if v := s.ruleMux[RuleSelection{Host: host, Entrypoint: entrypoint}]; v != nil {
			return v
		}

		for i := strings.IndexByte(host, '.'); i > 0; {
			if v := s.ruleMux[RuleSelection{Host: "*" + host[i:], Entrypoint: entrypoint}]; v != nil {
				return v
			}

			next := strings.IndexByte(host[i+1:], '.')
			if next < 0 {
				break
			}

			i += next + 1
		}
	}

	return s.ruleMux[RuleSelection{Entrypoint: entrypoint}]
}

func (s *RuleRouter) SetRule(selection RuleSelection) {
	if _, ok := s.ruleMux[selection]; !ok {
		s.ruleMux[selection] = ada.NewMux()
	}
}

func (s *RuleRouter) GetMux(r RuleSelection) RouterHandler {
	return s.ruleMux[r]
}

// RuleSwitch serves requests from the currently stored RuleRouter, allowing
// the routing table to be replaced atomically while servers keep running.
type RuleSwitch struct {
	current atomic.Pointer[RuleRouter]
}

func NewRuleSwitch() *RuleSwitch {
	s := &RuleSwitch{}
	s.current.Store(NewRuleRouter())

	return s
}

func (s *RuleSwitch) Store(r *RuleRouter) {
	s.current.Store(r)
}

func (s *RuleSwitch) Load() *RuleRouter {
	return s.current.Load()
}

// Serve returns an http.Handler for the entrypoint that always routes with the
// latest stored RuleRouter.
func (s *RuleSwitch) Serve(entrypoint string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.current.Load().serve(entrypoint, w, r)
	})
}

func notFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("404 not found - turna"))
}

func hostSanitizer(host string) string {
	return strings.SplitN(host, ":", 2)[0]
}
