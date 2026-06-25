package loader

import "strings"

// InnerPath returns the value at the / separated path inside v.
//
// An empty path returns v unchanged. A missing path returns nil.
func InnerPath(s string, v map[string]interface{}) interface{} {
	if s == "" {
		return v
	}

	if v == nil {
		return nil
	}

	maps := strings.Split(s, "/")

	// find the inner path
	for _, m := range maps {
		if m == maps[len(maps)-1] {
			// found correct path
			return v[m]
		}

		if v[m] == nil {
			return nil
		}

		var ok bool
		v, ok = v[m].(map[string]interface{})

		if !ok {
			return nil
		}
	}

	return nil
}

// MapPath wraps v inside a nested map described by the / separated path s.
//
// An empty path returns v unchanged.
func MapPath(s string, v interface{}) interface{} {
	if s == "" {
		return v
	}

	maps := strings.Split(s, "/")

	mapDef := map[string]interface{}{}
	mapRange := mapDef
	for _, m := range maps {
		if m == maps[len(maps)-1] {
			mapRange[m] = v
			break
		}

		mapRange[m] = map[string]interface{}{}
		mapRange = mapRange[m].(map[string]interface{})
	}

	return mapDef
}
