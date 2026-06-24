# ClaudeCodePortable

Portable **Claude Code CLI** that tunnels **only itself** through your AmneziaWG
(wg2.0) server. No install, no admin, no driver on the host. One click sets the
whole thing up on a USB stick; then plug in, run `Start.bat`, work in Windows
Terminal.

## How it works (two scripts)
- **`Install.bat`** — one-click **install AND update**. Downloads every heavy
  component onto the stick (PowerShell 7, Node, Go, Windows Terminal, wireproxy,
  `claude.exe`) and lays down the config skeleton. Re-run any time to update
  (skips what's current, upgrades what's stale). Pure `cmd` + `curl` + `tar`
  (built into Win10 1803+) — needs **no** system PowerShell, so a restricted host
  execution policy can't block it.
- **`Start.bat`** — daily launcher: brings up the VPN proxy, opens Windows
  Terminal → portable pwsh → `claude` (tunnelled, kill-switch on).

## First-time setup
1. Copy this repo onto your stick (or just `Install.bat` — it fetches the rest),
   into the folder you want to be the portable root.
2. Provide the two **individual** things (never shipped in this repo):
   - **VPN:** in the Amnezia app → your connection → **Share** → save the
     `vpn://...` file into `Amnezia config\` (any name ending `.vpn`).
   - **Claude login:** done on first `claude` run (it prompts), or place your
     existing `claude-cfg\.credentials.json` / `.claude.json`.
3. Run **`Install.bat`**. It downloads + verifies everything onto the stick.
   (Needs reachable internet / GitHub. If your host's direct net is censored, the
   updater falls back to your AmneziaWG VPN automatically for the downloads.)
4. Run **`Start.bat`** → Windows Terminal opens → type `claude`.

To update later: run `Install.bat` again. To change VPN server: drop a different
`.vpn` into `Amnezia config\` and restart `Start.bat`.

## What's in this repo vs what gets fetched
| In the repo (this is all you carry) | Fetched by `Install.bat` |
|------|------|
| `Install.bat`, `bootstrap.cmd` | `pwsh/` (PowerShell 7) |
| `Start.bat`, `Stop.bat` | `node/`, `go/` |
| `shell/` (decode-vpn, profile, update) | `wt/` (Windows Terminal) |
| `claude-cfg/` (settings, hooks, generic memory, skills) | `wireproxy/wireproxy.exe` |
| | `bin/claude.exe` (verified by SHA-256 from the official release manifest) |

You supply: `Amnezia config\*.vpn` + your Claude credentials. Everything else installs
itself.

## Daily use
- Run `Start.bat` → Windows Terminal opens.
- Type `claude` → runs through AmneziaWG (kill-switch).
- Type anything else (`ping`, `curl`, `git`) → goes out **directly**, no VPN.
- Close the minimized `wireproxy-amnezia` window, or run `Stop.bat`, to drop VPN.

## "Only Claude + kill-switch"
- `claude` is the only process pointed at the proxy (`HTTPS_PROXY`, set only for
  that process). The shell itself has no proxy → other commands go direct.
- Claude has **no** direct route — only the proxy address. If AmneziaWG is down,
  wireproxy can't reach upstream, so Claude's requests just fail. Nothing leaks
  outside the tunnel (fail-closed). DNS also resolves through the tunnel.

## Config isolation (everything from the stick, nothing from the host)
- `CLAUDE_CONFIG_DIR` → `claude-cfg\` on the stick: relocates `settings.json`,
  `.claude.json`, MCP servers, skills, CLAUDE.md rules, and the auth token — the
  host's own Claude config is ignored.
- The `claude` wrapper scrubs host `ANTHROPIC_*` / `CLAUDE_CODE_*` env for the run.
- **Cannot** be overridden: org **managed policy** at
  `C:\Program Files\ClaudeCode\managed-settings.json` (the shell warns if present).

## Leave no trace
- **Install:** downloads extract in the host `%TEMP%` and are deleted after; the
  binary goes straight to the stick. `claude.exe` is placed by direct manifest
  download (SHA-256 verified) — the official `install` subcommand is **not** run,
  so nothing is written to `%USERPROFILE%\.local` / `.claude` / PATH / registry.
- **Runtime:** the decoded VPN key + proxy config live in an ephemeral `_run\`
  wiped on exit. Go/npm caches are redirected to `%TEMP%` and wiped on exit. pwsh
  history + telemetry off. When idle, the stick has no generated/temp files.
- **No** network adapter, driver, or service (wireproxy is userspace).
- Unavoidable OS-level traces (true for running any exe): Prefetch / AmCache —
  these record that a binary ran, not what it did. Removing them needs admin.

## Requirements / gotchas
- **Windows 10 1809+** (1803+ for `curl`/`tar`; 19041+ for Windows Terminal).
- **Secrets on the stick:** `Amnezia config\*.vpn` (private key) and `claude-cfg\`
  (auth token) are plaintext. If the stick can be lost, put it on an encrypted
  volume (e.g. VeraCrypt portable).
- **Windows Terminal won't open?** Needs `Microsoft.VCLibs.140`; if missing,
  delete the `wt` folder and `Start.bat` launches portable pwsh directly.
- **Locked corporate hosts** (GPO `AllSigned`/`Restricted`, Constrained Language
  Mode, AppLocker on removable drives) can block unsigned scripts/exes — nothing
  portable can bypass that. `Install.bat` detects it and fails with a clear note.

## Components (always fetched latest)
Claude Code CLI · Node LTS · Go stable · PowerShell 7 · Windows Terminal ·
wireproxy-awg. `Install.bat` re-run upgrades each in place.
