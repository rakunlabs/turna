package docker

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/gofret"
)

// labelTree turns flat labels below prefix into a nested map: "a.b.c=v"
// becomes {"a": {"b": {"c": "v"}}}. Keys are case sensitive as they become
// names of routers and middlewares; struct fields match loosely when decoded.
func labelTree(labels map[string]string, prefix string) (map[string]any, error) {
	root := make(map[string]any)

	keys := make([]string, 0, len(labels))
	for k := range labels {
		if strings.HasPrefix(k, prefix+".") {
			keys = append(keys, k)
		}
	}

	slices.Sort(keys)

	for _, k := range keys {
		path := strings.Split(strings.TrimPrefix(k, prefix+"."), ".")
		if slices.Contains(path, "") {
			return nil, fmt.Errorf("label %q has an empty segment", k)
		}

		node := root
		for i, p := range path {
			last := i == len(path)-1

			cur, exists := node[p]
			switch {
			case last && exists:
				return nil, fmt.Errorf("label %q conflicts with a nested label", k)
			case last:
				node[p] = labels[k]
			case !exists:
				child := make(map[string]any)
				node[p] = child
				node = child
			default:
				child, ok := cur.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("label %q conflicts with label %s", k, prefix+"."+strings.Join(path[:i+1], "."))
				}

				node = child
			}
		}
	}

	return root, nil
}

// codec decodes label values: strings convert to numbers, booleans and
// durations, comma separated strings or maps with index keys ("x.0", "x.1")
// become lists, and "true" or "" enables a section like tls: {}.
var codec = gofret.New(
	gofret.WithErrorUnused(),
	gofret.WithHooks(labelHook),
)

func decode(in, out any) error {
	return codec.ToInto(in, out)
}

func labelHook(c gofret.HookCtx) (any, error) {
	if c.From == nil || c.To == nil {
		return nil, gofret.ErrSkip
	}

	to := c.To
	for to.Kind() == reflect.Pointer {
		to = to.Elem()
	}

	switch {
	case c.From.Kind() == reflect.String && to.Kind() == reflect.Slice && to.Elem().Kind() != reflect.Uint8:
		value := c.Value.String()

		// patterns may contain commas, like ^/a{1,3}$; set them as indexed
		// lists (path_match.0) instead
		if isPatternField(c.Tag.Name) {
			return []any{value}, nil
		}

		parts := strings.Split(value, ",")
		out := make([]any, 0, len(parts))

		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}

		return out, nil
	case c.From.Kind() == reflect.Map && to.Kind() == reflect.Slice:
		m, ok := c.Value.Interface().(map[string]any)
		if !ok {
			return nil, gofret.ErrSkip
		}

		return indexedList(c.Path(), m)
	case c.From.Kind() == reflect.String && to.Kind() == reflect.Struct && to != reflect.TypeFor[time.Time]():
		// "true" enables a section with its defaults, like health_check=true
		switch strings.ToLower(strings.TrimSpace(c.Value.String())) {
		case "", "true":
			return map[string]any{}, nil
		default:
			return nil, fmt.Errorf("%s: use true to enable or set its fields", c.Path())
		}
	}

	return nil, gofret.ErrSkip
}

func isPatternField(name string) bool {
	name = strings.ToLower(name)

	return strings.Contains(name, "match") || strings.Contains(name, "regex") || strings.Contains(name, "pattern")
}

func indexedList(path string, m map[string]any) ([]any, error) {
	type item struct {
		i int
		v any
	}

	items := make([]item, 0, len(m))
	for k, v := range m {
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 {
			return nil, fmt.Errorf("%s: list key %q is not an index", path, k)
		}

		items = append(items, item{i, v})
	}

	slices.SortFunc(items, func(a, b item) int { return a.i - b.i })

	out := make([]any, len(items))
	for i, it := range items {
		out[i] = it.v
	}

	return out, nil
}

// middlewareType returns the only key of a middleware definition, which names
// the middleware type.
func middlewareType(def any) (string, map[string]any, error) {
	m, ok := def.(map[string]any)
	if !ok {
		return "", nil, fmt.Errorf("middleware must have a type, like <name>.strip_prefix.prefix=/api")
	}

	if len(m) != 1 {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}

		slices.Sort(keys)

		return "", nil, fmt.Errorf("middleware must have exactly one type, found %s", strings.Join(keys, ", "))
	}

	for typ, v := range m {
		return typ, map[string]any{typ: v}, nil
	}

	return "", nil, nil
}

// canonicalType returns the configuration key of a middleware type, label
// keys match loosely like strip-prefix or stripPrefix.
func canonicalType(typ string, known []string) (string, bool) {
	k := gofret.LooseKey(typ)
	for _, t := range known {
		if gofret.LooseKey(t) == k {
			return t, true
		}
	}

	return "", false
}

// cfgKeys returns the cfg keys of the fields of a struct type.
func cfgKeys(t reflect.Type) []string {
	out := make([]string, 0, t.NumField())
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("cfg"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}

	return out
}
