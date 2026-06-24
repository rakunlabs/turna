package render

import (
	"log/slog"
	"sync"

	"github.com/rytsh/mugo/fstore"
	_ "github.com/rytsh/mugo/fstore/registry"
	"github.com/spf13/cast"

	"github.com/rytsh/mugo/render"
	"github.com/rytsh/mugo/templatex"
)

var (
	ExecuteWithData = render.ExecuteWithData
)

var Data = make(map[string]any)

func Execute(content any) ([]byte, error) {
	return ExecuteWithData(cast.ToString(content), Data)
}

// validateTemplate carries the mugo function map so Validate can parse template
// content (mugo funcs included) without executing it.
var (
	validateTemplate = templatex.New(
		templatex.WithAddFuncMapWithOpts(func(o templatex.Option) map[string]any {
			return fstore.FuncMap(
				fstore.WithLog(slog.Default()),
				fstore.WithTrust(true),
				fstore.WithExecuteTemplate(o.T),
			)
		}),
	)
	validateMu sync.Mutex
)

// Validate parses template content and returns a syntax error if any. It does
// NOT execute the template, so the result is independent of the runtime data:
// which claim/user fields exist depends on the deployment (LDAP mapping, edited
// user data), and executing against fixed sample data would be misleading.
func Validate(content string) error {
	validateMu.Lock()
	defer validateMu.Unlock()

	return validateTemplate.Parse(content)
}
