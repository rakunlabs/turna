package http

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type RouterHandler interface {
	Handle(pattern string, handler http.Handler)
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

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
	found := s.ruleMux[RuleSelection{Entrypoint: s.entrypoint}]

	if v := s.ruleMux[RuleSelection{Host: hostSanitizer(r.Host), Entrypoint: s.entrypoint}]; v != nil {
		found = v
	}

	if found == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 not found - turna"))

		return
	}

	found.ServeHTTP(w, r)
}

func (s *RuleRouter) SetRule(selection RuleSelection) {
	if _, ok := s.ruleMux[selection]; !ok {
		s.ruleMux[selection] = chi.NewRouter()
	}
}

func (s *RuleRouter) GetMux(r RuleSelection) RouterHandler {
	return s.ruleMux[r]
}

func hostSanitizer(host string) string {
	return strings.SplitN(host, ":", 2)[0]
}
