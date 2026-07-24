package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/UberMorgott/ClaudeCodePortable/internal/fetch"
	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
)

// Bundled binary filenames under <Bin>.
const (
	claudeExe    = "claude.exe"
	rtkExe       = "rtk.exe"
	wireproxyExe = "wireproxy-awg.exe"
)

// Release endpoints.
const (
	claudeLatestURL  = "https://downloads.claude.ai/claude-code-releases/latest"
	claudeAssetBase  = "https://downloads.claude.ai/claude-code-releases" // /<ver>/{manifest.json,win32-x64/claude.exe}
	rtkReleaseURL   = "https://api.github.com/repos/rtk-ai/rtk/releases/latest"
	wireproxyRelURL = "https://api.github.com/repos/artem-russkikh/wireproxy-awg/releases/latest"
)

// Components returns the bundled-tool source table: claude, rtk, wireproxy-awg.
// Each entry knows how to read its installed version, find the latest, and
// install it. (The statusline is now embedded in ccp.exe — `ccp statusline` —
// and updated via ccp's own self-update, so it's not a separate component.)
func Components() []Comp {
	return []Comp{
		claudeComp(),
		rtkComp(),
		wireproxyComp(),
	}
}

// semverExact matches a plain "x.y.z" prefix (mirrors update.ps1's ^\d+\.\d+\.\d+).
var semverExact = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// claudeManifest is the subset of <ver>/manifest.json we read: per-platform checksum.
type claudeManifest struct {
	Platforms map[string]struct {
		Checksum string `json:"checksum"`
	} `json:"platforms"`
}

// claudeManifestFor fetches and decodes the release manifest for ver.
func claudeManifestFor(ctx context.Context, ver string) (claudeManifest, error) {
	var m claudeManifest
	url := fmt.Sprintf("%s/%s/manifest.json", claudeAssetBase, ver)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return m, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return m, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return m, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	err = json.NewDecoder(resp.Body).Decode(&m)
	return m, err
}

func claudeComp() Comp {
	return Comp{
		Name:    "claude",
		Current: func(l paths.Layout) string { return currentVersion(l.BinPath(claudeExe)) },
		Latest: func(ctx context.Context) (string, error) {
			// Authoritative source: /latest is plain-text "x.y.z" (see Ensure-Claude
			// in shell/update.ps1). Validate the shape before trusting it.
			s, err := httpGetString(ctx, claudeLatestURL)
			if err != nil {
				return "", err
			}
			if !semverExact.MatchString(s) {
				return "", fmt.Errorf("claude latest: bad version %q", s)
			}
			return s, nil
		},
		Install: func(ctx context.Context, l paths.Layout, ver string, p fetch.Progress) error {
			// Checksum comes from the release manifest, not an asset sibling.
			man, err := claudeManifestFor(ctx, ver)
			if err != nil {
				return err
			}
			sum := man.Platforms["win32-x64"].Checksum
			if sum == "" {
				return fmt.Errorf("no win32-x64 checksum in manifest")
			}
			url := fmt.Sprintf("%s/%s/win32-x64/claude.exe", claudeAssetBase, ver)
			tmp, err := downloadTemp(ctx, url, l.Bin, "claude-*.exe", p)
			if err != nil {
				return err
			}
			defer os.Remove(tmp) // sweeps the temp on any failure below (no half-written exe)
			if err := fetch.VerifySHA256(tmp, sum); err != nil {
				return err
			}
			return stagedReplace(tmp, l.BinPath(claudeExe))
		},
	}
}

func rtkComp() Comp {
	return Comp{
		Name:    "rtk",
		Current: func(l paths.Layout) string { return currentVersion(l.BinPath(rtkExe)) },
		Latest:  func(ctx context.Context) (string, error) { return githubTag(ctx, rtkReleaseURL) },
		Install: func(ctx context.Context, l paths.Layout, ver string, p fetch.Progress) error {
			rel, err := githubLatest(ctx, rtkReleaseURL)
			if err != nil {
				return err
			}
			url, name, ok := pickAsset(rel, func(n string) bool {
				n = strings.ToLower(n)
				return strings.Contains(n, "windows") && (strings.Contains(n, "amd64") || strings.Contains(n, "x86_64"))
			})
			if !ok {
				return fmt.Errorf("rtk: no windows/amd64 asset in %s", rel.TagName)
			}
			return installAsset(ctx, l, url, name, rtkExe, "rtk.exe", p)
		},
	}
}

func wireproxyComp() Comp {
	return Comp{
		Name:    "wireproxy-awg",
		Current: func(l paths.Layout) string { return currentVersion(l.BinPath(wireproxyExe)) },
		Latest:  func(ctx context.Context) (string, error) { return githubTag(ctx, wireproxyRelURL) },
		Install: func(ctx context.Context, l paths.Layout, ver string, p fetch.Progress) error {
			rel, err := githubLatest(ctx, wireproxyRelURL)
			if err != nil {
				return err
			}
			url, name, ok := pickAsset(rel, func(n string) bool { return n == "wireproxy_windows_amd64.tar.gz" })
			if !ok {
				return fmt.Errorf("wireproxy-awg: asset wireproxy_windows_amd64.tar.gz not in %s", rel.TagName)
			}
			return installAsset(ctx, l, url, name, wireproxyExe, "wireproxy.exe", p)
		},
	}
}

// githubTag returns the latest release tag_name of a releases API URL.
func githubTag(ctx context.Context, apiURL string) (string, error) {
	rel, err := githubLatest(ctx, apiURL)
	if err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("%s: empty tag_name", apiURL)
	}
	return rel.TagName, nil
}

// installAsset downloads a release asset and places the wanted binary at
// <Bin>/dstName. If the asset is an archive the member is extracted; a raw .exe
// asset is installed directly.
func installAsset(ctx context.Context, l paths.Layout, url, assetName, dstName, member string, p fetch.Progress) error {
	// Raw executable asset: download straight to a temp and replace.
	if strings.HasSuffix(assetName, ".exe") {
		tmp, err := downloadTemp(ctx, url, l.Bin, dstName+"-*.exe", p)
		if err != nil {
			return err
		}
		defer os.Remove(tmp)
		return stagedReplace(tmp, l.BinPath(dstName))
	}
	// Archive asset: download, extract the member, replace.
	arc, err := downloadTemp(ctx, url, l.Bin, "*-"+assetName, p)
	if err != nil {
		return err
	}
	defer os.Remove(arc)
	exe, err := extractMember(arc, member, l.Bin, dstName+"-*.exe")
	if err != nil {
		return err
	}
	defer os.Remove(exe)
	return stagedReplace(exe, l.BinPath(dstName))
}
