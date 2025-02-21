package tcontext

import (
	"context"
	"net/http"
	"sync"
)

type contextKey string

const (
	TurnaKey contextKey = "turna"
)

type Turna struct {
	Vars map[string]interface{}

	m sync.Mutex
}

func New(w http.ResponseWriter, r *http.Request) (*Turna, *http.Request) {
	// set turna value
	turna := &Turna{
		Vars: make(map[string]interface{}),
	}

	ctx := context.WithValue(r.Context(), TurnaKey, turna)
	r = r.WithContext(ctx)

	return turna, r
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

func (t *Turna) GetInterface(key string) interface{} {
	value, _ := t.Get(key)

	return value
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
