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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// errPermanent marks a response that must NOT be retried (a permanent 4xx like
// 404/403): retrying only burns the backoff budget before the same failure.
var errPermanent = errors.New("permanent http error")

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

// Retry/backoff/stall knobs. Vars (not consts) so tests can shrink them.
var (
	maxAttempts  = 5
	stallTimeout = 30 * time.Second
	// backoff[i] is the wait BEFORE attempt i+1; the last value caps.
	backoff = []time.Duration{
		500 * time.Millisecond, 1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
	}
)

// stallReader resets a watchdog before every Read; if a single Read blocks
// longer than stallTimeout the timer cancels the attempt's context, unblocking
// the Read with an error so a dead socket cannot hang forever.
type stallReader struct {
	r     io.Reader
	timer *time.Timer
}

func (s *stallReader) Read(b []byte) (int, error) {
	s.timer.Reset(stallTimeout)
	return s.r.Read(b)
}

// Download streams url to dst, calling p as bytes arrive (total=-1 if unknown).
// It retries transient failures with exponential backoff and resumes partial
// downloads across attempts via HTTP Range, so a flaky network does not force a
// restart from zero. Progress is cumulative: p(done,total) reports total bytes
// on disk (including earlier attempts) so the bar never jumps backwards.
func Download(ctx context.Context, url, dst string, p Progress) error {
	total := int64(-1) // full file size once known; shared across attempts
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			d := backoff[min(attempt-1, len(backoff)-1)]
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		lastErr = downloadAttempt(ctx, url, dst, p, &total)
		if lastErr == nil {
			return nil
		}
		// Parent cancellation/deadline is fatal (a stall-watchdog cancel is
		// internal to the attempt and leaves ctx.Err() nil, so we retry that).
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A permanent client error (404/403/…) won't change on retry — fail fast.
		if errors.Is(lastErr, errPermanent) {
			return lastErr
		}
	}
	return fmt.Errorf("download %s: giving up after %d attempts: %w", url, maxAttempts, lastErr)
}

// downloadAttempt performs one GET, resuming from any bytes already in dst.
func downloadAttempt(ctx context.Context, url, dst string, p Progress, total *int64) error {
	var have int64
	if fi, err := os.Stat(dst); err == nil {
		have = fi.Size()
	}

	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var f *os.File
	switch resp.StatusCode {
	case http.StatusOK:
		// Fresh, or the server ignored Range: truncate and write from zero.
		have = 0
		if resp.ContentLength >= 0 {
			*total = resp.ContentLength
		}
		f, err = os.Create(dst)
	case http.StatusPartialContent:
		// Resume: append the remainder onto the existing bytes.
		if t := totalFromContentRange(resp.Header.Get("Content-Range")); t >= 0 {
			*total = t
		} else if resp.ContentLength >= 0 {
			*total = have + resp.ContentLength
		}
		f, err = os.OpenFile(dst, os.O_WRONLY|os.O_APPEND, 0o644)
	case http.StatusRequestedRangeNotSatisfiable:
		// Stale/oversized partial: drop it and retry a full download.
		_ = os.Truncate(dst, 0)
		return fmt.Errorf("download %s: %s (range reset)", url, resp.Status)
	default:
		// Permanent client errors (4xx) won't change on retry; 408 Request Timeout
		// and 429 Too Many Requests are transient, so let those retry.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 &&
			resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests {
			return fmt.Errorf("download %s: %s: %w", url, resp.Status, errPermanent)
		}
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	if err != nil {
		return err
	}

	timer := time.AfterFunc(stallTimeout, cancel)
	defer timer.Stop()
	cr := &countingReader{r: &stallReader{r: resp.Body, timer: timer}, done: have, total: *total, p: p}
	_, copyErr := io.Copy(f, cr)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// totalFromContentRange parses the full size from a "bytes A-B/N" header,
// returning -1 when N is "*" (unknown) or the header is absent/malformed.
func totalFromContentRange(v string) int64 {
	i := strings.LastIndex(v, "/")
	if i < 0 {
		return -1
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v[i+1:]), 10, 64)
	if err != nil {
		return -1
	}
	return n
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
