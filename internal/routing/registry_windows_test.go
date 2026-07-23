package routing

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// setupThrowaway redirects the package at a disposable HKCU subkey so the test
// never touches the developer's real Internet Settings, and stubs the WinINET
// refresh to a no-op. It creates the throwaway key and registers full cleanup.
func setupThrowaway(t *testing.T) {
	t.Helper()
	const testPath = `Software\ccp-selftest`

	origPath, origRefresh := internetSettingsPath, refresh
	internetSettingsPath = testPath
	refresh = func() error { return nil }
	t.Cleanup(func() {
		internetSettingsPath = origPath
		refresh = origRefresh
		registry.DeleteKey(registry.CURRENT_USER, testPath)
	})

	// Real Internet Settings always exists; the throwaway must be created.
	k, _, err := registry.CreateKey(registry.CURRENT_USER, testPath, registry.ALL_ACCESS)
	if err != nil {
		t.Fatalf("create throwaway key: %v", err)
	}
	k.Close()
}

func readAutoConfigURL(t *testing.T) (string, error) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("open throwaway key: %v", err)
	}
	defer k.Close()
	v, _, err := k.GetStringValue(autoConfigURL)
	return v, err
}

func TestEnableDisableRoundTrip(t *testing.T) {
	setupThrowaway(t)

	pacFile := filepath.Join(t.TempDir(), "ccp.pac")
	body := BuildPAC([]string{"*.anthropic.com"}, "127.0.0.1:25345")

	if err := Enable(pacFile, body); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	// PAC file written.
	if _, err := os.Stat(pacFile); err != nil {
		t.Fatalf("pac not written: %v", err)
	}
	// AutoConfigURL points at our file.
	got, err := readAutoConfigURL(t)
	if err != nil {
		t.Fatalf("read AutoConfigURL: %v", err)
	}
	if want := "file:///" + filepath.ToSlash(pacFile); got != want {
		t.Fatalf("AutoConfigURL = %q, want %q", got, want)
	}

	// Disable removes it.
	if err := Disable(pacFile); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if _, err := readAutoConfigURL(t); err != registry.ErrNotExist {
		t.Fatalf("AutoConfigURL still present after Disable: err=%v", err)
	}
}

func TestSelfHealClearsStale(t *testing.T) {
	setupThrowaway(t)

	pacFile := filepath.Join(t.TempDir(), "ccp.pac")
	body := BuildPAC([]string{"*.anthropic.com"}, "127.0.0.1:25345")

	// Simulate a hard kill: Enable ran, Disable never did. PAC file remains,
	// so SelfHeal recognizes it by Marker in the body.
	if err := Enable(pacFile, body); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := SelfHeal(); err != nil {
		t.Fatalf("SelfHeal: %v", err)
	}
	if _, err := readAutoConfigURL(t); err != registry.ErrNotExist {
		t.Fatalf("AutoConfigURL still present after SelfHeal: err=%v", err)
	}
}

func TestForeignConfigUntouched(t *testing.T) {
	setupThrowaway(t)

	// A proxy config we did not write must never be removed.
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("open key: %v", err)
	}
	const foreign = "file:///C:/Users/someone/corp-proxy.pac"
	if err := k.SetStringValue(autoConfigURL, foreign); err != nil {
		t.Fatalf("set foreign: %v", err)
	}
	k.Close()

	if err := Disable(filepath.Join(t.TempDir(), "ccp.pac")); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	got, err := readAutoConfigURL(t)
	if err != nil {
		t.Fatalf("foreign AutoConfigURL was removed: err=%v", err)
	}
	if got != foreign {
		t.Fatalf("foreign AutoConfigURL mutated: got %q", got)
	}
}
