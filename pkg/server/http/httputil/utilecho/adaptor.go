package utilecho

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/worldline-go/turna/pkg/server/http/tcontext"
)

func AdaptEchoMiddleware(mw echo.MiddlewareFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// Convert http.Handler to echo.HandlerFunc
		handler := func(c echo.Context) error {
			next.ServeHTTP(c.Response(), c.Request())

			return nil
		}

		// Use echo's MiddlewareFunc
		echoHandler := mw(handler)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create an echo.Context from http.Request
			c := tcontext.GetEchoContext(r, w)

			// Execute echo's handler
			if err := echoHandler(c); err != nil {
				c.Error(err)
			}
		})
	}
}

func AdaptEchoMiddlewares(mws []echo.MiddlewareFunc) []func(http.Handler) http.Handler {
	adapted := make([]func(http.Handler) http.Handler, 0, len(mws))
	for _, mw := range mws {
		adapted = append(adapted, AdaptEchoMiddleware(mw))
	}

	return adapted
}
