// Package tui is the reactive bubbletea (v2) launcher menu for ccp: a live
// version header for every bundled component (plus ccp itself), per-component and
// bulk updates with progress bars, a persisted Use VPN toggle, and the launch
// handoff to claude.exe.
package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/UberMorgott/ClaudeCodePortable/internal/buildinfo"
	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
	"github.com/UberMorgott/ClaudeCodePortable/internal/update"
	"github.com/UberMorgott/ClaudeCodePortable/internal/version"
)

// compRow is one component's live state in the version header.
type compRow struct {
	name           string
	current, found string // raw versions; normalized for display via normalizeFound
	hasUpdate      bool
	checking       bool // version check in flight -> row shows a spinner
	downloading    bool
	pct            float64 // 0..1
	done           bool
}

type model struct {
	l          paths.Layout
	rows       []compRow
	comps      []update.Comp // source table, for looking up a row's Install func
	cursor     int           // 0=Launch, 1=Update all (component rows are click-only)
	useVPN     bool          // checkbox, persisted to data/claude-cfg/ccp-state.json
	updating   bool          // any download in flight -> Launch disabled
	launching  bool          // Launch chosen; quitting to hand off the console
	launch     bool          // final intent read by Run after Quit
	ccpUpdated bool          // a newer ccp was self-updated this session -> Restart offered
	restart    bool          // final intent read by Run after Quit: re-exec the new binary
	err        string
	spin       spinner.Model
	prog       progress.Model
	events     chan tea.Msg // install progress + completion events
}

func newModel(l paths.Layout) model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = spinnerStyle
	comps := update.Components()
	// Pre-populate rows in fixed order so the layout is stable from frame 1:
	// ccp first, then one row per component. Versions resolve async (checkOneMsg).
	rows := make([]compRow, 0, len(comps)+1)
	rows = append(rows, compRow{name: "ccp", current: buildinfo.Version, checking: true})
	for _, c := range comps {
		rows = append(rows, compRow{name: c.Name, checking: true})
	}
	return model{
		l:      l,
		rows:   rows,
		comps:  comps,
		useVPN: loadState(stateFilePath(l)).UseVPN,
		spin:   sp,
		prog:   progress.New(progress.WithWidth(22)),
		events: make(chan tea.Msg, 128),
	}
}

// compFor returns the source Comp for a row name (component rows only, not ccp).
func (m model) compFor(name string) (update.Comp, bool) {
	for _, c := range m.comps {
		if c.Name == name {
			return c, true
		}
	}
	return update.Comp{}, false
}

// --- pure predicates over the rows (unit-tested) --------------------------

func anyUpdating(rows []compRow) bool {
	for _, r := range rows {
		if r.downloading {
			return true
		}
	}
	return false
}

// launchEnabled: Launch is greyed while any component is downloading.
func launchEnabled(rows []compRow) bool { return !anyUpdating(rows) }

// updateAllEnabled: Update all is active only when at least one row has an update.
func updateAllEnabled(rows []compRow) bool { return updatesLeft(rows) > 0 }

// updatesLeft counts rows that still have a pending update.
func updatesLeft(rows []compRow) int {
	n := 0
	for _, r := range rows {
		if r.hasUpdate {
			n++
		}
	}
	return n
}

// normalizeFound renders a version for display: a v-prefixed or noisy tag is
// reduced to its x.y.z; a value with no semver (e.g. a commit sha) is kept as-is.
func normalizeFound(v string) string {
	if v == "" {
		return ""
	}
	if p := version.ParseSemverPrefix(v); p != "" {
		return p
	}
	return v
}

// --- persisted UI state ---------------------------------------------------

type uiState struct {
	UseVPN bool `json:"useVPN"`
}

func stateFilePath(l paths.Layout) string {
	return filepath.Join(l.ClaudeCfg, "ccp-state.json")
}

// loadState reads the persisted UI state; a missing/unreadable file defaults to
// Use VPN ON.
func loadState(path string) uiState {
	s := uiState{UseVPN: true}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	return s
}

func saveState(path string, s uiState) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// firstVPNFile returns the first (sorted) wg-config/*.vpn path.
func firstVPNFile(wgDir string) (string, error) {
	matches, _ := filepath.Glob(filepath.Join(wgDir, "*.vpn"))
	sort.Strings(matches)
	if len(matches) == 0 {
		return "", fmt.Errorf("no .vpn config in %s", wgDir)
	}
	return matches[0], nil
}

// --- layout geometry: shared by View and the mouse hit-test ---------------

// headerRows is the fixed number of lines before the component rows: title,
// blank, column header.
const headerRows = 3

func (m model) compRowY(i int) int { return headerRows + i }
func (m model) launchRowY() int    { return headerRows + len(m.rows) + 1 }
func (m model) updateAllRowY() int { return headerRows + len(m.rows) + 2 }

// restartRowY is the Restart action's line, rendered only when ccpUpdated.
func (m model) restartRowY() int { return headerRows + len(m.rows) + 3 }
