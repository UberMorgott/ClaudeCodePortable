package main

import (
	"fmt"
	"os"

	"github.com/UberMorgott/ClaudeCodePortable/internal/buildinfo"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("ccp", buildinfo.Version)
		return
	}
	// TUI entrypoint wired in Task 9.
	fmt.Println("ccp", buildinfo.Version, "- TUI not yet wired")
}
