package version

import "testing"

func TestParseClaude(t *testing.T) {
	if got := ParseClaude("2.1.190 (Claude Code)\n"); got != "2.1.190" {
		t.Fatalf("got %q", got)
	}
}
func TestParseSemverPrefix(t *testing.T) {
	if got := ParseSemverPrefix("rtk 0.9.0"); got != "0.9.0" {
		t.Fatalf("got %q", got)
	}
	if got := ParseSemverPrefix("v1.0.9-awg"); got != "1.0.9" {
		t.Fatalf("got %q", got)
	}
}
func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.8.2", "0.9.0", true}, {"1.0.9", "1.0.9", false},
		{"2.1.190", "2.1.9", false}, {"", "1.0.0", true}, {"1.0.0", "", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Fatalf("Newer(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}
