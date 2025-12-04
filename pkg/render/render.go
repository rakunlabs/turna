package render

import (
	_ "github.com/rytsh/mugo/fstore/registry"
	"github.com/spf13/cast"

	"github.com/rytsh/mugo/render"
)

var (
	ExecuteWithData = render.ExecuteWithData
)

var Data = make(map[string]any)

func Execute(content any) ([]byte, error) {
	return ExecuteWithData(cast.ToString(content), Data)
}
