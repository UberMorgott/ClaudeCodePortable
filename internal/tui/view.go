package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/UberMorgott/ClaudeCodePortable/internal/buildinfo"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	headStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	upStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	hiStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
)

// Launch-line click zones (display cells). The 2-cell prefix ("❯ " or "  ") is
// the same width in both cursor states, so these bounds never drift.
func launchZoneEnd() int { return lipgloss.Width("❯ Launch Claude") }
func vpnZoneStart() int  { return launchZoneEnd() + lipgloss.Width("   ") }
func vpnZoneEnd() int    { return vpnZoneStart() + lipgloss.Width("[x] Use VPN") }

func (m model) View() tea.View {
	var b []string

	b = append(b, titleStyle.Render("  ccp portable — launcher ("+buildinfo.Version+")"))
	b = append(b, "")
	// Rows are pre-populated (each spins until its check lands), so the header +
	// every row render every frame — a stable layout with no startup jump.
	b = append(b, headStyle.Render(fmt.Sprintf("  %-16s%-11s%-11s", "COMPONENT", "CURRENT", "FOUND")))
	for _, r := range m.rows {
		b = append(b, m.rowLine(r))
	}
	b = append(b, "")
	b = append(b, m.launchLine())
	b = append(b, m.updateAllLine())
	if m.ccpUpdated {
		b = append(b, m.restartLine())
	}
	b = append(b, "")
	if m.err != "" {
		b = append(b, errStyle.Render("  "+m.err))
	} else {
		b = append(b, "")
	}
	b = append(b, dimStyle.Render("  ↑↓ · enter · v vpn · q"))

	v := tea.NewView(strings.Join(b, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "ccp portable"
	return v
}

func (m model) rowLine(r compRow) string {
	name := fmt.Sprintf("%-16s", r.name)
	cur := fmt.Sprintf("%-11s", dash(normalizeFound(r.current)))
	fnd := fmt.Sprintf("%-11s", dash(normalizeFound(r.found)))
	return "  " + name + cur + fnd + m.glyph(r)
}

func (m model) glyph(r compRow) string {
	switch {
	case r.checking:
		return spinnerStyle.Render(m.spin.View())
	case r.downloading:
		return m.prog.ViewAs(r.pct)
	case r.done:
		return okStyle.Render("✓")
	case r.hasUpdate:
		return upStyle.Render("↑")
	case r.found == "":
		return dimStyle.Render("?")
	default:
		return okStyle.Render("✓")
	}
}

func (m model) launchLine() string {
	prefix := "  "
	if m.cursor == 0 {
		prefix = "❯ "
	}
	lbl := prefix + "Launch Claude"
	switch {
	case m.updating:
		lbl = dimStyle.Render(lbl)
	case m.cursor == 0:
		lbl = hiStyle.Render(lbl)
	}
	box := "[ ]"
	if m.useVPN {
		box = "[x]"
	}
	return lbl + "   " + box + " Use VPN"
}

func (m model) updateAllLine() string {
	prefix := "  "
	if m.cursor == 1 {
		prefix = "❯ "
	}
	if m.updating {
		return spinnerStyle.Render(prefix + m.spin.View() + " Updating…")
	}
	lbl := prefix + "Update all"
	en := updateAllEnabled(m.rows)
	if en {
		lbl += fmt.Sprintf("  (%d)", updatesLeft(m.rows))
	}
	switch {
	case !en:
		return dimStyle.Render(lbl)
	case m.cursor == 1:
		return hiStyle.Render(lbl)
	default:
		return lbl
	}
}

// restartLine is the Restart action, shown only after a ccp self-update. It is
// highlighted to read as obviously actionable.
func (m model) restartLine() string {
	prefix := "  "
	if m.cursor == 2 {
		prefix = "❯ "
	}
	lbl := prefix + "↻ Restart to apply ccp update"
	if m.cursor == 2 {
		return hiStyle.Render(lbl)
	}
	return upStyle.Render(lbl)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
