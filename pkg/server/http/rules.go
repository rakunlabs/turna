package http

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog/log"
	"github.com/worldline-go/logz/logecho"
	"github.com/ziflex/lecho/v3"
)

type RuleRouter struct {
	ruleEcho map[RuleSelection]*echo.Echo

	entrypoint string
}

type RuleSelection struct {
	Host       string
	Entrypoint string
}

func NewRuleRouter() *RuleRouter {
	return &RuleRouter{
		ruleEcho: make(map[RuleSelection]*echo.Echo),
	}
}

// Serve implements the http.Handler interface with changing entrypoint selection.
func (s RuleRouter) Serve(entrypoint string) http.Handler {
	s.entrypoint = entrypoint
	return &s
}

func (s *RuleRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	found := s.ruleEcho[RuleSelection{Entrypoint: s.entrypoint}]

	if v := s.ruleEcho[RuleSelection{Host: hostSanitizer(r.Host), Entrypoint: s.entrypoint}]; v != nil {
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
	if _, ok := s.ruleEcho[selection]; !ok {
		s.ruleEcho[selection] = EchoNew()
	}
}

func (s *RuleRouter) GetEcho(r RuleSelection) *echo.Echo {
	return s.ruleEcho[r]
}

func hostSanitizer(host string) string {
	return strings.SplitN(host, ":", 2)[0]
}

func EchoNew() *echo.Echo {
	e := echo.New()

	e.HideBanner = true
	e.Logger = lecho.New(log.With().Str("component", "server").Logger())

	recoverConfig := middleware.DefaultRecoverConfig
	recoverConfig.LogErrorFunc = func(c echo.Context, err error, stack []byte) error {
		log.Error().Err(err).Msgf("panic: %s", stack)

		return err
	}

	// default middlewares
	e.Use(
		middleware.RecoverWithConfig(recoverConfig),
	)

	// log middlewares
	e.Use(
		middleware.RequestID(),
		middleware.RequestLoggerWithConfig(logecho.RequestLoggerConfig()),
		logecho.ZerologLogger(),
	)

	return e
}
