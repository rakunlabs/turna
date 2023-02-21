package render

import (
	"fmt"

	"github.com/rytsh/liz/utils/fstore"
	"github.com/rytsh/liz/utils/templatex"
	"github.com/rytsh/liz/utils/templatex/store"
	"github.com/spf13/cast"
)

var Template = templatex.New(store.WithAddFuncsTpl(fstore.FuncMapTpl()))

var GlobalRender = Render{
	template: Template,
}

type Render struct {
	Data     map[string]interface{}
	template *templatex.Template
}

func New() Render {
	return Render{
		template: Template,
	}
}

func (r *Render) IsTemplateExist() bool {
	return r.template != nil
}

func (r *Render) Execute(content any) (string, error) {
	return r.ExecuteWithData(content, r.Data)
}

func (r *Render) ExecuteWithData(content any, data any) (string, error) {
	if r.template == nil {
		return "", fmt.Errorf("template is nil")
	}

	contentStr := cast.ToString(content)

	if err := r.template.Parse(contentStr); err != nil {
		return "", err
	}

	output, err := r.template.ExecuteBuffer(
		templatex.WithData(data),
		templatex.WithParsed(true),
	)
	if err != nil {
		return "", err
	}

	return string(output), err
}
