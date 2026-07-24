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
	// checkOneMsg carries one component's resolved version check, streamed back
	// independently so a slow/failed check never blocks the others.
	checkOneMsg struct {
		name, current, found string
		hasUpdate            bool
	}
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

// checkCcpCmd resolves ccp's own version (current from buildinfo, latest from the
// release feed) off the bubbletea loop, on its own timeout, as one checkOneMsg.
func checkCcpCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		found, err := update.SelfLatest(ctx)
		if err != nil {
			found = ""
		}
		return checkOneMsg{
			name:      "ccp",
			current:   buildinfo.Version,
			found:     found,
			hasUpdate: found != "" && version.Newer(buildinfo.Version, found),
		}
	}
}

// checkCompCmd resolves one bundled component's Current (local) + Latest (network)
// on its OWN context/timeout, so one hang can't block the others. Returns a
// checkOneMsg; a network failure leaves found "" and hasUpdate false.
func checkCompCmd(l paths.Layout, c update.Comp) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		current := c.Current(l)
		found, err := c.Latest(ctx)
		if err != nil {
			found = ""
		}
		return checkOneMsg{
			name:      c.Name,
			current:   current,
			found:     found,
			hasUpdate: found != "" && version.Newer(current, found),
		}
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
