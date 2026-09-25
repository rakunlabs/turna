package auth

import (
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

// normalizeResources folds the deprecated Resource.Path field into Resource.Paths.
//
// Path is still accepted on the wire for backwards compatibility, but it is
// never persisted or used for matching. Read responses derive a display-only
// Path from Paths. Excluded is normalized recursively.
func normalizeResources(resources []data.Resource) []data.Resource {
	for i := range resources {
		resources[i] = normalizeResource(resources[i])
	}

	return resources
}

func normalizeResource(resource data.Resource) data.Resource {
	if resource.Path != "" {
		if resource.Path != strings.Join(resource.Paths, " ") {
			// Path is the legacy singular representation. If a legacy client
			// changes it, treat that as a replacement rather than granting both
			// the old and new paths. An unchanged display Path keeps Paths intact.
			resource.Paths = []string{resource.Path}
		}

		resource.Path = ""
	}

	resource.Excluded = normalizeResources(resource.Excluded)

	return resource
}

// resourcesForDisplay adds the legacy Path display without mutating the cache.
func resourcesForDisplay(resources []data.Resource) []data.Resource {
	resources = slices.Clone(resources)
	for i := range resources {
		resources[i].Path = strings.Join(resources[i].Paths, " ")
		resources[i].Excluded = resourcesForDisplay(resources[i].Excluded)
	}

	return resources
}

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
			if checkPaths(res.Paths, path) {
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
