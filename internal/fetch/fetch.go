// Package fetch downloads, verifies, and extracts single members from release
// archives. Stdlib only; mirrors the zip-slip guard in shell/update.ps1.
package fetch

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Progress reports download bytes; total is -1 when the server omits Content-Length.
type Progress func(done, total int64)

// countingReader forwards Read and reports cumulative bytes via p.
type countingReader struct {
	r           io.Reader
	done, total int64
	p           Progress
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	if n > 0 {
		c.done += int64(n)
		if c.p != nil {
			c.p(c.done, c.total)
		}
	}
	return n, err
}

// Download streams url to dst, calling p as bytes arrive (total=-1 if unknown).
func Download(ctx context.Context, url, dst string, p Progress) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	cr := &countingReader{r: resp.Body, total: resp.ContentLength, p: p}
	if _, err := io.Copy(f, cr); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// VerifySHA256 returns nil iff dst hashes to wantHex (case-insensitive).
func VerifySHA256(dst, wantHex string) error {
	f, err := os.Open(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantHex) {
		return fmt.Errorf("sha256 mismatch for %s: got %s want %s", dst, got, wantHex)
	}
	return nil
}

// ExtractTarGz writes the .tar.gz entry whose basename is member to destFile.
func ExtractTarGz(srcTgz, member, destFile string) error {
	f, err := os.Open(srcTgz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	destDir := filepath.Dir(destFile)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(h.Name) != member {
			continue
		}
		if err := guard(destDir, h.Name); err != nil {
			return err
		}
		return writeMember(destFile, tr)
	}
	return fmt.Errorf("member %q not found in %s", member, srcTgz)
}

// ExtractZipMember writes the .zip entry whose basename is member to destFile.
func ExtractZipMember(srcZip, member, destFile string) error {
	zr, err := zip.OpenReader(srcZip)
	if err != nil {
		return err
	}
	defer zr.Close()
	destDir := filepath.Dir(destFile)
	for _, zf := range zr.File {
		if filepath.Base(zf.Name) != member {
			continue
		}
		if err := guard(destDir, zf.Name); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = writeMember(destFile, rc)
		rc.Close()
		return err
	}
	return fmt.Errorf("member %q not found in %s", member, srcZip)
}

// guard rejects an archive member whose name escapes destDir (zip-slip).
func guard(destDir, name string) error {
	rel, err := filepath.Rel(destDir, filepath.Join(destDir, name))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("member %q escapes destination %s", name, destDir)
	}
	return nil
}

func writeMember(destFile string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(destFile), 0o755); err != nil {
		return err
	}
	out, err := os.Create(destFile)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
