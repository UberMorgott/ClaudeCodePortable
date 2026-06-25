# ClaudeCodePortable

Portable **Claude Code CLI** that tunnels **only itself** through your AmneziaWG
(wg2.0) server. No install, no admin, no driver on the host. One click sets the
whole thing up on a USB stick; then plug in, run `Start.bat`, work in Windows
Terminal.

## How it works (Install / Start / Stop)
- **`Install or Update.bat`** — one-click **install AND update**. Downloads every heavy
  component onto the stick (PowerShell 7, Node, Windows Terminal, wireproxy,
  `claude.exe`) and lays down the config skeleton. Re-run any time to update
  (skips what's current, upgrades what's stale). Pure `cmd` + `curl` + `tar`
  (built into Win10 1803+) — needs **no** system PowerShell, so a restricted host
  execution policy can't block it. (Internally it fetches `bootstrap.cmd` fresh from
  GitHub and runs it from `%TEMP%`; bootstrap pulls bundled pwsh 7 + the repo
  skeleton, then runs `shell\update.ps1`. `bootstrap.cmd` is an internal helper — it
  is **not** copied to the stick root.)
- **`Start.bat`** — daily launcher: brings up the VPN proxy, opens Windows
  Terminal → portable pwsh → `claude` (tunnelled, kill-switch on).
- **`Stop.bat`** — kills the AmneziaWG proxy and wipes the ephemeral `_run\` dir.

## First-time setup
1. Copy this repo onto your stick (or just `Install or Update.bat` — it fetches the
   rest), into the folder you want to be the portable root.
2. Provide the two **individual** things (never shipped in this repo):
   - **VPN:** in the Amnezia app → your connection → **Share** → save the
     `vpn://...` file into `Amnezia config\` (any name ending `.vpn`).
   - **Claude login:** done on first `claude` run (it prompts). Auth is written to
     the on-stick `home\` dir (`home\.credentials.json`, `home\.claude*`) — see
     "Config isolation" below. To reuse an existing login, drop your
     `.credentials.json` / `.claude.json` into `home\`.
3. Run **`Install or Update.bat`**. It downloads + verifies everything onto the stick.
   (Needs reachable internet / GitHub. If your host's direct net is censored, the
   updater falls back to your AmneziaWG VPN automatically for the downloads.)
4. Run **`Start.bat`** → Windows Terminal opens → type `claude`.

To update later: run `Install or Update.bat` again. To change VPN server: drop a different
`.vpn` into `Amnezia config\` and restart `Start.bat`.

## What's in this repo vs what gets fetched
| In the repo (this is all you carry) | Fetched by `Install or Update.bat` |
|------|------|
| `Install or Update.bat` | `pwsh/` (PowerShell 7) |
| `Start.bat`, `Stop.bat` | `node/` |
| `shell/` (decode-vpn, profile, update) | `wt/` (Windows Terminal) |
| `claude-cfg/` (settings, hooks, generic memory, skills) | `wireproxy/wireproxy.exe` |
| | `bin/claude.exe` (verified by SHA-256 from the official release manifest) |

You supply: `Amnezia config\*.vpn` + your Claude credentials. Everything else installs
itself.

> **Note:** `bootstrap.cmd` is an internal helper, not an entry point. `Install or
> Update.bat` always fetches the latest copy fresh from GitHub and runs it from
> `%TEMP%`, so it is intentionally **not** present on the stick root.

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
  MCP servers, skills, CLAUDE.md rules — the host's own Claude config is ignored.
- `HOME` / `USERPROFILE` / `APPDATA` / `LOCALAPPDATA` are re-pinned to an on-stick
  `home\` dir (auto-created on launch). So `claude` **and** any child tool it spawns
  (node, git, npm) read/write user data on the stick, never the host profile. In
  particular the auth token and `.claude.json` land in `home\` —
  `home\.credentials.json`, `home\.claude*` — not in `claude-cfg\`.
- The `claude` wrapper scrubs host `ANTHROPIC_*` / `CLAUDE_CODE_*` env for the run.
- **Cannot** be overridden: org **managed policy** at
  `C:\Program Files\ClaudeCode\managed-settings.json` (the shell warns if present).

## Environment variables / defaults
- **`CC_WORKDIR`** — the folder `claude` opens in (its "project root"). **Default:
  the HOST user's home** (captured before `home\` re-pinning), so you work on the
  owner's files, not the stick. Set `CC_WORKDIR` before launching, or just `cd` to
  the folder you're fixing.
- **`CCP_AUTOCLAUDE`** — set to `1` by `Start.bat`; `profile.ps1` then auto-launches
  `claude` at the end of dot-sourcing (it's cleared first so re-sourcing won't
  re-trigger). Without it, you get the shell and type `claude` yourself.
- **Proxy port** — the local AmneziaWG http-proxy is fixed at **`127.0.0.1:25345`**
  (`HTTPS_PROXY`/`HTTP_PROXY` for the `claude` process only).

## Leave no trace
- **Install:** downloads extract in the host `%TEMP%` and are deleted after; the
  binary goes straight to the stick. `claude.exe` is placed by direct manifest
  download (SHA-256 verified) — the official `install` subcommand is **not** run,
  so nothing is written to `%USERPROFILE%\.local` / `.claude` / PATH / registry.
- **Runtime:** the decoded VPN key + proxy config live in an ephemeral `_run\`
  wiped on exit. npm caches are redirected to `%TEMP%` and wiped on exit. pwsh
  history + telemetry off. When idle, the stick has no generated/temp files.
- **No** network adapter, driver, or service (wireproxy is userspace).
- Unavoidable OS-level traces (true for running any exe): Prefetch / AmCache —
  these record that a binary ran, not what it did. Removing them needs admin.

## Requirements / gotchas
- **Windows 10 1809+** (1803+ for `curl`/`tar`; 19041+ for Windows Terminal).
- **Secrets on the stick:** `Amnezia config\*.vpn` (private key) and `home\`
  (auth token, e.g. `home\.credentials.json`) are plaintext. If the stick can be
  lost, put it on an encrypted volume (e.g. VeraCrypt portable).
- **Windows Terminal won't open?** Needs `Microsoft.VCLibs.140`; if missing,
  delete the `wt` folder and `Start.bat` launches portable pwsh directly.
- **Locked corporate hosts** (GPO `AllSigned`/`Restricted`, Constrained Language
  Mode, AppLocker on removable drives) can block unsigned scripts/exes — nothing
  portable can bypass that. `Install or Update.bat` detects it and fails with a clear note.

## Components (always fetched latest)
Claude Code CLI · Node LTS · PowerShell 7 · Windows Terminal ·
wireproxy-awg. `Install or Update.bat` re-run upgrades each in place.
