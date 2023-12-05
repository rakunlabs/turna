package middlewares

import (
	"fmt"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/worldline-go/klient"
)

type Service struct {
	InsecureSkipVerify bool         `cfg:"insecure_skip_verify"`
	PassHostHeader     bool         `cfg:"pass_host_header"`
	LoadBalancer       LoadBalancer `cfg:"loadbalancer"`
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

	cfg := middleware.DefaultProxyConfig
	cfg.Balancer = middleware.NewRoundRobinBalancer(targets)

	client, err := klient.New(
		klient.WithDisableBaseURLCheck(true),
		klient.WithDisableRetry(true),
		klient.WithDisableEnvValues(true),
		klient.WithInsecureSkipVerify(m.InsecureSkipVerify),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create klient: %w", err)
	}

	cfg.Transport = client.HTTP.Transport

	checkHost := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !m.PassHostHeader {
				c.Request().Host = ""
			}

			return next(c)
		}
	}

	return []echo.MiddlewareFunc{checkHost, middleware.ProxyWithConfig(cfg)}, nil
}
