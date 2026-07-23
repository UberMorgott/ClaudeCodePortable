// Package routing generates a fail-closed split-tunnel PAC and wires it into
// the Windows per-user proxy config (HKCU AutoConfigURL + WinINET refresh).
package routing

import (
	"fmt"
	"strings"
)

// Marker is written as a comment at the top of every PAC we generate so
// SelfHeal/Disable can recognize a config as ours before removing it.
const Marker = "ccp-portable"

// BuildPAC renders a fail-closed PAC: matched domains route ONLY through proxy
// (no "; DIRECT" fallback, so a dead tunnel fails closed instead of leaking),
// every other host goes DIRECT (that is the split, not a fallback).
func BuildPAC(domains []string, proxy string) string {
	var b strings.Builder
	b.WriteString("// " + Marker + "\nfunction FindProxyForURL(url, host) {\n")
	for _, d := range domains {
		fmt.Fprintf(&b, "  if (shExpMatch(host, %q)) return \"PROXY %s\";\n", d, proxy)
	}
	b.WriteString("  return \"DIRECT\";\n}\n")
	return b.String()
}
