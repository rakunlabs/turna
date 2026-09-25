// Package snimatch matches host names against simple wildcard patterns.
package snimatch

import "strings"

// Match matches a lower case host against a pattern: "*" matches everything
// and "*.example.com" matches any subdomain of example.com.
func Match(pattern, host string) bool {
	if pattern == "*" || pattern == host {
		return true
	}

	if suffix, ok := strings.CutPrefix(pattern, "*"); ok && strings.HasPrefix(suffix, ".") {
		return strings.HasSuffix(host, suffix) && len(host) > len(suffix)
	}

	return false
}
