package service

import (
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type commonBalancer struct {
	targets []*middleware.ProxyTarget
	mutex   sync.Mutex
}

func (b *commonBalancer) AddTarget(target *middleware.ProxyTarget) bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	for _, t := range b.targets {
		if t.Name == target.Name {
			return false
		}
	}
	b.targets = append(b.targets, target)

	return true
}

func (b *commonBalancer) RemoveTarget(name string) bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	for i, t := range b.targets {
		if t.Name == name {
			b.targets = append(b.targets[:i], b.targets[i+1:]...)

			return true
		}
	}

	return false
}

type PrefixBalancer struct {
	commonBalancer

	Prefixes       []PrefixServers `cfg:"prefixes"`
	DefaultServers []Server        `cfg:"default_servers"`

	defaultBalancer middleware.ProxyBalancer
}

type PrefixServers struct {
	Prefix  string   `cfg:"prefix"`
	Servers []Server `cfg:"servers"`

	balancer middleware.ProxyBalancer
}

func (b *PrefixBalancer) IsEnabled() bool {
	return len(b.Prefixes) > 0 || len(b.DefaultServers) > 0
}

func (b *PrefixBalancer) Next(c echo.Context) *middleware.ProxyTarget {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	path := c.Request().URL.Path

	for _, prefix := range b.Prefixes {
		if strings.HasPrefix(path, prefix.Prefix) {
			return prefix.balancer.Next(c)
		}
	}

	return b.defaultBalancer.Next(c)
}
