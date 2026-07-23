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
