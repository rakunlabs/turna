package render

import (
	"sync"

	"github.com/rytsh/mugo/render"
	"github.com/rytsh/mugo/templatex"
)

// Pool hands each execution its own template.
//
// A mugo template stores the last parsed content in itself on every Execute
// without locking, so one template must not be executed concurrently.
type Pool struct {
	pool sync.Pool
}

func NewPool(newTemplate func() *templatex.Template) *Pool {
	return &Pool{
		pool: sync.Pool{
			New: func() any { return newTemplate() },
		},
	}
}

func (p *Pool) Execute(opts ...templatex.OptionExecute) error {
	tpl := p.pool.Get().(*templatex.Template)
	defer p.pool.Put(tpl)

	return tpl.Execute(opts...)
}

var renderPool = sync.Pool{
	New: func() any { return render.NewRender() },
}

// ExecuteWithData renders content with data, safe for concurrent use.
func ExecuteWithData(content string, data any, opts ...templatex.OptionExecute) ([]byte, error) {
	r := renderPool.Get().(*render.Render)
	defer renderPool.Put(r)

	return r.ExecuteWithData(content, data, opts...)
}
