// Package version parses component version strings and compares semvers.
package version

import (
	"regexp"
	"strconv"
)

// Component is a bundled tool with its current and latest-found versions.
type Component struct {
	Name, Current, Found string
	HasUpdate            bool
}

var semver = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// ParseSemverPrefix extracts the first x.y.z from arbitrary --version output.
func ParseSemverPrefix(out string) string {
	m := semver.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	return m[1] + "." + m[2] + "." + m[3]
}

// ParseClaude extracts "2.1.190" from `claude --version` output "2.1.190 (Claude Code)".
func ParseClaude(out string) string {
	return ParseSemverPrefix(out)
}

// triple returns the (major, minor, patch) ints of the first semver in s, ok=false if none.
func triple(s string) ([3]int, bool) {
	m := semver.FindStringSubmatch(s)
	if m == nil {
		return [3]int{}, false
	}
	var t [3]int
	for i := 0; i < 3; i++ {
		t[i], _ = strconv.Atoi(m[i+1])
	}
	return t, true
}

// Newer reports whether b is a strictly newer semver than a. Parts compare as
// integers (2.1.190 > 2.1.9). Unparseable b => false; unparseable a but valid b => true.
func Newer(a, b string) bool {
	tb, okb := triple(b)
	if !okb {
		return false
	}
	ta, oka := triple(a)
	if !oka {
		return true
	}
	for i := 0; i < 3; i++ {
		if tb[i] != ta[i] {
			return tb[i] > ta[i]
		}
	}
	return false
}
