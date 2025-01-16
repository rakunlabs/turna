package splitter

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/registry"
)

type Splitter struct {
	Rules []Rule `cfg:"rules"`
}

type Rule struct {
	Rule        string   `cfg:"rule"`
	Middlewares []string `cfg:"middlewares"`

	rule    *vm.Program
	handler http.Handler

	once sync.Once
}

func (m *Splitter) funcs(r *http.Request) map[string]interface{} {
	req := &Requester{Req: r}

	return map[string]interface{}{
		"Header": req.Header,
		"Path":   req.Path,
		"Method": req.Method,
		"Host":   req.Host,
		"Query":  req.Query,
	}
}

func (m *Splitter) Middleware() (func(http.Handler) http.Handler, error) {
	for i := range m.Rules {
		program, err := expr.Compile(m.Rules[i].Rule)
		if err != nil {
			return nil, fmt.Errorf("failed to compile rule [%s]: %w", m.Rules[i].Rule, err)
		}

		m.Rules[i].rule = program
	}

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var next http.Handler

			for i := range m.Rules {
				output, err := expr.Run(m.Rules[i].rule, m.funcs(r))
				if err != nil {
					httputil.HandleError(w, httputil.NewError("failed to run expr", err, http.StatusInternalServerError))

					return
				}

				if v, _ := output.(bool); !v {
					continue
				}

				m.Rules[i].once.Do(func() {
					middlewares := make([]func(http.Handler) http.Handler, 0, len(m.Rules[i].Middlewares)+1)
					for _, middlewareName := range m.Rules[i].Middlewares {
						middlewareFromGlobal, err := registry.GlobalReg.GetHttpMiddleware(middlewareName)
						if err != nil {
							slog.Error("middleware not found", slog.String("middleware", middlewareName), slog.String("error", err.Error()))

							continue
						}

						middlewares = append(middlewares, middlewareFromGlobal...)
					}

					middlewares = append(middlewares,
						func(_ http.Handler) http.Handler {
							return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
								httputil.HandleError(w, httputil.NewError("", nil, http.StatusNotFound))
							})
						},
					)

					m.Rules[i].handler = httputil.NewMiddlewareHandler(middlewares)
				})

				next = m.Rules[i].handler

				break
			}

			if next == nil {
				httputil.HandleError(w, httputil.NewError("", nil, http.StatusNotFound))

				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}
