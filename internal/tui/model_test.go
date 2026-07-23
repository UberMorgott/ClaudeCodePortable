package tui

import (
	"path/filepath"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "ccp-state.json")

	// Missing file defaults to VPN ON.
	if got := loadState(path); !got.UseVPN {
		t.Fatalf("missing state: want UseVPN=true, got %+v", got)
	}

	for _, want := range []bool{false, true} {
		if err := saveState(path, uiState{UseVPN: want}); err != nil {
			t.Fatalf("saveState(%v): %v", want, err)
		}
		if got := loadState(path); got.UseVPN != want {
			t.Fatalf("round-trip: saved UseVPN=%v, loaded %v", want, got.UseVPN)
		}
	}
}

func TestNormalizeFound(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"1.2.3":         "1.2.3",
		"v1.2.3":        "1.2.3",
		"v2.1.190":      "2.1.190",
		"go1.26.4":      "1.26.4",
		"release-3.0.1": "3.0.1",
		"deadbeef":      "deadbeef", // no semver -> kept as-is (e.g. a commit sha)
	}
	for in, want := range cases {
		if got := normalizeFound(in); got != want {
			t.Errorf("normalizeFound(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPredicates(t *testing.T) {
	rows := []compRow{
		{name: "ccp", hasUpdate: false},
		{name: "claude", hasUpdate: true},
		{name: "rtk", hasUpdate: true, downloading: true, pct: 0.5},
		{name: "wireproxy-awg", hasUpdate: false, done: true},
	}

	if !anyUpdating(rows) {
		t.Error("anyUpdating: want true (rtk downloading)")
	}
	if launchEnabled(rows) {
		t.Error("launchEnabled: want false while a row is downloading")
	}
	if !updateAllEnabled(rows) {
		t.Error("updateAllEnabled: want true (claude + rtk have updates)")
	}
	if got := updatesLeft(rows); got != 2 {
		t.Errorf("updatesLeft = %d, want 2", got)
	}

	// Idle set: nothing downloading, nothing to update.
	idle := []compRow{{name: "ccp"}, {name: "claude", done: true}}
	if anyUpdating(idle) {
		t.Error("anyUpdating(idle): want false")
	}
	if !launchEnabled(idle) {
		t.Error("launchEnabled(idle): want true")
	}
	if updateAllEnabled(idle) {
		t.Error("updateAllEnabled(idle): want false")
	}
	if got := updatesLeft(idle); got != 0 {
		t.Errorf("updatesLeft(idle) = %d, want 0", got)
	}
}

// TestLayoutGeometry pins the View/hit-test contract: the geometry helpers must
// agree with the line order View renders (headerRows header lines, then rows,
// then a blank, then Launch, then Update all).
func TestLayoutGeometry(t *testing.T) {
	m := model{rows: make([]compRow, 5)}
	if got := m.compRowY(0); got != headerRows {
		t.Errorf("compRowY(0) = %d, want %d", got, headerRows)
	}
	if got := m.compRowY(4); got != headerRows+4 {
		t.Errorf("compRowY(4) = %d, want %d", got, headerRows+4)
	}
	if got := m.launchRowY(); got != headerRows+5+1 {
		t.Errorf("launchRowY = %d, want %d", got, headerRows+5+1)
	}
	if got := m.updateAllRowY(); got != headerRows+5+2 {
		t.Errorf("updateAllRowY = %d, want %d", got, headerRows+5+2)
	}

	// Launch-line X zones must be disjoint and ordered.
	if launchZoneEnd() <= 0 || vpnZoneStart() < launchZoneEnd() || vpnZoneEnd() <= vpnZoneStart() {
		t.Errorf("zones out of order: launchEnd=%d vpnStart=%d vpnEnd=%d",
			launchZoneEnd(), vpnZoneStart(), vpnZoneEnd())
	}
}
