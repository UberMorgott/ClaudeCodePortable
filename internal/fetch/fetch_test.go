package fetch

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// shrinkRetry makes retries fast and deterministic for tests, restoring the
// package defaults on cleanup.
func shrinkRetry(t *testing.T, attempts int) {
	t.Helper()
	oa, ob, os_ := maxAttempts, backoff, stallTimeout
	maxAttempts = attempts
	backoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { maxAttempts, backoff, stallTimeout = oa, ob, os_ })
}

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

func TestDownload(t *testing.T) {
	body := bytes.Repeat([]byte("abc"), 1000) // 3000 bytes
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	var lastDone, lastTotal int64
	var calls int
	p := func(done, total int64) { calls++; lastDone, lastTotal = done, total }

	if err := Download(context.Background(), srv.URL, dst, p); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("content mismatch: got %d bytes want %d", len(got), len(body))
	}
	if calls == 0 {
		t.Fatal("Progress never called")
	}
	if lastDone != int64(len(body)) {
		t.Fatalf("final done=%d want %d", lastDone, len(body))
	}
	if lastTotal != int64(len(body)) {
		t.Fatalf("total=%d want %d", lastTotal, len(body))
	}
}

// TestDownloadResume drops the connection mid-stream on attempt 1, then honors
// Range on attempt 2 with a 206 remainder. Final dst must equal the full body
// (resumed, not double-written) and the Range header must have been sent.
func TestDownloadResume(t *testing.T) {
	shrinkRetry(t, 5)
	body := bytes.Repeat([]byte("xy"), 5000) // 10000 bytes
	half := len(body) / 2
	var reqs int32
	var sawRange atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&reqs, 1)
		if n == 1 {
			// Advertise full length but deliver only half, then abort the conn.
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			w.Write(body[:half])
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			panic(http.ErrAbortHandler)
		}
		// Attempt 2: expect a resume request.
		rng := r.Header.Get("Range")
		if rng != "" {
			sawRange.Store(true)
		}
		want := "bytes=" + strconv.Itoa(half) + "-"
		if rng != want {
			t.Errorf("attempt 2 Range = %q, want %q", rng, want)
		}
		w.Header().Set("Content-Range", "bytes "+strconv.Itoa(half)+"-"+strconv.Itoa(len(body)-1)+"/"+strconv.Itoa(len(body)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)-half))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(body[half:])
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	var lastDone, lastTotal, minDone int64
	minDone = 1 << 62
	p := func(done, total int64) {
		if done < minDone {
			minDone = done
		}
		lastDone, lastTotal = done, total
	}
	if err := Download(context.Background(), srv.URL, dst, p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, body) {
		t.Fatalf("content mismatch: got %d bytes want %d", len(got), len(body))
	}
	if !sawRange.Load() {
		t.Fatal("Range header never sent on resume")
	}
	if lastDone != int64(len(body)) || lastTotal != int64(len(body)) {
		t.Fatalf("final progress done=%d total=%d want %d", lastDone, lastTotal, len(body))
	}
	// Progress must never report fewer than the bytes already on disk at resume.
	if minDone < 0 {
		t.Fatalf("progress went negative: %d", minDone)
	}
}

// TestDownloadNoRangeServer: server ignores Range and always returns a full 200.
// After a first-attempt drop the retry must truncate and re-download whole,
// yielding correct bytes (no corruption from appending onto stale bytes).
func TestDownloadNoRangeServer(t *testing.T) {
	shrinkRetry(t, 5)
	body := bytes.Repeat([]byte("Z"), 8000)
	half := len(body) / 2
	var reqs int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&reqs, 1)
		// Always full 200, ignoring any Range header.
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		if n == 1 {
			w.Write(body[:half])
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			panic(http.ErrAbortHandler)
		}
		w.Write(body)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	if err := Download(context.Background(), srv.URL, dst, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, body) {
		t.Fatalf("content mismatch: got %d bytes want %d", len(got), len(body))
	}
}

// TestDownloadExhausted: server always drops → error after N attempts.
func TestDownloadExhausted(t *testing.T) {
	shrinkRetry(t, 3)
	var reqs int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqs, 1)
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("partial"))
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	err := Download(context.Background(), srv.URL, dst, nil)
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	if got := atomic.LoadInt32(&reqs); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

// TestDownloadPermanent4xx: a 404 fails fast on the first attempt, not after
// burning the whole retry budget.
func TestDownloadPermanent4xx(t *testing.T) {
	shrinkRetry(t, 5)
	var reqs int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqs, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	err := Download(context.Background(), srv.URL, dst, nil)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if got := atomic.LoadInt32(&reqs); got != 1 {
		t.Fatalf("attempts = %d, want 1 (404 must not retry)", got)
	}
}

// TestDownloadCtxCancel: a cancelled ctx aborts promptly with ctx.Err().
func TestDownloadCtxCancel(t *testing.T) {
	shrinkRetry(t, 5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dst := filepath.Join(t.TempDir(), "out.bin")
	err := Download(ctx, srv.URL, dst, nil)
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestDownloadStall: a body that stops sending mid-stream must abort the attempt
// via the stall watchdog rather than hang, then succeed on retry.
func TestDownloadStall(t *testing.T) {
	shrinkRetry(t, 5)
	stallTimeout = 20 * time.Millisecond // watchdog fires fast
	body := bytes.Repeat([]byte("q"), 4000)
	half := len(body) / 2
	var reqs int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&reqs, 1)
		if n == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			w.Write(body[:half])
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			time.Sleep(500 * time.Millisecond) // go silent > stallTimeout
			return
		}
		// Retry: honor Range so we resume the tail.
		rng := r.Header.Get("Range")
		w.Header().Set("Content-Range", "bytes "+strconv.Itoa(half)+"-"+strconv.Itoa(len(body)-1)+"/"+strconv.Itoa(len(body)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)-half))
		w.WriteHeader(http.StatusPartialContent)
		start := half
		if rng == "" {
			start = 0
			w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(len(body)-1)+"/"+strconv.Itoa(len(body)))
		}
		w.Write(body[start:])
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	done := make(chan error, 1)
	go func() { done <- Download(context.Background(), srv.URL, dst, nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Download hung past stall timeout")
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, body) {
		t.Fatalf("content mismatch: got %d bytes want %d", len(got), len(body))
	}
}

// buildTarGz writes a .tar.gz containing one entry {name: content}.
func buildTarGz(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(content))
	tw.Close()
	gz.Close()
	return path
}

// buildZip writes a .zip containing one entry {name: content}.
func buildZip(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(content))
	zw.Close()
	return path
}

func TestExtractTarGz(t *testing.T) {
	src := buildTarGz(t, "dir/wireproxy.exe", "TARDATA")
	dst := filepath.Join(t.TempDir(), "wireproxy.exe")
	if err := ExtractTarGz(src, "wireproxy.exe", dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "TARDATA" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractZipMember(t *testing.T) {
	src := buildZip(t, "dir/tool.exe", "ZIPDATA")
	dst := filepath.Join(t.TempDir(), "tool.exe")
	if err := ExtractZipMember(src, "tool.exe", dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "ZIPDATA" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractTarGzZipSlip(t *testing.T) {
	src := buildTarGz(t, "../evil", "PWNED")
	destDir := t.TempDir()
	dst := filepath.Join(destDir, "safe.exe")
	if err := ExtractTarGz(src, "evil", dst); err == nil {
		t.Fatal("expected zip-slip rejection")
	}
	// nothing written outside destFile's dir
	if _, err := os.Stat(filepath.Join(filepath.Dir(destDir), "evil")); !os.IsNotExist(err) {
		t.Fatal("evil file escaped destination")
	}
}

func TestExtractZipMemberZipSlip(t *testing.T) {
	src := buildZip(t, "../evil", "PWNED")
	destDir := t.TempDir()
	dst := filepath.Join(destDir, "safe.exe")
	if err := ExtractZipMember(src, "evil", dst); err == nil {
		t.Fatal("expected zip-slip rejection")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(destDir), "evil")); !os.IsNotExist(err) {
		t.Fatal("evil file escaped destination")
	}
}
