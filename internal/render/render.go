package render

import (
	"fmt"

	"github.com/rytsh/liz/utils/templatex"
)

var Template = templatex.New()

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
	if r.template == nil {
		return "", fmt.Errorf("template is nil")
	}

	return r.template.Execute(r.Data, fmt.Sprint(content))
}
