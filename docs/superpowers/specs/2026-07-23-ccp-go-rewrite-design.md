# ClaudeCodePortable v2 — Go single-binary rewrite

- **Date:** 2026-07-23
- **Status:** approved design, ready for implementation plan
- **Repo:** `github.com/UberMorgott/ClaudeCodePortable` (branch `main`)

## Goal

- Collapse the whole cmd + pwsh + Windows-Terminal launcher/updater/tunnel layer into **one Go binary `ccp.exe`**.
- Native `claude.exe` no longer needs Node → drop the bundled Node toolchain.
- Drop bundled pwsh 7 (~150 MB) and Windows Terminal (~100 MB) → **≈ −320 MB** on the stick.
- Kill the recurring class of cmd/pwsh plumbing bugs (stdout-pipe hang, session-lock, OEM-codepage mojibake on Cyrillic paths, orphaned wireproxy) — Go handles UTF-8 natively and uses a Job Object for lifecycle.

## Non-goals

- No embedding of the WireGuard/AmneziaWG userspace into `ccp.exe`. `wireproxy-awg.exe` stays a **separate bundled binary** that `ccp.exe` decodes-for and spawns.
- No MCP servers, no skills, no plugins on the stick.
- No memory auto-injection, no anti-stuck detector (the two pwsh SessionStart/PreToolUse hooks are dropped).

## Target stick structure

```
STICK/
├── ccp.exe                 # Go: launch + update TUI. The ONLY file on a blank stick.
├── wg-config/              # Amnezia *.vpn share file(s). Swap file = swap server.
└── data/
    ├── bin/                # fetched by `ccp.exe` update:
    │   ├── claude.exe          #   native, downloads.claude.ai (+ manifest checksum)
    │   ├── rtk.exe             #   github.com/rtk-ai/rtk releases
    │   ├── wireproxy-awg.exe   #   github.com/artem-russkikh/wireproxy-awg releases
    │   └── <statusline>        #   github.com/UberMorgott/MorgottStatusLine
    ├── claude-cfg/         # CLAUDE_CONFIG_DIR — stripped settings.json, CLAUDE.md,
    │   │                   #   .credentials.json, .claude.json
    │   └── hooks/          # (empty unless rtk needs a wrapper; memory/stuck removed)
    ├── home/              # pinned $HOME: .local\bin\claude.exe + .local\share\claude\versions\<ver>
    └── _run/              # ephemeral: decoded WG key + proxy.conf. Wiped every launch.
```

- Root holds exactly three entries: `ccp.exe`, `wg-config/`, `data/`.

## `ccp.exe` architecture

Go 1.26, module layout mirrors `MorgTweaker` / `morgward` (`cmd/` + `internal/`). Reference projects: **MorgTweaker** (portable single-binary Windows, Bubble Tea v2, data-driven — closest shape), **morgward** (bubbletea v2 + go-selfupdate + release pipeline), **morgue** (one-binary Go+Bubbletea TUI).

```
cmd/ccp/main.go
internal/
  tui/       # bubbletea v2 model: version header + reactive menu (see TUI spec)
  vpn/       # decode vpn:// -> wg.conf + proxy.conf
  tunnel/    # spawn wireproxy-awg.exe, Job Object kill-on-close, port guard
  claude/    # self-managing-launcher layout, env, exec (console handoff)
  update/    # component version-check + download (progress) + staged swap
  version/   # per-component current/latest resolution
```

### Dependencies (all proven in morgward/MorgTweaker; no new stack)

- `charm.land/bubbletea/v2`, `charm.land/bubbles/v2` (spinner, progress), `charm.land/lipgloss/v2` — TUI.
- `github.com/creativeprojects/go-selfupdate` — `ccp.exe` self-update + GitHub release version resolution.
- `golang.org/x/sys/windows` — Job Object (`CreateJobObject` / `SetInformationJobObject` `KILL_ON_JOB_CLOSE` / `AssignProcessToJobObject`), process control.
- **stdlib only** for the rest: `encoding/base64`, `compress/zlib`, `encoding/json` (vpn decode); `net/http`, `archive/zip`, `archive/tar`, `compress/gzip` (downloads/extract); `crypto/sha256` (checksums).

## VPN decode (replaces `shell/decode-vpn.ps1`)

- Read `wg-config/*.vpn` (deterministic: sorted, first; warn if >1). Strip `vpn://` prefix.
- base64url → standard base64 (`-`→`+`, `_`→`/`, pad to %4).
- Qt `qCompress` framing: first 4 bytes = big-endian uncompressed size (skip), rest = **zlib stream** → `compress/zlib` (Go reads the 2-byte zlib header directly — no manual header-skip hack the pwsh 5.1 path would need).
- Parse JSON → find container with `awg` → `awg.last_config` (nested JSON) → `.config` text.
- Substitute Amnezia DNS placeholders `$PRIMARY_DNS`/`$SECONDARY_DNS` with `obj.dns1`/`dns2` (fallback `1.1.1.1`/`1.0.0.1`) — wireproxy treats leading `$` as env-ref.
- Write `_run/awg.conf` (UTF-8, no BOM). Emit `_run/proxy.conf`:
  ```
  WGConfig = <fullpath-forward-slashes>/awg.conf

  [http]
  BindAddress = 127.0.0.1:25345
  ```
- Go writes UTF-8 natively → the Cyrillic-path mojibake bug the pwsh comment documents cannot occur.

## Launch flow

Steps 1–4 (tunnel) run **only when the `Use VPN` checkbox is on**. With it off, skip straight to step 5 and omit the proxy env in step 6 — claude runs with no tunnel, traffic direct.

1. **Self-heal / single-instance:** read `_run/wireproxy.pid` if present, kill that PID (scoped to this stick). **Remove any stale `AutoConfigURL` bearing `ccp`'s signature** (left by a prior hard kill). Wipe `_run/`, recreate. Port guard: fail clean if `127.0.0.1:25345` already LISTENING (foreign holder).
2. **Decode** `wg-config/*.vpn` → `_run/` (see above).
3. **Validate** `wireproxy-awg.exe -n -c _run/proxy.conf`; fail clean on reject.
4. **Start tunnel:** spawn `wireproxy-awg.exe -c _run/proxy.conf` detached, no window. Write its PID to `_run/wireproxy.pid`. **Assign to a Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`**, handle held open by `ccp.exe` → wireproxy dies on ANY `ccp.exe` death (graceful exit OR hard window-close), replacing the fragile orphan/self-heal + PowerShell.Exiting logic.
4b. **Browser split-tunnel:** write `_run/proxy.pac` (fail-closed, configurable Anthropic/Claude domain list) and set `HKCU …\AutoConfigURL` to its `file://` path (see Tunnel routing). Removed on graceful exit; self-healed on next launch after a hard kill.
5. **Claude self-managing-launcher layout (critical, non-obvious):** `claude.exe` expects to live at `$HOME\.local\bin\claude.exe` with a matching copy under `$HOME\.local\share\claude\versions\<ver>`. Run elsewhere it warns "run claude install" and **re-execs a worker that loses `CLAUDE_CONFIG_DIR`** → hooks resolve against `$HOME\.claude` instead of the stick. `ccp.exe` replicates the layout by hand into `data/home/.local` (never `claude install`, which writes HOST registry + PATH).
6. **Env for the claude child only:**
   - `HOME` = `data/home` (pins every per-user dir to the stick).
   - `CLAUDE_CONFIG_DIR` = `data/claude-cfg`; `CCP_HOOKS` = `data/claude-cfg/hooks`.
   - `PATH` prepend `data/bin` (so `claude`, `rtk`, `wireproxy-awg`, statusline resolve stick-first).
   - `HTTPS_PROXY` / `HTTP_PROXY` = `http://127.0.0.1:25345` **(only when `Use VPN` on)** — **kill-switch: only claude's traffic tunnels**; VPN down → requests fail, never leak. Everything else typed in the terminal goes direct. VPN off → these are not set, claude goes direct.
   - Scrub host `ANTHROPIC_*` / `CLAUDE_CODE_*`.
7. **Console handoff:** launch `claude.exe` with stdin/stdout/stderr = `ccp.exe`'s console (Go `os/exec`, inherit std handles). claude's native TUI draws in the **same window** — the launcher window becomes the claude window (same seamless handoff pwsh→claude does today). `ccp.exe` waits.
8. **Exit:** claude exits (or window hard-closed) → Job Object kills wireproxy → `_run/` wiped → PAC / `AutoConfigURL` removed (see tunnel routing).

## Tunnel routing (split-tunnel, driverless)

`wireproxy-awg` is a **userspace HTTP/SOCKS proxy on `127.0.0.1:25345`, not a TUN network adapter** — that is exactly why the build is portable (no driver, no admin). "Split-tunnel like a VPN client" therefore has two driverless mechanisms, applied only when `Use VPN` is on:

- **Per-application** — set `HTTPS_PROXY`/`HTTP_PROXY` on the process `ccp` launches (claude.exe). Env-only → **fail-closed** (proxy down → request fails, never leaks), zero host trace, dies with the process.
- **Per-domain (browser / host processes `ccp` does not launch)** — a **PAC** file routes a configurable domain list through `127.0.0.1:25345`, everything else DIRECT. Covers browser login / re-auth so it exits the **same IP as the CLI** (Anthropic won't flag an IP change).

**True kernel per-IP/per-app split is out of scope** — it needs a TUN adapter + WFP filtering + admin + a driver, which kills portability and leave-no-trace.

### PAC details

- File: `data/_run/proxy.pac`, referenced via `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings\AutoConfigURL = file:///…/data/_run/proxy.pac` (per-user, no admin). Set on launch when VPN on; removed on graceful exit.
- Default tunneled domains (configurable list): `*.anthropic.com` (api, console, statsig, oauth), `*.claude.ai`, `*.claude.com`. Everything else → DIRECT.
- **Fail-closed:** `FindProxyForURL` returns `"PROXY 127.0.0.1:25345"` with **no `DIRECT` fallback**. Tunnel dead → those domains are unreachable in the browser (never leak from the real IP), consistent with the CLI kill-switch.

### Robustness on hard kill (X button / Task Manager / crash / power loss)

The Job Object handles wireproxy (kernel object on a handle); the registry value is **not** handle-bound, so a hard kill skips cleanup and leaves `AutoConfigURL` set. Neutralized three ways:

1. **Self-heal:** every `ccp` launch first removes any `AutoConfigURL` bearing its signature (stale from a prior crash) — see launch step 1.
2. **Stick removal:** the PAC is `file://` on the stick → unplug → PAC unretrievable → WinINET falls back to a direct connection (validate exact WinINET behavior in implementation).
3. **CLI leaves zero trace** regardless (env-only).

Residue after a hard kill with the stick still inserted and no relaunch: one dangling, inert HKCU string; gone on the next launch or unplug.

## TUI spec (reactive, mouse + keyboard)

- **Version header** (table): per component `Current` vs `Found`, status glyph (`✓` up to date / `↑` update available / spinner while downloading / bar while downloading). Components: `claude`, `rtk`, `wireproxy-awg`, `statusline`, and **`ccp` itself**.
- **Background check:** on open, a goroutine resolves each `Found` (latest) concurrently → streams results into the model → header updates live (no blocking the menu).
- **Actions:**
  - `Launch Claude` — **active/highlighted by default**. Goes **grey / non-clickable while any update is in flight**.
  - **`Use VPN` checkbox** — to the **right of the Launch button**. Toggles whether the launch tunnels claude through AmneziaWG. Default **on** (matches current always-tunnel behavior); state persisted between runs (small field in a stick-local state file).
    - On **check**: validate `wg-config/*.vpn` exists AND decodes (base64/zlib/JSON → an `awg` container). If missing or undecodable → show a message ("VPN config not found. In the Amnezia app: your connection → Share → save the `vpn://…` file into `wg-config\` — any name ending `.vpn`. Swap that file to change server.") and **auto-uncheck** the box.
    - When **off**: launch runs claude with **no tunnel, no proxy env** — traffic goes direct (user's explicit choice; the kill-switch below applies only when on).
  - `Update all` (bottom) — **active only when ≥1 component has an update** (including `ccp`); grey otherwise.
  - **Per-component update:** mouse-click a component row that has an update → starts just that download; spinner then progress bar appears to the **right** of that row.
- **Reactive download:** each download runs in a goroutine emitting progress messages → bubbletea re-renders that component's bar. On completion → status flips to green `✓` (`Current` now == `Found`) → recompute "any updates left?" → `Update all` greys out when everything is green.
- **Missing component** (not on stick) → shown as needing download; same progress UI.
- Mouse hit-zones via lipgloss-rendered row bounds; `bubbles` spinner + progress components.

## Component version sources

| Component | Current | Latest / download |
|---|---|---|
| claude | `claude.exe --version` → `2.1.190 (Claude Code)` | `downloads.claude.ai/claude-code-releases` manifest → `<ver>/win32-x64/claude.exe` + manifest sha256 verify |
| rtk | `rtk.exe --version` → `rtk X.Y.Z` | `api.github.com/repos/rtk-ai/rtk/releases/latest` |
| wireproxy-awg | `wireproxy.exe --version` | `github.com/artem-russkikh/wireproxy-awg` latest, asset `wireproxy_windows_amd64.tar.gz` |
| statusline | tag file / `--version` | `github.com/UberMorgott/MorgottStatusLine` (release or `main`; see risk) |
| ccp | build-time version | `github.com/UberMorgott/ClaudeCodePortable` releases via go-selfupdate |

- Downloads show progress bars. `claude.exe` swap is staged (download to temp, verify checksum, atomic replace) — never overwrite a live/broken exe in place.

## `settings.json` after strip

- **Keep:** `env` (`CLAUDE_CODE_USE_POWERSHELL_TOOL=1`, `DISABLE_AUTOUPDATER=1`, `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`), `permissions.defaultMode=bypassPermissions`, `skipDangerousModePermissionPrompt`, `defaultShell=powershell`, `language=russian`, `teammateMode`, `voiceEnabled`, `statusLine`, and the **rtk hook** (PreToolUse `Bash|PowerShell` → `rtk hook claude`, `shell:powershell` → runs under host Windows PowerShell 5.1).
- **Remove:** `enabledPlugins`, `extraKnownMarketplaces`, the `load-memory.ps1` (SessionStart) and `detect-stuck.ps1` (UserPromptSubmit/PreToolUse/PostToolUse/PostToolUseFailure) hook blocks.
- `mcp-servers.json`, `mcp-secrets*.ps1` removed.

## Deleted from repo/stick

- Binaries/dirs: `node/`, `pwsh/`, `wt/`, old `wireproxy/` layout is renamed target `data/bin/`.
- Scripts: all `*.bat`, `*.cmd` (`Start.bat`, `Stop.bat`, `Install or Update.bat`, `bootstrap.cmd`), all `shell/*.ps1` (`decode-vpn`, `profile`, `start-tunnel`, `sync-files`, `update`), `claude-cfg/hooks/*.ps1`.
- Config trees: `claude-cfg/skills/`, `claude-cfg/memory/`, `claude-cfg/plugins/`, `claude-cfg/mcp-servers.json`.
- `CLAUDE.md` "Portable run context" section rewritten: no bundled node/pwsh; toolchain is `ccp.exe` + host PS 5.1 for the rtk hook + PowerShell tool.

## Build & distribution

- Windows-only target: `GOOS=windows GOARCH=amd64`.
- `go build -trimpath -ldflags '-s -w'` (strip symbols/debug), then **UPX** compress the resulting exe. Order: build → strip → `upx --best` → sha256 → `checksums.txt`.
- `checksums.txt` in sha256sum format go-selfupdate's ChecksumValidator parses: `<lowercase-hex-sha256>␠␠<filename>` — checksum computed on the **UPX-packed** binary.
- GH Actions on tag `v*` → build → UPX → publish `ccp.exe` + `checksums.txt` to Releases.
- Blank-stick bootstrap: download `ccp.exe` from the Releases page (one file) → run → `Update` fetches `data/bin/*`.

## Performance (portable / slow-stick)

- **No RAM-disk, no copy-to-RAM.** USB sticks are slow on **random small-file I/O**, not sequential bulk. The rewrite already removes the worst offenders — `node/` (~10k tiny files), `pwsh/` (~10k), skills — leaving ~4 large binaries + a few config files (sequential-friendly). That deletion IS the main startup win.
- **OS file cache gives "run from RAM" for free.** Windows standby cache holds read files in RAM; after first launch `claude.exe` + config are served from memory. A manual RAM disk only duplicates this and needs a driver + admin → breaks portability and leave-no-trace. Rejected.
- **Deferred (only if measured slow):** optional copy of `data/bin/*` to a host `%TEMP%` (SSD ≫ USB) with cleanup on exit. Marginal gain over OS cache; add only if a real slow-stick measurement justifies it.
- **Self-managing layout copies:** claude.exe exists 2–3× on the stick (`data/bin` + `home/.local/bin` + `versions/<ver>`) — cost paid once per update, not per launch. On an NTFS-formatted stick these can be hardlinks; FAT32/exFAT must copy.

## Open risks / implementation validation

- **statusline is a Shell (bash) script.** Claude runs `statusLine.command` via a shell; the stick has no git-bash. Validate in implementation: does `MorgottStatusLine` have a non-bash/native path, or must it run through the host shell? May need repackaging or a small Go statusline shim. Not a design blocker.
- **UPX + AV false positives.** Packed exes trip antivirus heuristics; the portable runs on other people's machines. Ship UPX per request, but keep an unpacked fallback artifact in Releases if AV flags surface.
- **UPX + go-selfupdate:** self-update replaces the packed exe; ensure the new download is the packed artifact whose sha matches `checksums.txt`. Windows in-place self-replace uses the rename-old-then-move pattern (exe can't delete itself while running).
- **Host PS 5.1 for the rtk hook / PowerShell tool.** `rtk hook claude` just invokes `rtk.exe` → 5.1 fine. Confirm Claude's PowerShell tool binds to whatever `powershell`/`pwsh` is on PATH (host 5.1 present on every Windows).
- **claude native re-exec / CLAUDE_CONFIG_DIR loss** — must verify the `data/home/.local` layout fully suppresses the "run claude install" re-exec on the current native `claude.exe`.
- **PAC / WinINET specifics:** after writing/removing `AutoConfigURL`, notify WinINET via `InternetSetOption(INTERNET_OPTION_SETTINGS_CHANGED)` + `INTERNET_OPTION_REFRESH` so Edge/Chrome pick it up without restart. **Firefox does not read WinINET** by default (own proxy store) → its login won't be tunneled unless set to "use system proxy"; document as a limitation. Validate WinINET's exact fallback when the `file://` PAC is missing (assumed → direct).

## Reference material (user's own repos)

- `UberMorgott/MorgTweaker` — portable single-binary Windows + Bubble Tea v2 + data-driven engine (primary structural template).
- `UberMorgott/morgward` — bubbletea v2 + go-selfupdate + `-trimpath -ldflags '-s -w'` release recipe + checksums.txt contract.
- `UberMorgott/morgue` — one-binary Go+Bubbletea TUI.
- `UberMorgott/install-winget` — robust install on oldest Win10 (fetch robustness reference).
