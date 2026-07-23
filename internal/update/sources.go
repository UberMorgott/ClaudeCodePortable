package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/UberMorgott/ClaudeCodePortable/internal/fetch"
	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
	"github.com/UberMorgott/ClaudeCodePortable/internal/version"
)

// Bundled binary filenames under <Bin>.
const (
	claudeExe     = "claude.exe"
	rtkExe        = "rtk.exe"
	wireproxyExe  = "wireproxy-awg.exe"
	statuslineExe = "statusline.exe"
)

// Release endpoints.
const (
	claudeStableURL  = "https://downloads.claude.ai/claude-code-releases/stable"
	claudeAssetBase  = "https://downloads.claude.ai/claude-code-releases" // /<ver>/win32-x64/claude.exe
	rtkReleaseURL    = "https://api.github.com/repos/rtk-ai/rtk/releases/latest"
	wireproxyRelURL  = "https://api.github.com/repos/artem-russkikh/wireproxy-awg/releases/latest"
	statuslineRelURL = "https://api.github.com/repos/UberMorgott/MorgottStatusLine/releases/latest"
	statuslineCommit = "https://api.github.com/repos/UberMorgott/MorgottStatusLine/commits/HEAD"
)

// Components returns the bundled-tool source table: claude, rtk, wireproxy-awg,
// statusline. Each entry knows how to read its installed version, find the
// latest, and install it.
func Components() []Comp {
	return []Comp{
		claudeComp(),
		rtkComp(),
		wireproxyComp(),
		statuslineComp(),
	}
}

func claudeComp() Comp {
	return Comp{
		Name:    "claude",
		Current: func(l paths.Layout) string { return currentVersion(l.BinPath(claudeExe)) },
		Latest: func(ctx context.Context) (string, error) {
			s, err := httpGetString(ctx, claudeStableURL)
			if err != nil {
				return "", err
			}
			v := version.ParseSemverPrefix(s)
			if v == "" {
				return "", fmt.Errorf("claude stable: no version in %q", s)
			}
			return v, nil
		},
		Install: func(ctx context.Context, l paths.Layout, ver string, p fetch.Progress) error {
			url := fmt.Sprintf("%s/%s/win32-x64/claude.exe", claudeAssetBase, ver)
			tmp, err := downloadTemp(ctx, url, l.Bin, "claude-*.exe", p)
			if err != nil {
				return err
			}
			defer os.Remove(tmp)
			// TODO(task-8): the checksum endpoint for claude assets is UNCONFIRMED.
			// The URL below is an ASSUMED convention (<asset>.sha256). If it 404s we
			// proceed unverified — a security gap flagged in the task report. Replace
			// with the real manifest-sha source once the release layout is confirmed.
			if want, err := httpGetString(ctx, url+".sha256"); err == nil && want != "" {
				// A sha256 file is typically "<hex>  <filename>"; take the first field.
				want = strings.Fields(want)[0]
				if err := fetch.VerifySHA256(tmp, want); err != nil {
					return err
				}
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

func statuslineComp() Comp {
	return Comp{
		Name:    "statusline",
		Current: func(l paths.Layout) string { return currentVersion(l.BinPath(statuslineExe)) },
		Latest: func(ctx context.Context) (string, error) {
			// Prefer a tagged release; MorgottStatusLine may be script-only with no
			// releases, in which case fall back to the default-branch HEAD commit sha.
			if tag, err := githubTag(ctx, statuslineRelURL); err == nil {
				return tag, nil
			}
			return githubHeadSHA(ctx, statuslineCommit)
		},
		Install: func(ctx context.Context, l paths.Layout, ver string, p fetch.Progress) error {
			rel, err := githubLatest(ctx, statuslineRelURL)
			if err != nil {
				// TODO(task-8): MorgottStatusLine has no confirmed release assets; the
				// commit-sha fallback path has no defined installable artifact. Flagged
				// as a concern — wire the real install once the repo's ship format is known.
				return fmt.Errorf("statusline: no release to install (script-only repo?): %w", err)
			}
			url, name, ok := pickAsset(rel, func(n string) bool {
				n = strings.ToLower(n)
				return strings.Contains(n, "windows") && (strings.Contains(n, "amd64") || strings.Contains(n, "x86_64"))
			})
			if !ok {
				return fmt.Errorf("statusline: no windows/amd64 asset in %s", rel.TagName)
			}
			return installAsset(ctx, l, url, name, statuslineExe, "statusline.exe", p)
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

// githubHeadSHA returns the sha of a repo's HEAD commit (commits/HEAD API URL).
// ponytail: sha is not a semver, so version.Newer(cur, sha) is always false —
// this fallback surfaces the pinned commit but never flags an update. Good
// enough until MorgottStatusLine cuts real releases.
func githubHeadSHA(ctx context.Context, apiURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", apiURL, resp.Status)
	}
	var body struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.SHA == "" {
		return "", fmt.Errorf("%s: no sha in commit payload", apiURL)
	}
	return body.SHA, nil
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
