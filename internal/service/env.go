package service

import (
	"fmt"
	"os"
	"strings"

	"github.com/worldline-go/turna/pkg/template"
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
	rV, err := template.Ext(nil, fmt.Sprint(v))
	if err != nil {
		return "", err
	}

	return string(rV), err
}
