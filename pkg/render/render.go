package render

import (
	"bytes"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/rytsh/mugo/fstore"
	"github.com/rytsh/mugo/templatex"
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

func (r *Render) Execute(content any) ([]byte, error) {
	return r.ExecuteWithData(content, r.Data)
}

func (r *Render) ExecuteWithData(content any, data any) ([]byte, error) {
	if r.template == nil {
		return nil, fmt.Errorf("template is nil")
	}

	contentStr := cast.ToString(content)

	if err := r.template.Parse(contentStr); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err := r.template.Execute(
		templatex.WithIO(&buf),
		templatex.WithData(data),
		templatex.WithParsed(true),
	)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), err
}
