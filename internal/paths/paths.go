package paths

import (
	"os"
	"path/filepath"
)

type Layout struct {
	Root, WGConfig, Data, Bin, ClaudeCfg, Home, Run string
}

func Resolve(exePath string) Layout {
	root := filepath.Dir(exePath)
	data := filepath.Join(root, "data")
	return Layout{
		Root:      root,
		WGConfig:  filepath.Join(root, "wg-config"),
		Data:      data,
		Bin:       filepath.Join(data, "bin"),
		ClaudeCfg: filepath.Join(data, "claude-cfg"),
		Home:      filepath.Join(data, "home"),
		Run:       filepath.Join(data, "_run"),
	}
}

func (l Layout) EnsureRuntimeDirs() error {
	for _, d := range []string{l.WGConfig, l.Data, l.Bin, l.ClaudeCfg, l.Home, l.Run} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (l Layout) BinPath(name string) string { return filepath.Join(l.Bin, name) }
