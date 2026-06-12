package auth

import (
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

// checkAccess reports whether the permission allows host/path/method with the given check config.
func checkAccess(cfg data.CheckConfig, perm *data.Permission, host, pathRequest, method string) bool {
	for _, req := range perm.Resources {
		if !cfg.NoHostCheck {
			hosts := req.Hosts
			if len(hosts) == 0 && len(cfg.DefaultHosts) > 0 {
				hosts = cfg.DefaultHosts
			}

			if !checkHost(hosts, host) {
				continue
			}
		}

		if !checkMethod(req.Methods, method) {
			continue
		}

		if checkExcluded(req.Excluded, host, pathRequest, method) {
			continue
		}

		if checkPaths(req.Paths, pathRequest) {
			return true
		}

		if checkPath(req.Path, pathRequest) {
			return true
		}
	}

	return false
}

func checkExcluded(resources []data.Resource, host, pathRequest, method string) bool {
	for _, req := range resources {
		if len(req.Hosts) > 0 && !checkHost(req.Hosts, host) {
			continue
		}

		if !checkMethod(req.Methods, method) {
			continue
		}

		if checkPaths(req.Paths, pathRequest) {
			return true
		}

		if checkPath(req.Path, pathRequest) {
			return true
		}
	}

	return false
}

func checkHost(hosts []string, host string) bool {
	for _, pattern := range hosts {
		if v, _ := doublestar.Match(pattern, host); v {
			return true
		}
	}

	return false
}

func checkMethod(methods []string, method string) bool {
	return slices.ContainsFunc(methods, func(v string) bool {
		if v == "*" {
			return true
		}

		return strings.EqualFold(v, method)
	})
}

func checkPath(pattern, pathRequest string) bool {
	if pattern == "" {
		return false
	}

	v, _ := doublestar.Match(pattern, pathRequest)

	return v
}

func checkPaths(patterns []string, pathRequest string) bool {
	for _, pattern := range patterns {
		if v, _ := doublestar.Match(pattern, pathRequest); v {
			return true
		}
	}

	return false
}

func permissionMatchesRequest(perm *data.Permission, method, path string) bool {
	if method != "" {
		found := false
		for _, res := range perm.Resources {
			if checkMethod(res.Methods, method) {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	if path != "" {
		found := false
		for _, res := range perm.Resources {
			if checkPath(res.Path, path) || checkPaths(res.Paths, path) {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}

func containsFold(value string, search string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(search))
}

func matchAnyNameFold(value string, names []string) bool {
	for _, name := range names {
		if containsFold(value, name) {
			return true
		}
	}

	return false
}
