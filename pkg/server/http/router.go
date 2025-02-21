package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/requestid"
	"github.com/worldline-go/turna/pkg/server/http/tcontext"
	"github.com/worldline-go/turna/pkg/server/registry"
)

var ServerInfo = "turna"

type Router struct {
	Host        string    `cfg:"host"`
	Path        []string  `cfg:"path"`
	Middlewares []string  `cfg:"middlewares"`
	TLS         *struct{} `cfg:"tls"`
	EntryPoints []string  `cfg:"entrypoints"`

	PreMiddlewares PreMiddlewares `cfg:"pre_middlewares"`
}

type PreMiddlewares struct {
	RequestID  *bool `cfg:"request_id"`  // default is true
	ServerInfo *bool `cfg:"server_info"` // default is true
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
		middlewares = append(middlewares, RecoverMiddleware, PreMiddleware)

		if r.PreMiddlewares.RequestID == nil || *r.PreMiddlewares.RequestID {
			middlewares = append(middlewares, requestid.RequestID{}.Middleware())
		}
		if r.PreMiddlewares.ServerInfo == nil || *r.PreMiddlewares.ServerInfo {
			middlewares = append(middlewares, ServerInfoMiddleware)
		}

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

var PostMiddleware = func(_ http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		_, _ = w.Write(nil)
	})
}

var ServerInfoMiddleware = func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", ServerInfo)

		next.ServeHTTP(w, r)
	})
}

var PreMiddleware = func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, r = tcontext.New(w, r)

		next.ServeHTTP(w, r)
	})
}
