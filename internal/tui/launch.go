package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/UberMorgott/ClaudeCodePortable/internal/claude"
	"github.com/UberMorgott/ClaudeCodePortable/internal/config"
	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
	"github.com/UberMorgott/ClaudeCodePortable/internal/routing"
	"github.com/UberMorgott/ClaudeCodePortable/internal/tunnel"
	"github.com/UberMorgott/ClaudeCodePortable/internal/vpn"
)

const (
	bindAddr = "127.0.0.1:25345"
	proxyURL = "http://" + bindAddr
)

// Run drives the launcher TUI, then — if the operator chose Launch — hands the
// console to claude.exe through the VPN/routing setup. It is the ccp entrypoint.
func Run(l paths.Layout, workDir string) error {
	if err := l.EnsureRuntimeDirs(); err != nil {
		return err
	}
	out, err := tea.NewProgram(newModel(l)).Run()
	if err != nil {
		return err
	}
	final, _ := out.(model)
	// Restart takes precedence over launch: ccp self-updated on disk, so re-exec
	// the freshly-written binary (os.Executable now points at it) instead.
	if final.restart {
		return reexec()
	}
	if !final.launch {
		return nil
	}
	return launch(l, final.useVPN, workDir)
}

// reexec runs the current on-disk executable (the just-applied new ccp) with the
// same args, handing it this console; when it exits, so does this process.
func reexec() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// launch runs the VPN tunnel + split-tunnel routing (when useVPN) and execs
// claude.exe with the pinned env, tearing everything down on exit. It runs only
// after the TUI has fully quit, so claude owns the console.
func launch(l paths.Layout, useVPN bool, workDir string) error {
	pidFile := filepath.Join(l.Run, "wireproxy.pid")
	pacFile := filepath.Join(l.Run, "proxy.pac")

	// Heal any stale state from a prior hard-killed session, then start clean.
	_ = routing.SelfHeal()
	_ = tunnel.KillStalePID(pidFile)
	_ = os.RemoveAll(l.Run)
	if err := os.MkdirAll(l.Run, 0o755); err != nil {
		return err
	}

	// Seed baseline config onto a blank stick (write-if-absent); fail closed so
	// claude never launches without settings.json/CLAUDE.md. Independent of VPN,
	// so it must run before the VPN branch can early-return on a missing .vpn.
	if err := config.Seed(l.ClaudeCfg); err != nil {
		return err
	}

	var tun *tunnel.Tunnel
	pacEnabled := false
	defer func() {
		if pacEnabled {
			_ = routing.Disable(pacFile)
		}
		_ = tun.Close() // nil-safe
		_ = os.RemoveAll(l.Run)
	}()

	if useVPN {
		vf, err := firstVPNFile(l.WGConfig)
		if err != nil {
			return errors.New(vpnGenerateMsg)
		}
		if tunnel.PortInUse(bindAddr) {
			return fmt.Errorf("%s is already in use by another process", bindAddr)
		}
		b, err := os.ReadFile(vf)
		if err != nil {
			return err
		}
		proxyConf, err := vpn.WriteRuntime(string(b), l.Run, bindAddr)
		if err != nil {
			return err
		}
		wpExe := l.BinPath("wireproxy-awg.exe")
		if err := tunnel.Validate(wpExe, proxyConf); err != nil {
			return err
		}
		tun, err = tunnel.Start(wpExe, proxyConf, pidFile)
		if err != nil {
			return err
		}
		// Confirm wireproxy actually bound the port; if it never listens the proxy
		// died on start (e.g. a bad flag) — fail closed instead of leaking traffic.
		if !waitPortListening(bindAddr, time.Second) {
			return fmt.Errorf("wireproxy did not bind %s (proxy died on start)", bindAddr)
		}
		pac := routing.BuildPAC([]string{"*.anthropic.com", "*.claude.ai", "*.claude.com"}, bindAddr)
		if err := routing.Enable(pacFile, pac); err != nil {
			return err
		}
		pacEnabled = true
	}

	// claude.Run calls EnsureLayout itself, so no need to place the binary here.
	_, err := claude.Run(claude.LaunchOpts{Layout: l, UseProxy: useVPN, ProxyURL: proxyURL, WorkDir: workDir})
	return err
}

// waitPortListening polls until something listens on addr, up to timeout.
func waitPortListening(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if tunnel.PortInUse(addr) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return tunnel.PortInUse(addr)
}
