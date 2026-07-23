# ClaudeCodePortable v2 (ccp.exe) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the cmd + pwsh + Windows-Terminal launcher/updater/tunnel layer of ClaudeCodePortable with a single Go binary `ccp.exe` (bubbletea v2 TUI, optional driverless split-tunnel VPN, self-updating component fetcher).

**Architecture:** One Windows-only Go binary. Pure-logic modules (paths, vpn decode, version, download/verify, PAC gen) are unit-tested TDD. Windows-integration modules (tunnel via Job Object, registry PAC, claude self-managing-launcher layout + console handoff) expose narrow interfaces and are integration-verified. A bubbletea v2 TUI wires them: a version header + reactive menu (Launch / Use-VPN checkbox / per-component + all Update). `wireproxy-awg.exe` stays a separate bundled binary; `ccp.exe` decodes the Amnezia `.vpn` and spawns it.

**Tech Stack:** Go 1.26; `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`; `github.com/creativeprojects/go-selfupdate`; `golang.org/x/sys/windows`; stdlib (`net/http`, `compress/zlib`, `compress/gzip`, `archive/zip`, `archive/tar`, `encoding/base64`, `encoding/json`, `crypto/sha256`).

## Global Constraints

- Design source of truth: `docs/superpowers/specs/2026-07-23-ccp-go-rewrite-design.md`. Read it before any task.
- Platform: Windows / amd64 only. Build target `GOOS=windows GOARCH=amd64`.
- Go module path: `github.com/UberMorgott/ClaudeCodePortable` (repo root becomes the Go module). Binary package: `cmd/ccp`.
- Deps limited to the Tech Stack list. No new third-party deps without updating the spec. Everything else stdlib.
- Proxy bind address is fixed: `127.0.0.1:25345`.
- Tunneled domains default list (configurable): `*.anthropic.com`, `*.claude.ai`, `*.claude.com`.
- PAC is **fail-closed**: `PROXY 127.0.0.1:25345` with no `DIRECT` fallback.
- CLI kill-switch is env-only (`HTTPS_PROXY`/`HTTP_PROXY` on the claude child), fail-closed, zero host trace.
- All files ccp writes are UTF-8 **without BOM**; paths passed to wireproxy use forward slashes.
- Comments/commits/code in English. Frequent commits (one per task minimum).
- Release: `go build -trimpath -ldflags '-s -w'` → UPX → sha256 → `checksums.txt` (`<lowercasehex>␠␠<name>`), computed on the UPX-packed exe.
- Stick layout (all runtime paths resolve from the dir containing `ccp.exe`):
  ```
  <root>/ccp.exe
  <root>/wg-config/*.vpn
  <root>/data/{bin,claude-cfg,home,_run}
  ```

---

### Task 0: Project scaffold + build pipeline

**Files:**
- Create: `go.mod`, `cmd/ccp/main.go`, `Makefile`, `build.ps1`, `.github/workflows/release.yml`, `internal/buildinfo/version.go`
- Modify: `.gitignore` (add `ccp.exe`, `/dist/`)

**Interfaces:**
- Produces: `buildinfo.Version string` (ldflags-injected), `buildinfo.Repo = "UberMorgott/ClaudeCodePortable"`.

- [ ] **Step 1: Init module + deps**

Run:
```bash
cd /d/MorgDEV/ClaudeCodePortable
go mod init github.com/UberMorgott/ClaudeCodePortable
go get charm.land/bubbletea/v2@v2.0.7 charm.land/bubbles/v2@v2.1.0 charm.land/lipgloss/v2@v2.0.3 github.com/creativeprojects/go-selfupdate@v1.5.2 golang.org/x/sys@latest
```
Expected: `go.mod` + `go.sum` created with those requires.

- [ ] **Step 2: buildinfo**

`internal/buildinfo/version.go`:
```go
package buildinfo

// Version is injected at build time via -ldflags "-X ...Version=<tag>".
var Version = "dev"

const Repo = "UberMorgott/ClaudeCodePortable"
```

- [ ] **Step 3: main skeleton**

`cmd/ccp/main.go`:
```go
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
```

- [ ] **Step 4: Makefile (strip + UPX + checksums)**

`Makefile`:
```makefile
BINARY := ccp.exe
PKG := ./cmd/ccp
VERSION ?= dev
LDFLAGS := -s -w -X github.com/UberMorgott/ClaudeCodePortable/internal/buildinfo.Version=$(VERSION)

.PHONY: build release vet fmt clean
build:
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) $(PKG)
vet:
	go vet ./...
fmt:
	gofmt -w .
clean:
	rm -rf dist $(BINARY)
release:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BINARY) $(PKG)
	upx --best --lzma dist/$(BINARY)
	cd dist && sha256sum --text $(BINARY) > checksums.txt
```

- [ ] **Step 5: build + run**

Run: `go build -o ccp.exe ./cmd/ccp && ./ccp.exe --version`
Expected: prints `ccp dev`.

- [ ] **Step 6: GH Actions release**

`.github/workflows/release.yml`:
```yaml
name: release
on:
  push:
    tags: ['v*']
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26' }
      - name: Install UPX
        run: sudo apt-get update && sudo apt-get install -y upx-ucl
      - name: Build
        run: make release VERSION=${{ github.ref_name }}
      - name: Release
        uses: softprops/action-gh-release@v2
        with:
          files: |
            dist/ccp.exe
            dist/checksums.txt
```

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum cmd/ccp/main.go internal/buildinfo Makefile .github/workflows/release.yml .gitignore
git commit -m "feat(ccp): project scaffold + strip/UPX release pipeline"
```

---

### Task 1: Stick path resolution (`internal/paths`)

**Files:**
- Create: `internal/paths/paths.go`, `internal/paths/paths_test.go`

**Interfaces:**
- Produces:
  ```go
  type Layout struct {
      Root, WGConfig, Data, Bin, ClaudeCfg, Home, Run string
  }
  func Resolve(exePath string) Layout          // derives all paths from dir(exePath)
  func (l Layout) EnsureRuntimeDirs() error     // mkdir Data,Bin,ClaudeCfg,Home,Run
  func (l Layout) BinPath(name string) string   // filepath.Join(Bin, name)
  ```

- [ ] **Step 1: Failing test**

`internal/paths/paths_test.go`:
```go
package paths

import (
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	l := Resolve(filepath.FromSlash("X:/stick/ccp.exe"))
	want := map[string]string{
		"root":   filepath.FromSlash("X:/stick"),
		"wg":     filepath.FromSlash("X:/stick/wg-config"),
		"data":   filepath.FromSlash("X:/stick/data"),
		"bin":    filepath.FromSlash("X:/stick/data/bin"),
		"cfg":    filepath.FromSlash("X:/stick/data/claude-cfg"),
		"home":   filepath.FromSlash("X:/stick/data/home"),
		"run":    filepath.FromSlash("X:/stick/data/_run"),
	}
	if l.Root != want["root"] || l.WGConfig != want["wg"] || l.Bin != want["bin"] ||
		l.ClaudeCfg != want["cfg"] || l.Home != want["home"] || l.Run != want["run"] {
		t.Fatalf("got %+v", l)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL** (`go test ./internal/paths/` → undefined Resolve)

- [ ] **Step 3: Implement**

`internal/paths/paths.go`:
```go
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
	for _, d := range []string{l.Data, l.Bin, l.ClaudeCfg, l.Home, l.Run} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (l Layout) BinPath(name string) string { return filepath.Join(l.Bin, name) }
```

- [ ] **Step 4: Run test — expect PASS**

- [ ] **Step 5: Commit** — `git commit -am "feat(paths): stick layout resolution"`

---

### Task 2: Amnezia `.vpn` decode (`internal/vpn`)

Port of `shell/decode-vpn.ps1`. Pure logic, fully TDD.

**Files:**
- Create: `internal/vpn/decode.go`, `internal/vpn/decode_test.go`, `internal/vpn/testdata/sample.vpn`

**Interfaces:**
- Produces:
  ```go
  // Decode parses vpn:// share text into a WireGuard .conf body.
  func Decode(vpnText string) (wgConf string, err error)
  // ProxyConf renders the wireproxy config pointing at wgPath (forward slashes).
  func ProxyConf(wgPath, bindAddr string) string
  // WriteRuntime decodes vpnText and writes awg.conf + proxy.conf into runDir,
  // returning the proxy.conf path. bindAddr e.g. "127.0.0.1:25345".
  func WriteRuntime(vpnText, runDir, bindAddr string) (proxyConfPath string, err error)
  ```

- [ ] **Step 1: Build a real test fixture**

Generate `internal/vpn/testdata/sample.vpn` from the qCompress framing (4-byte big-endian length + zlib of a JSON `{"containers":[{"awg":{"last_config":"{\"config\":\"[Interface]\\nAddress=10.0.0.2/32\\n...\"}"}}],"dns1":"1.1.1.1","dns2":"1.0.0.1"}`). Write a throwaway Go generator or reuse the current `Start.bat` on a real `.vpn` to capture expected output. Commit both the `.vpn` and the expected decoded `.conf` as `testdata/expected.conf`.

- [ ] **Step 2: Failing test**

`internal/vpn/decode_test.go`:
```go
package vpn

import (
	"os"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	raw, err := os.ReadFile("testdata/sample.vpn")
	if err != nil { t.Fatal(err) }
	want, err := os.ReadFile("testdata/expected.conf")
	if err != nil { t.Fatal(err) }
	got, err := Decode(string(raw))
	if err != nil { t.Fatal(err) }
	if strings.TrimSpace(got) != strings.TrimSpace(string(want)) {
		t.Fatalf("decode mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestProxyConf(t *testing.T) {
	got := ProxyConf("X:/stick/data/_run/awg.conf", "127.0.0.1:25345")
	if !strings.Contains(got, "WGConfig = X:/stick/data/_run/awg.conf") ||
		!strings.Contains(got, "BindAddress = 127.0.0.1:25345") {
		t.Fatalf("bad proxy conf: %q", got)
	}
}

func TestDNSPlaceholderSubstitution(t *testing.T) {
	// Decoded config must not contain literal $PRIMARY_DNS/$SECONDARY_DNS.
	raw, _ := os.ReadFile("testdata/sample.vpn")
	got, _ := Decode(string(raw))
	if strings.Contains(got, "$PRIMARY_DNS") || strings.Contains(got, "$SECONDARY_DNS") {
		t.Fatalf("DNS placeholders not substituted: %q", got)
	}
}
```

- [ ] **Step 3: Run — expect FAIL**

- [ ] **Step 4: Implement**

`internal/vpn/decode.go`:
```go
package vpn

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type shareFile struct {
	Containers []struct {
		AWG *struct {
			LastConfig string `json:"last_config"`
		} `json:"awg"`
	} `json:"containers"`
	DNS1 string `json:"dns1"`
	DNS2 string `json:"dns2"`
}

func Decode(vpnText string) (string, error) {
	s := strings.TrimSpace(vpnText)
	s = strings.TrimPrefix(s, "vpn://")
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	case 1:
		return "", fmt.Errorf("corrupt base64 in .vpn (len %% 4 == 1)")
	}
	blob, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("base64: %w", err)
	}
	if len(blob) <= 4 {
		return "", fmt.Errorf("vpn payload too short")
	}
	// Qt qCompress: first 4 bytes big-endian size, rest zlib.
	zr, err := zlib.NewReader(bytes.NewReader(blob[4:]))
	if err != nil {
		return "", fmt.Errorf("zlib: %w", err)
	}
	defer zr.Close()
	dec, err := io.ReadAll(zr)
	if err != nil {
		return "", fmt.Errorf("inflate: %w", err)
	}
	var sf shareFile
	if err := json.Unmarshal(dec, &sf); err != nil {
		return "", fmt.Errorf("outer json: %w", err)
	}
	var lastCfg string
	for _, c := range sf.Containers {
		if c.AWG != nil && c.AWG.LastConfig != "" {
			lastCfg = c.AWG.LastConfig
			break
		}
	}
	if lastCfg == "" {
		return "", fmt.Errorf("no AmneziaWG (awg) container in .vpn")
	}
	var inner struct {
		Config string `json:"config"`
	}
	if err := json.Unmarshal([]byte(lastCfg), &inner); err != nil {
		return "", fmt.Errorf("inner json: %w", err)
	}
	if inner.Config == "" {
		return "", fmt.Errorf("no .config text inside last_config")
	}
	dns1 := sf.DNS1
	if dns1 == "" {
		dns1 = "1.1.1.1"
	}
	dns2 := sf.DNS2
	if dns2 == "" {
		dns2 = "1.0.0.1"
	}
	cfg := strings.ReplaceAll(inner.Config, "$PRIMARY_DNS", dns1)
	cfg = strings.ReplaceAll(cfg, "$SECONDARY_DNS", dns2)
	return cfg, nil
}

func ProxyConf(wgPath, bindAddr string) string {
	wgPath = filepath.ToSlash(wgPath)
	return fmt.Sprintf("WGConfig = %s\n\n[http]\nBindAddress = %s\n", wgPath, bindAddr)
}

func WriteRuntime(vpnText, runDir, bindAddr string) (string, error) {
	wg, err := Decode(vpnText)
	if err != nil {
		return "", err
	}
	wgPath := filepath.Join(runDir, "awg.conf")
	if err := os.WriteFile(wgPath, []byte(wg), 0o600); err != nil {
		return "", err
	}
	proxyPath := filepath.Join(runDir, "proxy.conf")
	abs, _ := filepath.Abs(wgPath)
	if err := os.WriteFile(proxyPath, []byte(ProxyConf(abs, bindAddr)), 0o600); err != nil {
		return "", err
	}
	return proxyPath, nil
}
```

- [ ] **Step 5: Run — expect PASS** (`go test ./internal/vpn/ -v`)

- [ ] **Step 6: Commit** — `git commit -am "feat(vpn): Amnezia .vpn decode + proxy.conf gen"`

---

### Task 3: Component version resolution (`internal/version`)

**Files:**
- Create: `internal/version/version.go`, `internal/version/version_test.go`

**Interfaces:**
- Produces:
  ```go
  // ParseClaude extracts "2.1.190" from `claude --version` output "2.1.190 (Claude Code)".
  func ParseClaude(out string) string
  // ParseSemverPrefix extracts the first x.y.z from arbitrary --version output.
  func ParseSemverPrefix(out string) string
  // Newer reports whether b is a strictly newer semver than a (missing => false).
  func Newer(a, b string) bool
  type Component struct{ Name, Current, Found string; HasUpdate bool }
  ```

- [ ] **Step 1: Failing test**

```go
package version

import "testing"

func TestParseClaude(t *testing.T) {
	if got := ParseClaude("2.1.190 (Claude Code)\n"); got != "2.1.190" {
		t.Fatalf("got %q", got)
	}
}
func TestParseSemverPrefix(t *testing.T) {
	if got := ParseSemverPrefix("rtk 0.9.0"); got != "0.9.0" {
		t.Fatalf("got %q", got)
	}
	if got := ParseSemverPrefix("v1.0.9-awg"); got != "1.0.9" {
		t.Fatalf("got %q", got)
	}
}
func TestNewer(t *testing.T) {
	cases := []struct{ a, b string; want bool }{
		{"0.8.2", "0.9.0", true}, {"1.0.9", "1.0.9", false},
		{"2.1.190", "2.1.9", false}, {"", "1.0.0", true}, {"1.0.0", "", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Fatalf("Newer(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement** using a regexp `(\d+)\.(\d+)\.(\d+)` and integer-triple comparison for `Newer` (compare major, then minor, then patch as ints — NOT string compare, so 190 > 9). `ParseClaude` = `ParseSemverPrefix` of the first token.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit** — `git commit -am "feat(version): version parse + semver compare"`

---

### Task 4: Download + verify + extract (`internal/fetch`)

**Files:**
- Create: `internal/fetch/fetch.go`, `internal/fetch/fetch_test.go`

**Interfaces:**
- Produces:
  ```go
  type Progress func(done, total int64)
  // Download streams url to dst, calling p as bytes arrive (total=-1 if unknown).
  func Download(ctx context.Context, url, dst string, p Progress) error
  // VerifySHA256 returns nil iff dst hashes to wantHex (lowercase).
  func VerifySHA256(dst, wantHex string) error
  // ExtractTarGz unpacks want (basename) from a .tar.gz into destDir (zip-slip guarded).
  func ExtractTarGz(srcTgz, member, destFile string) error
  // ExtractZipMember unpacks a single member from a .zip (zip-slip guarded).
  func ExtractZipMember(srcZip, member, destFile string) error
  ```

- [ ] **Step 1: Failing test** (checksum + zip-slip are the pure, testable parts)

```go
package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySHA256(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x")
	os.WriteFile(f, []byte("hello"), 0o644)
	sum := sha256.Sum256([]byte("hello"))
	if err := VerifySHA256(f, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if err := VerifySHA256(f, "deadbeef"); err == nil {
		t.Fatal("expected mismatch error")
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement.** `Download` uses `http.Get`, reads `Content-Length` for total, wraps the body in a counting reader that calls `Progress`. `VerifySHA256` streams the file through `sha256`. Extract functions reject any member whose cleaned path escapes destDir (`strings.HasPrefix(filepath.Clean(joined), destDir)`), matching the current `update.ps1` zip-slip guard.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit** — `git commit -am "feat(fetch): download+sha256+extract with zip-slip guard"`

---

### Task 5: Tunnel lifecycle (`internal/tunnel`) — Windows integration

**Files:**
- Create: `internal/tunnel/tunnel_windows.go`, `internal/tunnel/job_windows.go`

**Interfaces:**
- Consumes: `vpn.WriteRuntime`, `paths.Layout`.
- Produces:
  ```go
  type Tunnel struct{ /* holds job handle + wireproxy *os.Process + pidFile */ }
  // Validate runs `wireproxy-awg.exe -n -c proxyConf`; error on reject.
  func Validate(exe, proxyConf string) error
  // Start spawns wireproxy detached (no window), assigns it to a KILL_ON_JOB_CLOSE
  // job owned by this process, writes pidFile. Returns a Tunnel to Close() on exit.
  func Start(exe, proxyConf, pidFile string) (*Tunnel, error)
  func (t *Tunnel) Close() error   // idempotent: closes job handle (kills wireproxy), removes pidFile
  // KillStalePID reads pidFile (if any) and kills that PID; no-op if absent.
  func KillStalePID(pidFile string) error
  // PortInUse reports whether 127.0.0.1:25345 is already LISTENING (foreign holder).
  func PortInUse(bindAddr string) bool
  ```

- [ ] **Step 1: Job Object wrapper**

`internal/tunnel/job_windows.go`: wrap `windows.CreateJobObject`, `windows.SetInformationJobObject` with `JOBOBJECT_EXTENDED_LIMIT_INFORMATION{ BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE }`, and `windows.AssignProcessToJobObject`. Keep the job handle open for the process lifetime — closing it kills all assigned processes.

- [ ] **Step 2: Start/Validate/Close** using `os/exec` with `SysProcAttr{ HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW }`. After `cmd.Start()`, get `cmd.Process.Pid`, `AssignProcessToJobObject(job, processHandle)`, write pid to `pidFile`. `Close()` closes the job handle (OS kills wireproxy) and deletes `pidFile`; idempotent.

- [ ] **Step 3: PortInUse** — attempt `net.DialTimeout("tcp", bindAddr, 200ms)`; success => something is listening. `KillStalePID` — read pidfile int, `os.FindProcess` + `.Kill()`, ignore "not found".

- [ ] **Step 4: Integration verify** (manual, documented in commit body):
  - Place a real `wireproxy-awg.exe` + a valid `proxy.conf` in a temp dir.
  - Small `main` that calls `Start`, sleeps, then exits WITHOUT calling `Close` (simulates hard kill) → confirm via Task Manager the wireproxy child dies when the parent process exits (Job Object works).
  - Second run: confirm `Start` then `Close` leaves no wireproxy and no pidfile.

- [ ] **Step 5: Commit** — `git commit -m "feat(tunnel): wireproxy spawn + KILL_ON_JOB_CLOSE job + stale-pid heal"`

---

### Task 6: Split-tunnel PAC + registry (`internal/routing`) — Windows integration

**Files:**
- Create: `internal/routing/pac.go`, `internal/routing/pac_test.go`, `internal/routing/registry_windows.go`

**Interfaces:**
- Produces:
  ```go
  // BuildPAC renders a fail-closed PAC routing domains through proxy, else DIRECT.
  func BuildPAC(domains []string, proxy string) string
  // Signature marker written alongside AutoConfigURL so self-heal recognizes ours.
  const Marker = "ccp-portable"
  // Enable writes pacFile and sets HKCU AutoConfigURL=file://pacFile, refreshes WinINET.
  func Enable(pacFile, pacBody string) error
  // Disable removes AutoConfigURL iff it points at our marker/pacFile; refreshes WinINET.
  func Disable(pacFile string) error
  // SelfHeal removes any stale ccp AutoConfigURL left by a prior hard kill.
  func SelfHeal() error
  ```

- [ ] **Step 1: Failing test (PAC generation is pure)**

```go
package routing

import (
	"strings"
	"testing"
)

func TestBuildPACFailClosed(t *testing.T) {
	pac := BuildPAC([]string{"*.anthropic.com", "*.claude.ai"}, "127.0.0.1:25345")
	if !strings.Contains(pac, `shExpMatch(host, "*.anthropic.com")`) {
		t.Fatal("missing anthropic rule")
	}
	if !strings.Contains(pac, `return "PROXY 127.0.0.1:25345"`) {
		t.Fatal("missing proxy return")
	}
	if strings.Contains(pac, "DIRECT\"") && strings.Contains(pac, "PROXY 127.0.0.1:25345; DIRECT") {
		t.Fatal("fail-closed violated: proxy line must not fall back to DIRECT")
	}
	if !strings.Contains(pac, `return "DIRECT"`) { // non-matched hosts still go direct
		t.Fatal("non-matched hosts must be DIRECT")
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement `BuildPAC`**

```go
package routing

import (
	"fmt"
	"strings"
)

const Marker = "ccp-portable"

func BuildPAC(domains []string, proxy string) string {
	var b strings.Builder
	b.WriteString("// " + Marker + "\nfunction FindProxyForURL(url, host) {\n")
	for _, d := range domains {
		fmt.Fprintf(&b, "  if (shExpMatch(host, %q)) return \"PROXY %s\";\n", d, proxy)
	}
	b.WriteString("  return \"DIRECT\";\n}\n")
	return b.String()
}
```
Note fail-closed: matched Anthropic hosts return **only** `PROXY …` (no `; DIRECT`) → dead tunnel = unreachable, no leak. Non-Anthropic hosts return `DIRECT` (that is the split, not a fallback).

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Registry (`registry_windows.go`)** using `golang.org/x/sys/windows/registry`:
  - `Enable`: write `pacFile`; open `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, set `AutoConfigURL` = `file:///` + ToSlash(pacFile); call `InternetSetOption(0, INTERNET_OPTION_SETTINGS_CHANGED, 0, 0)` then `INTERNET_OPTION_REFRESH` (via `wininet.dll` `syscall`/`LazyDLL`).
  - `Disable` / `SelfHeal`: read `AutoConfigURL`; if it points at a `file://…` path whose PAC contains `Marker` (or equals our pacFile) → delete the value + refresh. Never touch a foreign proxy config.

- [ ] **Step 6: Integration verify** (manual, commit body): run a tiny main that calls `Enable`, open Edge → `chrome://net-internals` or check `Get-ItemProperty 'HKCU:\...\Internet Settings' AutoConfigURL`; call `Disable` → value gone. Kill main mid-run → next `SelfHeal()` clears it. Note Firefox limitation (own proxy store).

- [ ] **Step 7: Commit** — `git commit -m "feat(routing): fail-closed PAC + HKCU AutoConfigURL + self-heal"`

---

### Task 7: Claude launch (`internal/claude`) — Windows integration

**Files:**
- Create: `internal/claude/launch_windows.go`

**Interfaces:**
- Consumes: `paths.Layout`.
- Produces:
  ```go
  type LaunchOpts struct {
      Layout   paths.Layout
      UseProxy bool     // set HTTPS_PROXY when true
      ProxyURL string   // "http://127.0.0.1:25345"
  }
  // EnsureLayout copies data/bin/claude.exe into home/.local/bin/claude.exe and
  // home/.local/share/claude/versions/<ver>/claude.exe (the self-managing layout).
  func EnsureLayout(l paths.Layout) (claudeExe string, err error)
  // Run execs claudeExe inheriting this console (stdin/out/err), with the pinned
  // env, and blocks until it exits. Returns claude's exit code.
  func Run(opts LaunchOpts) (int, error)
  ```

- [ ] **Step 1: EnsureLayout** — read version via `data/bin/claude.exe --version` (Task 3 parse). Create `home/.local/bin/` and `home/.local/share/claude/versions/<ver>/`; copy the exe into both (NTFS: try `os.Link` hardlink first, fall back to copy). This suppresses the native binary's "run claude install" re-exec that otherwise loses `CLAUDE_CONFIG_DIR`.

- [ ] **Step 2: Run** — build env from `os.Environ()`:
  - remove any `ANTHROPIC_*` / `CLAUDE_CODE_*` (scrub host), and existing `HOME`, `HTTPS_PROXY`, `HTTP_PROXY`.
  - set `HOME=l.Home`, `CLAUDE_CONFIG_DIR=l.ClaudeCfg`, `CCP_HOOKS=l.ClaudeCfg\hooks`.
  - prepend `l.Bin` to `PATH`.
  - if `opts.UseProxy`: set `HTTPS_PROXY` and `HTTP_PROXY` = `opts.ProxyURL`.
  - `cmd := exec.Command(claudeExe)`; `cmd.Stdin/Stdout/Stderr = os.Stdin/os.Stdout/os.Stderr` (console handoff — same window becomes claude); `cmd.Run()`; return `cmd.ProcessState.ExitCode()`.

- [ ] **Step 3: Integration verify** (manual, commit body): with a real `data/bin/claude.exe` + valid `claude-cfg`, run a tiny main calling `EnsureLayout`+`Run{UseProxy:false}` → claude REPL takes over the same window; `/status` shows config dir = the stick's `claude-cfg` (NOT `$HOME\.claude`), proving the re-exec was suppressed.

- [ ] **Step 4: Commit** — `git commit -m "feat(claude): self-managing layout + pinned-env console handoff"`

---

### Task 8: Update engine (`internal/update`)

**Files:**
- Create: `internal/update/update.go`, `internal/update/sources.go`, `internal/update/update_test.go`

**Interfaces:**
- Consumes: `fetch`, `version`, `paths`.
- Produces:
  ```go
  type Comp struct {
      Name    string
      Current func(l paths.Layout) string                 // installed version ("" if absent)
      Latest  func(ctx context.Context) (ver string, err error)
      Install func(ctx context.Context, l paths.Layout, ver string, p fetch.Progress) error
  }
  func Components() []Comp   // claude, rtk, wireproxy-awg, statusline
  // CheckAll resolves Current+Latest concurrently into version.Component rows.
  func CheckAll(ctx context.Context, l paths.Layout) []version.Component
  // SelfLatest / SelfUpdate use go-selfupdate against buildinfo.Repo.
  ```

- [ ] **Step 1: Source table (`sources.go`)** — one `Comp` per component:
  - **claude**: Current = parse `data/bin/claude.exe --version`; Latest = GET `https://downloads.claude.ai/claude-code-releases/stable` (manifest) → version; Install = download `.../<ver>/win32-x64/claude.exe` to temp, `fetch.VerifySHA256` against manifest sha, atomic replace `data/bin/claude.exe`.
  - **rtk**: Latest = `api.github.com/repos/rtk-ai/rtk/releases/latest` tag; Install = download windows asset, extract to `data/bin/rtk.exe`.
  - **wireproxy-awg**: Latest = `api.github.com/repos/artem-russkikh/wireproxy-awg/releases/latest`; Install = download `wireproxy_windows_amd64.tar.gz`, `fetch.ExtractTarGz` member `wireproxy.exe` → `data/bin/wireproxy-awg.exe`.
  - **statusline**: Latest = `api.github.com/repos/UberMorgott/MorgottStatusLine/releases/latest` (fallback: main-branch commit sha if no releases — see spec risk); Install = download/extract to `data/bin/`.
- [ ] **Step 2: `CheckAll`** — `parallel` via goroutines + `sync.WaitGroup`, each resolving Current (local, fast) and Latest (network); assemble `version.Component{HasUpdate: version.Newer(cur, found)}`. Network failure → `Found=""`, no update, non-fatal.
- [ ] **Step 3: Test** — table-test `version.Newer` wiring by injecting a fake `Comp` with stub `Current`/`Latest` and asserting `HasUpdate`. (Real network calls are integration, not unit.)
- [ ] **Step 4: Run — expect PASS**
- [ ] **Step 5: Commit** — `git commit -m "feat(update): component source table + concurrent check"`

---

### Task 9: TUI (`internal/tui`) + main wiring

**Files:**
- Create: `internal/tui/model.go`, `internal/tui/view.go`, `internal/tui/update.go`, `internal/tui/messages.go`
- Modify: `cmd/ccp/main.go`

**Interfaces:**
- Consumes: `update.CheckAll`, `update.Comp.Install`, `tunnel.*`, `routing.*`, `claude.*`, `vpn.WriteRuntime`, `paths.Layout`.
- Produces: `func Run(l paths.Layout) error` — the program entrypoint.

**Model (state machine):**
```go
type compRow struct {
	name           string
	current, found string
	hasUpdate      bool
	downloading    bool
	pct            float64 // 0..1
	done           bool
}
type model struct {
	l         paths.Layout
	rows      []compRow
	cursor    int      // 0=Launch, 1=Update all (rows are clickable via mouse)
	useVPN    bool     // checkbox, persisted (data/claude-cfg/ccp-state.json)
	updating  bool     // any download in flight -> Launch disabled
	spin      spinner.Model
	launching bool
	err       string
}
```

**Messages (`messages.go`):** `checkDoneMsg{rows []compRow}`, `progressMsg{name string; pct float64}`, `installDoneMsg{name string; newVer string; err error}`, `vpnValidateMsg{ok bool; reason string}`.

**Behavior (`update.go`) — implement exactly per spec §TUI:**
- On init: `spin.Tick` + a `tea.Cmd` running `update.CheckAll` (+ self version) → `checkDoneMsg`. Header shows current/found live.
- `Launch` action (enter on cursor 0, or click): **disabled while `updating`**. On trigger → `launching=true`, `tea.Quit` with a stored intent; `main` then runs the launch sequence (below). 
- `Use VPN` checkbox (right of Launch; key `v`, or click): toggling ON runs a `tea.Cmd` validating `wg-config/*.vpn` decodes (`vpn.Decode`); invalid → `vpnValidateMsg{ok:false}` → show the generate-config message + revert to unchecked. Persist state on change.
- `Update all` (cursor 1 / click; **enabled only if any `hasUpdate`, incl. ccp**) → start every updatable row's `Install` as a `tea.Cmd` streaming `progressMsg`. Per-component: click a row with `hasUpdate` → start just that one. Set `updating=true`; `Launch` greys out.
- `progressMsg` → set row.pct, re-render bar. `installDoneMsg` → row.done, current=newVer, hasUpdate=false; recompute `updating` = any still downloading; when none, re-enable Launch and grey `Update all` if nothing left.
- Mouse: enable `tea.WithMouseCellMotion()`; hit-test click Y against rendered row/button bounds (store bounds during `View`).

**View (`view.go`):** lipgloss table header (Component | Current | Found | status glyph: `✓`/`↑`/spinner/`bubbles/progress` bar to the right of the row). Below: `❯ Launch Claude   [x] Use VPN` line, then `Update all` (greyed when disabled). Footer: `↑↓ · enter · v vpn · q`.

**Launch sequence (in `main`, after TUI quits with launch intent):**
1. `routing.SelfHeal()`; `tunnel.KillStalePID(pid)`; wipe+recreate `_run`.
2. if `useVPN`: read `wg-config/*.vpn`; `vpn.WriteRuntime` → proxyConf; `tunnel.Validate`; `tunnel.Start` (job); `routing.Enable(pac)`. Guard `tunnel.PortInUse` first.
3. `claude.EnsureLayout`; `claude.Run(LaunchOpts{UseProxy:useVPN, ProxyURL:"http://127.0.0.1:25345"})`.
4. defer: `routing.Disable`; `tunnel.Close`; wipe `_run`.

- [ ] **Step 1:** model + messages + init check command; render static header from a stubbed `CheckAll`. Verify: `go run ./cmd/ccp` shows the header + menu, `q` quits.
- [ ] **Step 2:** wire `Use VPN` toggle + validation message + persistence. Verify toggling with no `.vpn` shows the generate message and reverts.
- [ ] **Step 3:** wire per-component + `Update all` with live progress bars (use a fake slow `Install` first to see bars). Verify Launch greys during update, `Update all` greys when all green.
- [ ] **Step 4:** wire the real launch sequence in `main`; end-to-end: pick VPN off → Launch → claude takes the window; exit → back to shell, `_run` empty, no `AutoConfigURL`.
- [ ] **Step 5:** mouse hit-testing.
- [ ] **Step 6: Commit** — `git commit -m "feat(tui): reactive bubbletea menu + launch wiring"`

---

### Task 10: Config strip + repo cleanup

**Files:**
- Modify: `claude-cfg/settings.json`, `claude-cfg/CLAUDE.md`, `.gitignore`, `README.md`
- Delete (tracked): `Start.bat`, `Stop.bat`, `Install or Update.bat`, `bootstrap.cmd`, `shell/*.ps1`, `claude-cfg/hooks/*.ps1`, `claude-cfg/mcp-servers.json`, `claude-cfg/mcp-secrets*.ps1`, `claude-cfg/skills/`, `claude-cfg/memory/`

- [ ] **Step 1: settings.json** — reduce to: `env` (`CLAUDE_CODE_USE_POWERSHELL_TOOL`, `DISABLE_AUTOUPDATER`, `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS`), `permissions.defaultMode=bypassPermissions`, `skipDangerousModePermissionPrompt`, `defaultShell`, `language`, `teammateMode`, `voiceEnabled`, `statusLine`, and the **rtk hook only** (PreToolUse `Bash|PowerShell` → `rtk hook claude`, `shell:powershell`). Remove `enabledPlugins`, `extraKnownMarketplaces`, `load-memory`/`detect-stuck` hook blocks.
- [ ] **Step 2: CLAUDE.md** — rewrite "Portable run context": no bundled node/pwsh; toolchain is `ccp.exe` + host Windows PowerShell 5.1 for the rtk hook + Claude PowerShell tool; remove references to bundled sequential-thinking MCP / skills.
- [ ] **Step 3:** `git rm` the deleted set; update `.gitignore` (drop `node/ pwsh/ wt/ wireproxy/`; add `data/`, `ccp.exe`, `/dist/`); update `README.md` to the new blank-stick flow (download `ccp.exe` → run → Update).
- [ ] **Step 4: Verify** — `git status` shows only intended deletions/edits; no stray references to deleted scripts (`grep -ril 'Start.bat\|profile.ps1\|update.ps1' .`).
- [ ] **Step 5: Commit** — `git commit -m "chore(config): strip to ccp.exe world; remove cmd/pwsh/wt/mcp/skills"`

---

### Task 11: End-to-end validation on a real stick

- [ ] **Step 1:** `make release VERSION=v2.0.0-rc1`; confirm `dist/ccp.exe` runs, `dist/checksums.txt` matches the packed exe.
- [ ] **Step 2:** Blank stick: copy only `ccp.exe` → run → `Update all` fetches claude/rtk/wireproxy/statusline into `data/bin`.
- [ ] **Step 3:** Put a real `wg-config/*.vpn`; VPN on → Launch → in claude run a request; confirm exit IP = VPN (`curl ifconfig.me` from claude's PowerShell tool through proxy) and host `curl` (direct terminal) shows the real IP (split works).
- [ ] **Step 4:** Browser: with VPN on, open `claude.ai` in Edge → confirm it loads via VPN IP (re-auth path). Kill ccp hard → confirm next launch self-heals `AutoConfigURL`.
- [ ] **Step 5:** VPN off → Launch → claude goes direct, no proxy env, no PAC.
- [ ] **Step 6:** Hard-close the window mid-session → confirm wireproxy dies (Job Object) and no leftover in `_run`.
- [ ] **Step 7: Tag** — `git commit`, push branch, open PR.

---

## Self-Review notes

- Spec coverage: structure(T1,T10) · vpn decode(T2) · tunnel+JobObject(T5) · claude self-managing layout+kill-switch(T7) · split-tunnel PAC fail-closed+self-heal(T6) · update+version sources(T3,T4,T8) · reactive TUI+VPN checkbox(T9) · settings strip(T10) · strip+UPX+GH Actions+checksums(T0) · perf(no code, design-only) · risks(validated in T5/T6/T7/T11). All covered.
- Type consistency: `paths.Layout`, `version.Component`, `fetch.Progress`, `update.Comp` names used identically across T1–T9.
- Integration modules (T5/T6/T7) carry manual verification steps in lieu of unit tests where Windows syscalls/registry/console make unit tests impractical; pure logic (T1–T4, T6 PAC, T8 wiring) is unit-tested.
