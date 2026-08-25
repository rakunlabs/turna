package service

import (
	"os"
	"strings"

	"github.com/rakunlabs/turna/internal/loader"
	"github.com/rakunlabs/turna/pkg/render"
)

// baselineEnv is the minimal set of environment variables that are always
// inherited from the parent process, even when inherit_env is false.
// Keys are matched case-insensitively to also cover Windows (e.g. "Path").
var baselineEnv = map[string]struct{}{
	// unix
	"PATH":    {},
	"HOME":    {},
	"USER":    {},
	"LOGNAME": {},
	"SHELL":   {},
	"TMPDIR":  {},
	"LANG":    {},
	"TZ":      {},
	// windows
	"SYSTEMROOT":  {},
	"SYSTEMDRIVE": {},
	"TEMP":        {},
	"TMP":         {},
	"PATHEXT":     {},
	"USERPROFILE": {},
	"COMSPEC":     {},
}

func (s *Service) GetEnv(predefined map[string]any, environ bool, envPaths []string) ([]string, error) {
	v := make(map[string]string)
	for _, e := range os.Environ() {
		pair := strings.SplitN(e, "=", 2)
		if !environ {
			// inherit only the baseline set (PATH, HOME, TMPDIR, ...)
			if _, ok := baselineEnv[strings.ToUpper(pair[0])]; !ok {
				continue
			}
		}

		v[pair[0]] = pair[1]
	}

	// add values
	for _, path := range envPaths {
		if vInner, ok := loader.InnerPath(path, render.Data).(map[string]any); ok {
			for k, val := range vInner {
				rV, err := render.Execute(val)
				if err != nil {
					return nil, err
				}
				v[k] = string(rV)
			}
		}
	}

	for k, val := range predefined {
		rV, err := render.Execute(val)
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
