// Package update resolves bundled-tool versions and installs newer releases.
// The source table lives in sources.go; this file holds the concurrent check,
// the shared HTTP/GitHub/replace helpers, and the go-selfupdate wiring.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	selfupdate "github.com/creativeprojects/go-selfupdate"

	"github.com/UberMorgott/ClaudeCodePortable/internal/buildinfo"
	"github.com/UberMorgott/ClaudeCodePortable/internal/fetch"
	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
	"github.com/UberMorgott/ClaudeCodePortable/internal/version"
)

// Comp is one bundled component: how to read its installed version, how to find
// the latest published version, and how to install a given version.
type Comp struct {
	Name    string
	Current func(l paths.Layout) string                       // installed version ("" if absent/unreadable)
	Latest  func(ctx context.Context) (ver string, err error) // published version
	Install func(ctx context.Context, l paths.Layout, ver string, p fetch.Progress) error
}

// checkAll resolves every component's Current (local, fast) and Latest (network)
// concurrently into version.Component rows. A network failure for one component
// is non-fatal: its Found is left "" and HasUpdate false. Ordering matches the
// passed slice. It takes the component slice explicitly so a unit test can
// inject fakes without touching the network.
//
// The TUI no longer calls this (it streams per-component checks); kept as the
// tested concurrent-check core.
func checkAll(ctx context.Context, l paths.Layout, comps []Comp) []version.Component {
	rows := make([]version.Component, len(comps))
	var wg sync.WaitGroup
	for i, c := range comps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cur := c.Current(l)
			found, err := c.Latest(ctx)
			if err != nil {
				found = "" // non-fatal: report no known latest
			}
			rows[i] = version.Component{
				Name:      c.Name,
				Current:   cur,
				Found:     found,
				HasUpdate: found != "" && version.Newer(cur, found),
			}
		}()
	}
	wg.Wait()
	return rows
}

// checksumsFile is the release asset go-selfupdate's ChecksumValidator verifies
// the downloaded binary against (goreleaser default, matched by Makefile/build.ps1).
const checksumsFile = "checksums.txt"

// newUpdater builds a go-selfupdate Updater whose downloads are gated on a
// SHA-256 ChecksumValidator, mirroring morgward's newUpdater(): both DetectLatest
// and the download path verify the asset against checksums.txt before it can
// replace the running binary. A release lacking checksums.txt fails closed
// rather than applying an unverified binary. Matches how claude.exe is verified
// against its manifest checksum before replace.
func newUpdater() (*selfupdate.Updater, error) {
	return selfupdate.NewUpdater(selfupdate.Config{
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: checksumsFile},
	})
}

// SelfLatest returns the latest CCP release version published on buildinfo.Repo.
func SelfLatest(ctx context.Context) (string, error) {
	updater, err := newUpdater()
	if err != nil {
		return "", err
	}
	rel, found, err := updater.DetectLatest(ctx, selfupdate.ParseSlug(buildinfo.Repo))
	if err != nil {
		return "", err
	}
	if !found || rel == nil {
		return "", fmt.Errorf("no release asset found for this OS/arch")
	}
	return rel.Version(), nil
}

// SelfUpdate replaces the running executable with the latest CCP release,
// verifying its checksum before the swap. It is a no-op returning the current
// version when nothing newer is published.
func SelfUpdate(ctx context.Context) (string, error) {
	updater, err := newUpdater()
	if err != nil {
		return "", err
	}
	rel, found, err := updater.DetectLatest(ctx, selfupdate.ParseSlug(buildinfo.Repo))
	if err != nil {
		return "", err
	}
	if !found || rel == nil || !rel.GreaterThan(buildinfo.Version) {
		return buildinfo.Version, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// UpdateTo applies exactly the release we detected (verifying its checksum),
	// avoiding a re-detect TOCTOU window between detect and download.
	if err := updater.UpdateTo(ctx, rel, exe); err != nil {
		return "", err
	}
	return rel.Version(), nil
}

// --- shared helpers -------------------------------------------------------

// currentVersion runs `<exe> --version` and returns its first x.y.z, or "" when
// the binary is absent or emits no parseable version.
func currentVersion(exePath string) string {
	out, err := exec.Command(exePath, "--version").Output()
	if err != nil {
		return ""
	}
	return version.ParseSemverPrefix(string(out))
}

// httpGetString GETs url and returns its trimmed body (for plain-text endpoints).
func httpGetString(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// ghRelease is the subset of the GitHub "latest release" payload we use.
type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// githubLatest fetches the latest release of a repo's releases API URL.
func githubLatest(ctx context.Context, apiURL string) (ghRelease, error) {
	var r ghRelease
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return r, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("GET %s: %s", apiURL, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return r, err
	}
	return r, nil
}

// pickAsset returns the download URL + filename of the first release asset whose
// name satisfies match.
func pickAsset(r ghRelease, match func(name string) bool) (url, name string, ok bool) {
	for _, a := range r.Assets {
		if match(a.Name) {
			return a.URL, a.Name, true
		}
	}
	return "", "", false
}

// downloadTemp downloads url into a fresh temp file inside dir and returns its
// path. dir is used (not the OS temp dir) so a later same-volume rename into
// place is atomic. The caller owns cleanup.
func downloadTemp(ctx context.Context, url, dir, pattern string, p fetch.Progress) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	f.Close()
	if err := fetch.Download(ctx, url, tmp, p); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// stagedReplace moves tmp onto dst atomically. On Windows a running binary can
// be renamed aside but not deleted, so the prior file is moved to "<dst>.old"
// (swept best-effort here; a still-locked leftover is left for the next run).
func stagedReplace(tmp, dst string) error {
	old := dst + ".old"
	_ = os.Remove(old)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			return fmt.Errorf("move aside %s: %w", dst, err)
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("install %s: %w", dst, err)
	}
	_ = os.Remove(old)
	return nil
}

// extractMember extracts member from a .tar.gz or .zip archive to a temp file in
// dir and returns its path. The archive extension picks the extractor.
func extractMember(archive, member, dir, pattern string) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	f.Close()
	switch {
	case strings.HasSuffix(archive, ".tar.gz") || strings.HasSuffix(archive, ".tgz"):
		err = fetch.ExtractTarGz(archive, member, tmp)
	case strings.HasSuffix(archive, ".zip"):
		err = fetch.ExtractZipMember(archive, member, tmp)
	default:
		err = fmt.Errorf("unsupported archive %q", filepath.Base(archive))
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}
