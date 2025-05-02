package accesslog

import (
	"slices"
	"strings"
)

type Method struct {
	Blocked  []string `cfg:"blocked"`
	Allowed  []string `cfg:"allowed"`
	AllowAll bool     `cfg:"allow_all"`
}

func NewMethod(methods []string) *Method {
	var blocked, allowed []string
	allowAll := false

	if methods == nil {
		methods = []string{"*"}
	}

	for _, method := range methods {
		switch {
		case method == "":
			continue
		case strings.HasPrefix(method, "+"):
			allowed = append(allowed, sanitizeMethod(strings.TrimPrefix(method, "+")))
		case strings.HasPrefix(method, "-"):
			blocked = append(blocked, sanitizeMethod(strings.TrimPrefix(method, "-")))
		case method == "*":
			allowAll = true
		default:
			allowed = append(allowed, sanitizeMethod(method))
		}
	}

	return &Method{
		Blocked:  blocked,
		Allowed:  allowed,
		AllowAll: allowAll,
	}
}

func (r *Method) IsIn(method string) bool {
	method = sanitizeMethod(method)

	if slices.Contains(r.Blocked, method) {
		return false
	}

	if slices.Contains(r.Allowed, method) {
		return true
	}

	if r.AllowAll {
		return true
	}

	return false
}

func sanitizeMethod(method string) string {
	return strings.ToUpper(strings.TrimSpace(method))
}
