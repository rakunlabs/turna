package auth

import "strings"

func normalizeAlias(alias string) string {
	// Keep this definition identical to auth_normalize_alias in migration 14.
	alias = strings.Trim(alias, " \t\n\r\f\v")
	if strings.Contains(alias, "@") {
		return strings.ToLower(alias)
	}
	return alias
}

func normalizedAliases(aliases []string) []string {
	result := make([]string, 0, len(aliases))
	seen := make(map[string]bool, len(aliases))
	for _, alias := range aliases {
		alias = normalizeAlias(alias)
		if alias != "" && !seen[alias] {
			result = append(result, alias)
			seen[alias] = true
		}
	}
	return result
}
