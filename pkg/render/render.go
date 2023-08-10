package render

import (
	"bytes"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/rytsh/mugo/pkg/fstore"
	"github.com/rytsh/mugo/pkg/templatex"
	"github.com/spf13/cast"
	"github.com/worldline-go/logz"
)

var Template = templatex.New(templatex.WithAddFuncsTpl(fstore.FuncMapTpl(
	fstore.WithLog(logz.AdapterKV{Log: log.Logger}),
	fstore.WithTrust(true),
)))

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

	var buf bytes.Buffer
	err := r.template.Execute(
		templatex.WithIO(&buf),
		templatex.WithData(data),
		templatex.WithParsed(true),
	)
	if err != nil {
		return "", err
	}

	return buf.String(), err
}
