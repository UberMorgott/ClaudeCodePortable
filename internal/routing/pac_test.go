package routing

import (
	"strings"
	"testing"
)

func TestBuildPACFailClosed(t *testing.T) {
	pac := BuildPAC([]string{"*.anthropic.com", "*.claude.ai"}, "127.0.0.1:25345")
	if !strings.Contains(pac, `shExpMatch(host, "*.anthropic.com")`) {
		t.Fatal("missing anthropic rule")
	}
	if !strings.Contains(pac, `return "PROXY 127.0.0.1:25345"`) {
		t.Fatal("missing proxy return")
	}
	if strings.Contains(pac, "DIRECT\"") && strings.Contains(pac, "PROXY 127.0.0.1:25345; DIRECT") {
		t.Fatal("fail-closed violated: proxy line must not fall back to DIRECT")
	}
	if !strings.Contains(pac, `return "DIRECT"`) { // non-matched hosts still go direct
		t.Fatal("non-matched hosts must be DIRECT")
	}
}
