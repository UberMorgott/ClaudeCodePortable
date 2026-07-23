package routing

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// internetSettingsPath is the HKCU subkey that holds the per-user proxy config.
// It is a package-level var ONLY so tests can redirect to a throwaway key and
// never mutate the developer's real proxy settings. Production keeps the default.
var internetSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// refresh notifies WinINET that proxy settings changed. Overridable to a no-op
// in tests (a throwaway key needs no system-wide WinINET refresh).
var refresh = winInetRefresh

const autoConfigURL = "AutoConfigURL"

// Enable writes the PAC file, sets HKCU AutoConfigURL=file:///<pacFile>, and
// refreshes WinINET so open browsers pick up the change.
func Enable(pacFile, pacBody string) error {
	if err := os.WriteFile(pacFile, []byte(pacBody), 0o644); err != nil {
		return err
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetStringValue(autoConfigURL, "file:///"+filepath.ToSlash(pacFile)); err != nil {
		return err
	}
	return refresh()
}

// Disable removes AutoConfigURL iff it points at our PAC, then refreshes WinINET.
func Disable(pacFile string) error { return removeIfOurs(pacFile) }

// SelfHeal removes a stale ccp AutoConfigURL left behind by a prior hard kill.
func SelfHeal() error { return removeIfOurs("") }

// removeIfOurs deletes AutoConfigURL only when it is a file:// PAC that either
// equals pacFile (when given) or whose body contains Marker. A foreign proxy
// config is never touched.
func removeIfOurs(pacFile string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer k.Close()

	val, _, err := k.GetStringValue(autoConfigURL)
	if errors.Is(err, registry.ErrNotExist) {
		return nil // nothing set
	}
	if err != nil {
		return err
	}
	if !isOurs(val, pacFile) {
		return nil // foreign config — never touch
	}
	if err := k.DeleteValue(autoConfigURL); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return refresh()
}

// isOurs reports whether a file:// AutoConfigURL is one we wrote: it matches
// pacFile (when provided) or its PAC body contains Marker.
func isOurs(autoURL, pacFile string) bool {
	if !strings.HasPrefix(strings.ToLower(autoURL), "file:") {
		return false
	}
	path := strings.TrimPrefix(autoURL, "file:///")
	if path == autoURL {
		path = strings.TrimPrefix(autoURL, "file://")
	}
	if pacFile != "" && strings.EqualFold(filepath.ToSlash(path), filepath.ToSlash(pacFile)) {
		return true
	}
	body, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		return false
	}
	return strings.Contains(string(body), Marker)
}

var (
	wininet               = windows.NewLazyDLL("wininet.dll")
	procInternetSetOption = wininet.NewProc("InternetSetOptionW")
)

const (
	internetOptionRefresh         = 37 // INTERNET_OPTION_REFRESH
	internetOptionSettingsChanged = 39 // INTERNET_OPTION_SETTINGS_CHANGED
)

// winInetRefresh tells WinINET to reload proxy settings. The return value is
// advisory (best-effort UI refresh), so a failure here is not fatal.
func winInetRefresh() error {
	procInternetSetOption.Call(0, internetOptionSettingsChanged, 0, 0)
	procInternetSetOption.Call(0, internetOptionRefresh, 0, 0)
	return nil
}
