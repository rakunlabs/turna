package tcontext

import (
	"net/http"
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

func GetTurna(r *http.Request) (*Turna, bool) {
	turna, ok := r.Context().Value(TurnaKey).(*Turna)

	return turna, ok
}

func Set(r *http.Request, key string, value any) {
	turna, ok := GetTurna(r)
	if !ok {
		return
	}

	turna.Set(key, value)
}

func Get(r *http.Request, key string) interface{} {
	turna, ok := GetTurna(r)
	if !ok {
		return nil
	}

	v, _ := turna.Get(key)

	return v
}

func GetEchoContext(r *http.Request, w http.ResponseWriter) echo.Context {
	var c echo.Context
	turna, _ := r.Context().Value(TurnaKey).(*Turna)
	if turna == nil {
		c = echo.New().NewContext(r, w)
	} else {
		c = turna.EchoContext
	}

	return c
}
