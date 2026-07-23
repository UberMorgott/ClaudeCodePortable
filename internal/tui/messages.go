package tui

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/UberMorgott/ClaudeCodePortable/internal/buildinfo"
	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
	"github.com/UberMorgott/ClaudeCodePortable/internal/update"
	"github.com/UberMorgott/ClaudeCodePortable/internal/version"
	"github.com/UberMorgott/ClaudeCodePortable/internal/vpn"
)

// Async messages fed back into the bubbletea Update loop.
type (
	// checkDoneMsg carries the version rows resolved by the initial CheckAll.
	checkDoneMsg struct{ rows []compRow }
	// progressMsg updates one component row's download bar.
	progressMsg struct {
		name string
		pct  float64
	}
	// installDoneMsg reports a finished (or failed) component install.
	installDoneMsg struct {
		name, newVer string
		err          error
	}
	// vpnValidateMsg is the result of validating the first wg-config/*.vpn.
	vpnValidateMsg struct {
		ok     bool
		reason string
	}
)

// vpnGenerateMsg is shown when Use VPN is toggled on but no valid .vpn exists.
const vpnGenerateMsg = `generate config in Amnezia and place in wg-config\`

// checkCmd resolves ccp (self) + every bundled component's Current/Found off the
// bubbletea loop and returns them as a single checkDoneMsg.
func checkCmd(l paths.Layout) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()

		rows := make([]compRow, 0, 8)
		// ccp itself first.
		self := compRow{name: "ccp", current: buildinfo.Version}
		if found, err := update.SelfLatest(ctx); err == nil {
			self.found = found
			self.hasUpdate = version.Newer(buildinfo.Version, found)
		}
		rows = append(rows, self)

		for _, c := range update.CheckAll(ctx, l) {
			rows = append(rows, compRow{
				name:      c.Name,
				current:   c.Current,
				found:     c.Found,
				hasUpdate: c.HasUpdate,
			})
		}
		return checkDoneMsg{rows: rows}
	}
}

// listen delivers the next install/progress event from the shared channel as a
// tea.Msg. Its handlers re-issue it, so exactly one listener stays alive.
func listen(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// startInstallCmd kicks off a component install in a goroutine that streams
// progressMsg and ends with an installDoneMsg on the shared channel. The Cmd
// itself returns nil immediately (the events arrive via the listener).
func startInstallCmd(ch chan tea.Msg, l paths.Layout, c update.Comp, ver string) tea.Cmd {
	return func() tea.Msg {
		go func() {
			err := c.Install(context.Background(), l, ver, func(done, total int64) {
				pct := 0.0
				if total > 0 {
					pct = float64(done) / float64(total)
				}
				ch <- progressMsg{name: c.Name, pct: pct}
			})
			ch <- installDoneMsg{name: c.Name, newVer: ver, err: err}
		}()
		return nil
	}
}

// startSelfCmd self-updates ccp.exe in a goroutine (go-selfupdate has no byte
// progress hook), ending with an installDoneMsg on the shared channel.
func startSelfCmd(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			ver, err := update.SelfUpdate(context.Background())
			ch <- installDoneMsg{name: "ccp", newVer: ver, err: err}
		}()
		return nil
	}
}

// vpnValidateCmd checks that the first wg-config/*.vpn decodes; failure resolves
// to vpnValidateMsg{ok:false} with the generate-config hint.
func vpnValidateCmd(l paths.Layout) tea.Cmd {
	return func() tea.Msg {
		f, err := firstVPNFile(l.WGConfig)
		if err != nil {
			return vpnValidateMsg{ok: false, reason: vpnGenerateMsg}
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return vpnValidateMsg{ok: false, reason: vpnGenerateMsg}
		}
		if _, err := vpn.Decode(string(b)); err != nil {
			return vpnValidateMsg{ok: false, reason: vpnGenerateMsg}
		}
		return vpnValidateMsg{ok: true}
	}
}
