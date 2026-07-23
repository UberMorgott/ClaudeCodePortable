package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/spinner"
)

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, checkCmd(m.l), listen(m.events))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case checkDoneMsg:
		m.rows = msg.rows
		return m, nil

	case progressMsg:
		for i := range m.rows {
			if m.rows[i].name == msg.name {
				m.rows[i].pct = msg.pct
			}
		}
		return m, listen(m.events)

	case installDoneMsg:
		for i := range m.rows {
			if m.rows[i].name != msg.name {
				continue
			}
			m.rows[i].downloading = false
			if msg.err != nil {
				m.err = msg.name + ": " + msg.err.Error()
			} else {
				m.rows[i].done = true
				m.rows[i].hasUpdate = false
				m.rows[i].current = msg.newVer
				m.rows[i].found = msg.newVer
				m.rows[i].pct = 1
			}
		}
		m.updating = anyUpdating(m.rows)
		return m, listen(m.events)

	case vpnValidateMsg:
		if msg.ok {
			m.useVPN = true
			m.err = ""
			_ = saveState(stateFilePath(m.l), uiState{UseVPN: true})
		} else {
			m.useVPN = false
			m.err = msg.reason
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseClickMsg:
		mc := msg.Mouse()
		if mc.Button != tea.MouseLeft {
			return m, nil
		}
		return m.handleClick(mc.X, mc.Y)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "j":
		if m.cursor < 1 {
			m.cursor++
		}
		return m, nil
	case "v":
		return m.toggleVPN()
	case "enter":
		// Evaluate the mutating pointer method as a STATEMENT before returning m:
		// in `return m, m.activate()` the copy of m is unordered vs. the call, so
		// the returned model could keep pre-mutation fields (launch/updating).
		cmd := m.activate()
		return m, cmd
	}
	return m, nil
}

// handleClick dispatches a left click by hit-testing its Y against the geometry
// helpers (compRowY/launchRowY/updateAllRowY) that View renders to, and its X
// against the launch/vpn zones on the Launch line.
func (m model) handleClick(x, y int) (tea.Model, tea.Cmd) {
	for i := range m.rows {
		if y == m.compRowY(i) {
			if m.rows[i].hasUpdate && !m.rows[i].downloading {
				cmd := m.startUpdate(m.rows[i].name)
				return m, cmd
			}
			return m, nil
		}
	}
	if y == m.launchRowY() {
		switch {
		case x >= 0 && x < launchZoneEnd():
			m.cursor = 0
			cmd := m.activate()
			return m, cmd
		case x >= vpnZoneStart() && x < vpnZoneEnd():
			return m.toggleVPN()
		}
		return m, nil
	}
	if y == m.updateAllRowY() {
		m.cursor = 1
		cmd := m.activate()
		return m, cmd
	}
	return m, nil
}

// activate performs the action under the keyboard cursor (0=Launch, 1=Update all).
func (m *model) activate() tea.Cmd {
	switch m.cursor {
	case 0:
		if m.updating {
			return nil // Launch disabled while updating
		}
		m.launching = true
		m.launch = true
		return tea.Quit
	case 1:
		if !updateAllEnabled(m.rows) {
			return nil
		}
		var names []string
		for _, r := range m.rows {
			if r.hasUpdate {
				names = append(names, r.name)
			}
		}
		return m.startUpdate(names...)
	}
	return nil
}

// startUpdate marks each named updatable row downloading and returns a batched
// Cmd that starts each install; ccp routes to the self-updater.
func (m *model) startUpdate(names ...string) tea.Cmd {
	var cmds []tea.Cmd
	for _, name := range names {
		for i := range m.rows {
			if m.rows[i].name != name || !m.rows[i].hasUpdate || m.rows[i].downloading {
				continue
			}
			m.rows[i].downloading = true
			m.rows[i].pct = 0
			if name == "ccp" {
				cmds = append(cmds, startSelfCmd(m.events))
			} else if c, ok := m.compFor(name); ok {
				cmds = append(cmds, startInstallCmd(m.events, m.l, c, m.rows[i].found))
			}
		}
	}
	m.updating = anyUpdating(m.rows)
	return tea.Batch(cmds...)
}

// toggleVPN flips the Use VPN checkbox. Turning OFF is immediate and persisted;
// turning ON first validates the .vpn (result arrives as vpnValidateMsg).
func (m model) toggleVPN() (tea.Model, tea.Cmd) {
	if m.useVPN {
		m.useVPN = false
		m.err = ""
		_ = saveState(stateFilePath(m.l), uiState{UseVPN: false})
		return m, nil
	}
	return m, vpnValidateCmd(m.l)
}
