package buildinfo

// Version is injected at build time via -ldflags "-X ...Version=<tag>".
var Version = "dev"

const Repo = "UberMorgott/ClaudeCodePortable"
