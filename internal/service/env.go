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
	Data     map[string]interface{}
)

func GetEnv(predefined map[string]interface{}, environ bool, envPaths []string) ([]string, error) {
	v := make(map[string]string)
	if environ {
		for _, e := range os.Environ() {
			pair := strings.SplitN(e, "=", 2)
			v[pair[0]] = pair[1]
		}
	}

	// add values
	for _, path := range envPaths {
		if vInner, ok := loader.InnerPath(path, Data).(map[string]interface{}); ok {
			for k, val := range vInner {
				rV, err := RenderValue(val)
				if err != nil {
					return nil, err
				}
				v[k] = rV
			}
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
	return template.Execute(Data, fmt.Sprint(v))
}
