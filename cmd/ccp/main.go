package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/UberMorgott/ClaudeCodePortable/internal/buildinfo"
	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
	"github.com/UberMorgott/ClaudeCodePortable/internal/statusline"
	"github.com/UberMorgott/ClaudeCodePortable/internal/tui"
)

func main() {
	// `ccp statusline` is claude's statusLine command: read status JSON from
	// stdin, print line 1. Must be fast and side-effect-free (claude calls it
	// often) — no paths.Resolve/tui.Run/EnsureLayout here.
	if len(os.Args) > 1 && os.Args[1] == "statusline" {
		in, _ := io.ReadAll(os.Stdin)
		cols, _ := strconv.Atoi(os.Getenv("COLUMNS")) // empty/invalid → 0 (Render defaults)
		cwd, _ := os.Getwd()
		home := os.Getenv("USERPROFILE")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		fmt.Print(statusline.Render(in, cols, statusline.LoadConfig(cwd, home)))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("ccp", buildinfo.Version)
		return
	}
	// Optional positional arg = project dir for claude's cwd. Empty → inherit.
	workDir := ""
	if len(os.Args) > 1 {
		abs, err := filepath.Abs(os.Args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "ccp: bad project dir:", err)
			os.Exit(1)
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			fmt.Fprintf(os.Stderr, "ccp: not a directory: %s\n", abs)
			os.Exit(1)
		}
		workDir = abs
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccp: resolve executable:", err)
		os.Exit(1)
	}
	if err := tui.Run(paths.Resolve(exe), workDir); err != nil {
		fmt.Fprintln(os.Stderr, "ccp:", err)
		os.Exit(1)
	}
}
