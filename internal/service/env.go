package service

import (
	"fmt"
	"os"
	"strings"

	"github.com/rytsh/liz/loader"
	"github.com/rytsh/liz/utils/templatex"
)

var (
	template = templatex.New()
	Data     = loader.Data{}
)

func GetEnv(predefined map[string]interface{}, environ bool) ([]string, error) {
	v := make(map[string]string)
	if environ {
		for _, e := range os.Environ() {
			pair := strings.SplitN(e, "=", 2)
			v[pair[0]] = pair[1]
		}
	}

	for k, val := range predefined {
		rV, err := RenderValue(val)
		if err != nil {
			return nil, err
		}
		v[k] = rV
	}

	env := []string{}
	for k, val := range v {
		env = append(env, k+"="+val)
	}

	return env, nil
}

func RenderValue(v interface{}) (string, error) {
	var pass interface{}
	if Data.Raw != nil {
		pass = Data.Raw
	} else {
		pass = Data.Map
	}

	return template.Execute(pass, fmt.Sprint(v))
}
