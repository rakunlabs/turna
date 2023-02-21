package middlewares

import (
	"fmt"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Service struct {
	LoadBalancer LoadBalancer `cfg:"loadbalancer"`
}

type LoadBalancer struct {
	Servers []Server `cfg:"servers"`
}

type Server struct {
	URL string `cfg:"url"`
}

func (m *Service) Middleware() ([]echo.MiddlewareFunc, error) {
	targets := make([]*middleware.ProxyTarget, 0, len(m.LoadBalancer.Servers))

	for _, server := range m.LoadBalancer.Servers {
		u, err := url.Parse(server.URL)
		if err != nil {
			return nil, fmt.Errorf("cannot parse url %s: %w", server.URL, err)
		}

		targets = append(targets, &middleware.ProxyTarget{
			URL: u,
		})
	}

	return []echo.MiddlewareFunc{middleware.Proxy(middleware.NewRoundRobinBalancer(targets))}, nil
}
