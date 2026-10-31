// Package statuscode parses lists of HTTP status codes and ranges.
package statuscode

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type span struct{ from, to int }

// Set matches status codes.
type Set []span

var rangeSpaces = regexp.MustCompile(`\s*-\s*`)

// Parse reads codes like "404", "500-599", "500 - 599" or "502, 503". def is
// used when values is empty.
func Parse(name string, values []string, def ...string) (Set, error) {
	if len(values) == 0 {
		values = def
	}

	var set Set

	for _, value := range values {
		value = rangeSpaces.ReplaceAllString(strings.TrimSpace(value), "-")

		for v := range strings.FieldsFuncSeq(value, func(r rune) bool { return r == ',' || r == ' ' }) {
			from, to, isRange := strings.Cut(v, "-")

			a, err := strconv.Atoi(from)
			if err != nil {
				return nil, fmt.Errorf("%s status %q is invalid", name, v)
			}

			b := a
			if isRange {
				if b, err = strconv.Atoi(to); err != nil {
					return nil, fmt.Errorf("%s status %q is invalid", name, v)
				}
			}

			if a < 100 || b > 599 || a > b {
				return nil, fmt.Errorf("%s status %q is out of range", name, v)
			}

			set = append(set, span{a, b})
		}
	}

	if len(set) == 0 && len(values) > 0 {
		return nil, fmt.Errorf("%s statuses are empty", name)
	}

	return set, nil
}

// Contains reports whether code is in the set.
func (s Set) Contains(code int) bool {
	for _, sp := range s {
		if code >= sp.from && code <= sp.to {
			return true
		}
	}

	return false
}

// Min returns the lowest code of the set, 0 when it is empty.
func (s Set) Min() int {
	low := 0
	for _, sp := range s {
		if low == 0 || sp.from < low {
			low = sp.from
		}
	}

	return low
}

// Width is the number of codes the set matches, used to prefer exact codes
// over ranges.
func (s Set) Width() int {
	n := 0
	for _, sp := range s {
		n += sp.to - sp.from + 1
	}

	return n
}
