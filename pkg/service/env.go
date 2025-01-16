package service

import (
	"os"
	"strings"

	"github.com/worldline-go/turna/pkg/render"
	"github.com/rytsh/liz/loader"
)

func (s *Service) GetEnv(predefined map[string]interface{}, environ bool, envPaths []string) ([]string, error) {
	v := make(map[string]string)
	if environ {
		for _, e := range os.Environ() {
			pair := strings.SplitN(e, "=", 2)
			v[pair[0]] = pair[1]
		}
	}

	// add values
	for _, path := range envPaths {
		if vInner, ok := loader.InnerPath(path, render.GlobalRender.Data).(map[string]interface{}); ok {
			for k, val := range vInner {
				rV, err := render.GlobalRender.Execute(val)
				if err != nil {
					return nil, err
				}
				v[k] = string(rV)
			}
		}
	}

	for k, val := range predefined {
		rV, err := render.GlobalRender.Execute(val)
		if err != nil {
			return nil, err
		}
		v[k] = string(rV)
	}

	env := []string{}
	for k, val := range v {
		env = append(env, k+"="+val)
	}

	return env, nil
}
