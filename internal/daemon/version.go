package daemon

import (
	"strconv"
	"strings"
)

// ParseVersion reads vMAJOR.MINOR.PATCH. Build metadata after "+", which source builds carry, is ignored; anything else, such as "dev", is not a version.
func ParseVersion(v string) ([3]int, bool) {
	var parts [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(v, "v"), "+")
	fields := strings.Split(core, ".")
	if len(fields) != 3 {
		return parts, false
	}
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

// NewerVersion reports whether latest is a later release than current. Unparsable versions never count as newer.
func NewerVersion(latest, current string) bool {
	l, ok := ParseVersion(latest)
	c, ok2 := ParseVersion(current)
	if !ok || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}
