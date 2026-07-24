package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/UberMorgott/ClaudeCodePortable/internal/buildinfo"
	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
	"github.com/UberMorgott/ClaudeCodePortable/internal/update"
)

// TestEnterActivatesLaunch guards the eval-order fix: pressing enter on the
// Launch cursor must set launch=true on the RETURNED model. With the buggy
// `return m, m.activate()` form the copy of m is captured before activate()'s
// mutation, so launch stays false and this fails.
func TestEnterActivatesLaunch(t *testing.T) {
	m := model{cursor: 0} // 0 = Launch, not updating
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !next.(model).launch {
		t.Fatal("enter on Launch: returned model.launch = false, want true")
	}
}

// TestEnterUpdateAllSetsUpdating guards the same fix on the Update-all path:
// enter on cursor 1 with an updatable row must set updating=true (and mark the
// row downloading) on the RETURNED model.
func TestEnterUpdateAllSetsUpdating(t *testing.T) {
	m := model{cursor: 1, rows: []compRow{{name: "claude", hasUpdate: true}}}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	nm := next.(model)
	if !nm.updating {
		t.Error("enter on Update all: returned model.updating = false, want true")
	}
	if !nm.rows[0].downloading {
		t.Error("enter on Update all: row not marked downloading on returned model")
	}
}

// TestUpdateAllLineSpinner guards the UX fix: while updating, the Update-all
// line shows a live spinner ("Updating…"), not the static "Update all" label.
func TestUpdateAllLineSpinner(t *testing.T) {
	m := newModel(paths.Resolve(filepath.Join(t.TempDir(), "ccp.exe")))
	m.rows = []compRow{{name: "claude", hasUpdate: true}}

	m.updating = true
	got := m.updateAllLine()
	if !strings.Contains(got, "Updating") {
		t.Errorf("updating: updateAllLine() = %q, want it to contain \"Updating\"", got)
	}
	if strings.Contains(got, "Update all") {
		t.Errorf("updating: updateAllLine() = %q, should not show the static label", got)
	}

	m.updating = false
	if got := m.updateAllLine(); !strings.Contains(got, "Update all") {
		t.Errorf("idle: updateAllLine() = %q, want it to contain \"Update all\"", got)
	}
}

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

// TestCheckOneMsgUpdatesOnlyThatRow: a per-component check result fills in only
// its own row (current/found/hasUpdate, checking=false) and leaves siblings'
// checking flag untouched — one slow check can't stall another.
func TestCheckOneMsgUpdatesOnlyThatRow(t *testing.T) {
	m := model{rows: []compRow{
		{name: "ccp", checking: true},
		{name: "claude", checking: true},
	}}
	next, _ := m.Update(checkOneMsg{name: "claude", current: "1.0.0", found: "1.2.0", hasUpdate: true})
	nm := next.(model)
	if nm.rows[1].checking {
		t.Error("claude row still checking after its checkOneMsg")
	}
	if nm.rows[1].current != "1.0.0" || nm.rows[1].found != "1.2.0" || !nm.rows[1].hasUpdate {
		t.Errorf("claude row not filled in: %+v", nm.rows[1])
	}
	if !nm.rows[0].checking {
		t.Error("ccp row checking flipped by an unrelated checkOneMsg")
	}
}

// TestNewModelPrePopulatesRows: rows exist from frame 1 in fixed order — ccp
// first, then every Components() entry — all marked checking.
func TestNewModelPrePopulatesRows(t *testing.T) {
	m := newModel(paths.Resolve(filepath.Join(t.TempDir(), "ccp.exe")))
	if want := len(update.Components()) + 1; len(m.rows) != want {
		t.Fatalf("newModel rows = %d, want %d", len(m.rows), want)
	}
	if m.rows[0].name != "ccp" {
		t.Errorf("first row = %q, want ccp", m.rows[0].name)
	}
	if m.rows[0].current != buildinfo.Version {
		t.Errorf("ccp current = %q, want %q", m.rows[0].current, buildinfo.Version)
	}
	for _, r := range m.rows {
		if !r.checking {
			t.Errorf("row %q not marked checking on init", r.name)
		}
	}
}

// TestInstallDoneCcpUpdatedFlag: a real newer ccp self-update lights ccpUpdated;
// a no-op self-update (SelfUpdate returns the current version) does not.
func TestInstallDoneCcpUpdatedFlag(t *testing.T) {
	newer := model{rows: []compRow{{name: "ccp", downloading: true}}, events: make(chan tea.Msg, 8)}
	next, _ := newer.Update(installDoneMsg{name: "ccp", newVer: "9999.0.0"})
	if !next.(model).ccpUpdated {
		t.Error("newer self-update: want ccpUpdated=true")
	}

	noop := model{rows: []compRow{{name: "ccp", downloading: true}}, events: make(chan tea.Msg, 8)}
	next2, _ := noop.Update(installDoneMsg{name: "ccp", newVer: buildinfo.Version})
	if next2.(model).ccpUpdated {
		t.Error("no-op self-update: want ccpUpdated=false")
	}
}

// TestLaunchEnabledWhileChecking: checking must NOT gate Launch — only active
// downloads do.
func TestLaunchEnabledWhileChecking(t *testing.T) {
	rows := []compRow{{name: "ccp", checking: true}, {name: "claude", checking: true}}
	if !launchEnabled(rows) {
		t.Error("Launch must stay enabled while rows are still checking")
	}
}

// TestCursorReachesRestartOnlyWhenUpdated: the Restart cursor index (2) is
// reachable only after ccp self-updates.
func TestCursorReachesRestartOnlyWhenUpdated(t *testing.T) {
	locked := model{cursor: 1}
	next, _ := locked.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if next.(model).cursor != 1 {
		t.Errorf("cursor moved past 1 without ccpUpdated: got %d", next.(model).cursor)
	}

	open := model{cursor: 1, ccpUpdated: true}
	next2, _ := open.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if next2.(model).cursor != 2 {
		t.Errorf("cursor did not reach Restart with ccpUpdated: got %d", next2.(model).cursor)
	}
}

// TestActivateRestartSetsRestartIntent: enter on the Restart cursor sets the
// restart intent flag (read by Run after quit); no exec happens here.
func TestActivateRestartSetsRestartIntent(t *testing.T) {
	m := model{cursor: 2, ccpUpdated: true}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !next.(model).restart {
		t.Error("enter on Restart: returned model.restart = false, want true")
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
