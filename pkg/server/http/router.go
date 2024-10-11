package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/labstack/echo/v4"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
	"github.com/rakunlabs/turna/pkg/server/registry"
)

var ServerInfo = "turna"

type Router struct {
	Host        string    `cfg:"host"`
	Path        []string  `cfg:"path"`
	Middlewares []string  `cfg:"middlewares"`
	TLS         *struct{} `cfg:"tls"`
	EntryPoints []string  `cfg:"entrypoints"`
}

func (r *Router) Set(_ string, ruleRouter *RuleRouter) error {
	entrypoints := r.EntryPoints
	if len(entrypoints) == 0 {
		entrypoints = registry.GlobalReg.GetListenerNamesList()
	}

	for _, entrypoint := range entrypoints {
		e := ruleRouter.GetMux(RuleSelection{
			Host:       r.Host,
			Entrypoint: entrypoint,
		})

		if e == nil {
			return fmt.Errorf("entrypoint %s, host %s, does not exist", entrypoint, r.Host)
		}

		middlewares := make([]func(http.Handler) http.Handler, 0, len(r.Middlewares)+4)
		middlewares = append(middlewares, RecoverMiddleware, RequestIDMiddleware, PreMiddleware)

		for _, middlewareName := range r.Middlewares {
			middlewareFromGlobal, err := registry.GlobalReg.GetHttpMiddleware(middlewareName)
			if err != nil {
				return err
			}

			middlewares = append(middlewares, middlewareFromGlobal...)
		}

		middlewares = append(middlewares, PostMiddleware)

		mPath := make(map[string]struct{}, len(r.Path))
		for _, path := range r.Path {
			mPath[path] = struct{}{}
		}

		for path := range mPath {
			e.Handle(path, httputil.NewMiddlewareHandler(middlewares))
		}
	}

	return nil
}

var ErrorMiddleware = func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), "error-handler", func(err error) {
			slog.Error(err.Error(), "request_id", r.Header.Get("X-Request-Id"))

			httputil.HandleError(w, httputil.NewError("", err, http.StatusInternalServerError))
		})

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

var RecoverMiddleware = func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if r := recover(); r != nil {
				if r == http.ErrAbortHandler {
					panic(r)
				}
				err, ok := r.(error)
				if !ok {
					err = fmt.Errorf("%v", r)
				}

				slog.Error(fmt.Sprintf("panic: %s", err.Error()))
				debug.PrintStack()

				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(fmt.Sprintf("panic: %s", err.Error())))

				return
			}
		}()

		next.ServeHTTP(w, r)
	})
}

var RequestIDMiddleware = func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = ulid.Make().String()
		}

		r.Header.Set("X-Request-Id", requestID)
		w.Header().Set("X-Request-Id", requestID)

		next.ServeHTTP(w, r)
	})
}

var PostMiddleware = func(_ http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		_, _ = w.Write(nil)
	})
}

var PreMiddleware = func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", ServerInfo)

		// set turna value
		turna := &tcontext.Turna{
			Vars: make(map[string]interface{}),
		}

		ctx := context.WithValue(r.Context(), tcontext.TurnaKey, turna)
		r = r.WithContext(ctx)

		echoContext := echo.New().NewContext(r, w)
		echoContext.Set("turna", turna)
		turna.EchoContext = echoContext

		next.ServeHTTP(w, r)
	})
}
