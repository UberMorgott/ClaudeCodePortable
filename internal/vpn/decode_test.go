package vpn

import (
	"os"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	raw, err := os.ReadFile("testdata/sample.vpn")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/expected.conf")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	// Compare line-ending-agnostically: Decode emits LF, but the expected.conf
	// fixture's CRLF/LF flips under .gitattributes text normalization on checkout.
	norm := func(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n") }
	if norm(got) != norm(string(want)) {
		t.Fatalf("decode mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestProxyConf(t *testing.T) {
	got := ProxyConf("X:/stick/data/_run/awg.conf", "127.0.0.1:25345")
	if !strings.Contains(got, "WGConfig = X:/stick/data/_run/awg.conf") ||
		!strings.Contains(got, "BindAddress = 127.0.0.1:25345") {
		t.Fatalf("bad proxy conf: %q", got)
	}
}

func TestDNSPlaceholderSubstitution(t *testing.T) {
	// Decoded config must not contain literal $PRIMARY_DNS/$SECONDARY_DNS.
	raw, _ := os.ReadFile("testdata/sample.vpn")
	got, _ := Decode(string(raw))
	if strings.Contains(got, "$PRIMARY_DNS") || strings.Contains(got, "$SECONDARY_DNS") {
		t.Fatalf("DNS placeholders not substituted: %q", got)
	}
}
