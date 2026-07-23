package paths

import (
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	l := Resolve(filepath.FromSlash("X:/stick/ccp.exe"))
	want := map[string]string{
		"root": filepath.FromSlash("X:/stick"),
		"wg":   filepath.FromSlash("X:/stick/wg-config"),
		"data": filepath.FromSlash("X:/stick/data"),
		"bin":  filepath.FromSlash("X:/stick/data/bin"),
		"cfg":  filepath.FromSlash("X:/stick/data/claude-cfg"),
		"home": filepath.FromSlash("X:/stick/data/home"),
		"run":  filepath.FromSlash("X:/stick/data/_run"),
	}
	if l.Root != want["root"] || l.WGConfig != want["wg"] || l.Bin != want["bin"] ||
		l.ClaudeCfg != want["cfg"] || l.Home != want["home"] || l.Run != want["run"] {
		t.Fatalf("got %+v", l)
	}
}
