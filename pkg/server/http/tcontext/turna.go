package tcontext

import (
	"sync"

	"github.com/labstack/echo/v4"
)

type contextKey string

const (
	TurnaKey contextKey = "turna"
)

type Turna struct {
	EchoContext echo.Context
	Vars        map[string]interface{}

	m sync.Mutex
}

func (t *Turna) Set(key string, value interface{}) {
	t.m.Lock()
	defer t.m.Unlock()

	t.Vars[key] = value
}

func (t *Turna) Get(key string) (interface{}, bool) {
	t.m.Lock()
	defer t.m.Unlock()

	value, ok := t.Vars[key]

	return value, ok
}
