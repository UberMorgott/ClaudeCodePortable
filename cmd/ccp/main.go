package main

import (
	"fmt"
	"os"

	"github.com/UberMorgott/ClaudeCodePortable/internal/buildinfo"
	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
	"github.com/UberMorgott/ClaudeCodePortable/internal/tui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("ccp", buildinfo.Version)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccp: resolve executable:", err)
		os.Exit(1)
	}
	if err := tui.Run(paths.Resolve(exe)); err != nil {
		fmt.Fprintln(os.Stderr, "ccp:", err)
		os.Exit(1)
	}
}
