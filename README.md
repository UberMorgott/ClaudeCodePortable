# ClaudeCodePortable

Portable **Claude Code CLI** on a USB stick that can tunnel **only itself** through
your AmneziaWG server. No install, no admin, no driver on the host. The whole thing
is a single Go binary — **`ccp.exe`** — that launches Claude, optionally brings up the
VPN, and keeps the bundled components up to date.

> **v2 rewrite (in progress).** The launcher/updater/tunnel layer is now one Go
> binary (`ccp.exe`) instead of the old `cmd` + PowerShell 7 + Windows Terminal
> stack. Native `claude.exe` needs no Node, so Node, bundled pwsh, and Windows
> Terminal are gone (~-320 MB). See `docs/superpowers/specs/` and
> `docs/superpowers/plans/` for the design and build plan.

## What it does

- **`ccp.exe`** — double-click it. A terminal menu opens showing each component's
  current vs. latest version. From there you can:
  - **Launch Claude** — the menu window becomes the Claude session. If **Use VPN**
    is checked, Claude's traffic (and browser Anthropic/Claude domains, via a
    fail-closed PAC) tunnel through AmneziaWG with a kill-switch; unchecked, Claude
    goes direct.
  - **Update** — fetch/upgrade any component (or all) with progress bars.
- On exit the tunnel is torn down (a Windows Job Object guarantees it, even on a
  hard window-close) and the ephemeral `data/_run/` is wiped.

## Layout on the stick

```
<stick>/
├── ccp.exe              # the launcher/updater (the only file you carry)
├── wg-config/           # your Amnezia *.vpn share file (optional — only for VPN)
└── data/                # everything ccp.exe manages (git-ignored, per-stick):
    ├── bin/             #   claude.exe, rtk.exe, wireproxy-awg.exe, statusline
    ├── claude-cfg/      #   settings.json, CLAUDE.md, auth (.credentials.json, .claude.json)
    ├── home/            #   pinned $HOME (.local\bin\claude.exe + versions\<ver>)
    └── _run/            #   decoded key + PAC, wiped every launch
```

## Components (fetched latest by `ccp.exe`)

| Component | Source |
|-----------|--------|
| `claude.exe` | `downloads.claude.ai` (native binary, SHA-256 verified from `manifest.json`) |
| `rtk.exe` | github `rtk-ai/rtk` (Rust Token Killer — trims noisy command I/O via a hook) |
| `wireproxy-awg.exe` | github `artem-russkikh/wireproxy-awg` (AmneziaWG userspace proxy) |
| statusline | github `UberMorgott/MorgottStatusLine` |

`ccp.exe` itself self-updates from this repo's GitHub Releases.

## VPN (optional)

- Provide it only if you want the tunnel: in the Amnezia app → your connection →
  **Share** → save the `vpn://…` file into `wg-config\` (any name ending `.vpn`).
  Swap that file to change server.
- **Kill-switch (fail-closed):** with **Use VPN** on, `claude.exe` is pointed at the
  local proxy `127.0.0.1:25345` via `HTTPS_PROXY` (that process only). If AmneziaWG
  is down, Claude's requests fail — nothing leaks. Other commands in the terminal
  go out directly.
- **Browser auth split-tunnel:** while VPN is on, a fail-closed PAC routes
  `*.anthropic.com` / `*.claude.ai` / `*.claude.com` through the same proxy, so a
  browser re-login exits the same IP as the app. It's removed on exit and
  self-healed on the next launch after a hard kill; pulling the stick neutralizes it.
- No network adapter, driver, or service — wireproxy is userspace (that's why it
  needs no admin).

## Config isolation

- `CLAUDE_CONFIG_DIR` → `data\claude-cfg\`; `HOME` and the per-user dirs are pinned
  into `data\home\` — Claude and its child tools read/write the stick, never the
  host profile. Host `ANTHROPIC_*` / `CLAUDE_CODE_*` env is scrubbed.
- Claude's PowerShell tool and the `rtk` hook run under the host's Windows
  PowerShell 5.1 (present on every Windows) — there is no bundled pwsh.

## Build

- `go build -trimpath -ldflags '-s -w' ./cmd/ccp` (Windows/amd64). Releases are
  stripped + UPX-packed by GitHub Actions on a `v*` tag, with a `checksums.txt`.
- Requirements: Windows 10 1809+.

## Status / remaining work

- **Config seeding:** `data\claude-cfg\` (settings.json, CLAUDE.md) is not yet
  auto-seeded onto a blank stick by `ccp.exe` — the tracked template lives at the
  repo-root `claude-cfg\`. Wiring `ccp.exe` to embed/seed it (auth still comes from
  a first-run login) is the open item before the "single file on a blank stick"
  flow is complete.
- **End-to-end validation** on real hardware (a physical stick, a real `.vpn`, an
  interactive Claude/browser session) is pending.
