package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/apm-go/apm/internal/version"
)

func TestPlatformAssetName(t *testing.T) {
	name := PlatformAssetName()
	if !strings.HasPrefix(name, "apm-go-") {
		t.Errorf("expected prefix apm-go-, got %s", name)
	}
	if !strings.Contains(name, runtime.GOOS) {
		t.Errorf("expected OS %s in %s", runtime.GOOS, name)
	}
	if !strings.Contains(name, runtime.GOARCH) {
		t.Errorf("expected arch %s in %s", runtime.GOARCH, name)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(name, ".exe") {
		t.Errorf("expected .exe suffix on windows, got %s", name)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{11718656, "11.2 MB"},
	}
	for _, tt := range tests {
		got := HumanBytes(tt.input)
		if got != tt.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCheckLatest_DevBuild(t *testing.T) {
	orig := version.Version
	version.Version = "dev"
	defer func() { version.Version = orig }()

	u := &Updater{}
	_, _, err := u.CheckLatest(context.Background())
	if err == nil {
		t.Fatal("expected error for dev build")
	}
	if !strings.Contains(err.Error(), "development builds") {
		t.Errorf("expected development builds error, got: %s", err)
	}
}

func TestCheckLatest_UpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"tag_name":"v1.0.0","prerelease":false,"assets":[]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	orig := version.Version
	version.Version = "1.0.0"
	defer func() { version.Version = orig }()

	u := &Updater{HTTPClient: srv.Client()}

	// Override the API base URL by using a custom repo that routes to the test server.
	// Instead, we'll use a custom transport.
	transport := &rewriteTransport{base: srv.URL}
	u.HTTPClient = &http.Client{Transport: transport}

	result, _, err := u.CheckLatest(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !result.UpToDate {
		t.Errorf("expected up to date, got update available: %s -> %s", result.Current, result.Latest)
	}
}

func TestCheckLatest_UpdateAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"tag_name":"v2.0.0","prerelease":false,"assets":[]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	orig := version.Version
	version.Version = "1.0.0"
	defer func() { version.Version = orig }()

	u := &Updater{HTTPClient: &http.Client{Transport: &rewriteTransport{base: srv.URL}}}

	result, _, err := u.CheckLatest(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if result.UpToDate {
		t.Error("expected update available")
	}
	if result.Latest != "2.0.0" {
		t.Errorf("expected latest 2.0.0, got %s", result.Latest)
	}
}

func TestCheckLatest_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v1.0.0"}`)
	}))
	defer srv.Close()

	orig := version.Version
	version.Version = "1.0.0"
	defer func() { version.Version = orig }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	u := &Updater{HTTPClient: &http.Client{Transport: &rewriteTransport{base: srv.URL}}}

	_, _, err := u.CheckLatest(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestCheckLatest_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	orig := version.Version
	version.Version = "1.0.0"
	defer func() { version.Version = orig }()

	u := &Updater{HTTPClient: &http.Client{Transport: &rewriteTransport{base: srv.URL}}}

	_, _, err := u.CheckLatest(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no releases found") {
		t.Errorf("expected 'no releases found', got: %s", err)
	}
}

func TestApply_FullFlow(t *testing.T) {
	fakeBinary := []byte("#!/bin/sh\necho fake-binary\n")
	hash := sha256.Sum256(fakeBinary)
	hashHex := hex.EncodeToString(hash[:])
	assetName := PlatformAssetName()

	checksumBody := fmt.Sprintf("%s  %s\n", hashHex, assetName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/"+assetName):
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fakeBinary)))
			w.Write(fakeBinary)
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			w.Write([]byte(checksumBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rel := &Release{
		TagName: "v2.0.0",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName, Size: int64(len(fakeBinary))},
			{Name: "SHA256SUMS", BrowserDownloadURL: srv.URL + "/SHA256SUMS"},
		},
	}

	// Create a fake "current binary" to replace.
	tmpDir := t.TempDir()
	currentBinary := filepath.Join(tmpDir, "apm-go")
	if runtime.GOOS == "windows" {
		currentBinary += ".exe"
	}
	os.WriteFile(currentBinary, []byte("old-binary"), 0o755)

	// Make os.Executable return our fake binary path.
	origExec := osExecutable
	osExecutable = func() (string, error) { return currentBinary, nil }
	defer func() { osExecutable = origExec }()

	var progressCalled bool
	u := &Updater{
		HTTPClient: srv.Client(),
		OnProgress: func(read, total int64) {
			progressCalled = true
		},
	}

	binaryPath, err := u.Apply(context.Background(), rel)
	if err != nil {
		t.Fatalf("Apply failed: %s", err)
	}

	if binaryPath != currentBinary {
		t.Errorf("expected binary path %s, got %s", currentBinary, binaryPath)
	}

	data, err := os.ReadFile(currentBinary)
	if err != nil {
		t.Fatalf("read replaced binary: %s", err)
	}
	if string(data) != string(fakeBinary) {
		t.Errorf("binary content mismatch: got %q", string(data))
	}

	if !progressCalled {
		t.Error("progress callback was not called")
	}
}

func TestApply_ChecksumMismatch(t *testing.T) {
	assetName := PlatformAssetName()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/"+assetName):
			w.Write([]byte("real-binary-content"))
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			w.Write([]byte(fmt.Sprintf("0000000000000000000000000000000000000000000000000000000000000000  %s\n", assetName)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rel := &Release{
		TagName: "v2.0.0",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "SHA256SUMS", BrowserDownloadURL: srv.URL + "/SHA256SUMS"},
		},
	}

	tmpDir := t.TempDir()
	currentBinary := filepath.Join(tmpDir, "apm-go")
	if runtime.GOOS == "windows" {
		currentBinary += ".exe"
	}
	os.WriteFile(currentBinary, []byte("old"), 0o755)

	origExec := osExecutable
	osExecutable = func() (string, error) { return currentBinary, nil }
	defer func() { osExecutable = origExec }()

	u := &Updater{HTTPClient: srv.Client()}

	_, err := u.Apply(context.Background(), rel)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected 'checksum mismatch', got: %s", err)
	}
	assertBinary(t, currentBinary, "old")
	assertOnlyEntry(t, tmpDir, filepath.Base(currentBinary))
}

// A rename that crosses directories fails as it does across file systems
// (EXDEV), so the update succeeds only when the new binary is staged beside
// the current one.
func TestApply_StagesBesideBinaryAcrossFileSystems(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows replaces the binary by copying, not by rename")
	}
	origRename := osRename
	osRename = func(src, dst string) error {
		if filepath.Dir(src) != filepath.Dir(dst) {
			return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EXDEV}
		}
		return os.Rename(src, dst)
	}
	defer func() { osRename = origRename }()

	newBinary := []byte("new-binary\n")
	rel, _ := newReleaseServer(t, newBinary, "")
	dir := t.TempDir()
	currentBinary := fakeCurrentBinary(t, dir)

	if _, err := (&Updater{}).Apply(context.Background(), rel); err != nil {
		t.Fatalf("Apply failed: %s", err)
	}
	assertBinary(t, currentBinary, string(newBinary))
	assertOnlyEntry(t, dir, filepath.Base(currentBinary))
	assertMode(t, currentBinary, 0o755)
}

func TestApply_DoesNotUseTMPDIR(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", filepath.Join(dir, "missing-tmpdir"))
	t.Setenv("TMP", filepath.Join(dir, "missing-tmpdir"))
	t.Setenv("TEMP", filepath.Join(dir, "missing-tmpdir"))

	newBinary := []byte("new-binary\n")
	rel, _ := newReleaseServer(t, newBinary, "")
	currentBinary := fakeCurrentBinary(t, dir)

	if _, err := (&Updater{}).Apply(context.Background(), rel); err != nil {
		t.Fatalf("Apply failed: %s", err)
	}
	assertBinary(t, currentBinary, string(newBinary))
	assertOnlyEntry(t, dir, filepath.Base(currentBinary))
	assertMode(t, currentBinary, 0o755)
}

func TestApply_UnwritableBinaryDirFailsBeforeDownload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission bits do not apply on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes to a 0555 directory")
	}
	rel, assetRequests := newReleaseServer(t, []byte("new-binary\n"), "")
	dir := t.TempDir()
	currentBinary := fakeCurrentBinary(t, dir)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	_, err := (&Updater{}).Apply(context.Background(), rel)
	if err == nil {
		t.Fatal("expected an error for an unwritable directory")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error does not name %s: %s", dir, err)
	}
	if n := assetRequests.Load(); n != 0 {
		t.Errorf("asset was requested %d time(s), want 0", n)
	}
	assertBinary(t, currentBinary, "old-binary")
}

// newReleaseServer serves asset and a SHA256SUMS for it (sums overrides the
// file when non-empty) and counts the requests for the asset.
func newReleaseServer(t *testing.T, asset []byte, sums string) (*Release, *atomic.Int32) {
	t.Helper()
	assetName := PlatformAssetName()
	if sums == "" {
		hash := sha256.Sum256(asset)
		sums = fmt.Sprintf("%s  %s\n", hex.EncodeToString(hash[:]), assetName)
	}
	var assetRequests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + assetName:
			assetRequests.Add(1)
			w.Write(asset)
		case "/SHA256SUMS":
			w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Release{
		TagName: "v2.0.0",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName, Size: int64(len(asset))},
			{Name: "SHA256SUMS", BrowserDownloadURL: srv.URL + "/SHA256SUMS"},
		},
	}, &assetRequests
}

// fakeCurrentBinary writes an "old-binary" in dir and makes it the binary
// Apply replaces.
func fakeCurrentBinary(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "apm-go")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	origExec := osExecutable
	osExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { osExecutable = origExec })
	return path
}

func assertBinary(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read binary: %s", err)
	}
	if string(got) != want {
		t.Errorf("binary content = %q, want %q", got, want)
	}
}

func assertOnlyEntry(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != name {
		t.Errorf("%s holds %v, want only [%s]", dir, names, name)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %s = %v, want %v", path, got, want)
	}
}

func TestApply_MissingAsset(t *testing.T) {
	rel := &Release{
		TagName: "v2.0.0",
		Assets: []Asset{
			{Name: "SHA256SUMS", BrowserDownloadURL: "http://example.com/SHA256SUMS"},
		},
	}

	u := &Updater{}
	_, err := u.Apply(context.Background(), rel)
	if err == nil {
		t.Fatal("expected missing asset error")
	}
	if !strings.Contains(err.Error(), "no asset") {
		t.Errorf("expected 'no asset', got: %s", err)
	}
}

func TestApply_DownloadTimeout(t *testing.T) {
	assetName := PlatformAssetName()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			w.Write([]byte(fmt.Sprintf("abcd1234  %s\n", assetName)))
		case strings.HasSuffix(r.URL.Path, "/"+assetName):
			time.Sleep(2 * time.Second)
			w.Write([]byte("late-data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rel := &Release{
		TagName: "v2.0.0",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "SHA256SUMS", BrowserDownloadURL: srv.URL + "/SHA256SUMS"},
		},
	}

	tmpDir := t.TempDir()
	currentBinary := filepath.Join(tmpDir, "apm-go")
	if runtime.GOOS == "windows" {
		currentBinary += ".exe"
	}
	os.WriteFile(currentBinary, []byte("old"), 0o755)

	origExec := osExecutable
	osExecutable = func() (string, error) { return currentBinary, nil }
	defer func() { osExecutable = origExec }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	u := &Updater{HTTPClient: srv.Client()}

	_, err := u.Apply(ctx, rel)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestProgressReader(t *testing.T) {
	data := strings.NewReader("hello world")
	var calls []int64

	pr := &progressReader{
		r:     data,
		total: 11,
		fn: func(read, total int64) {
			calls = append(calls, read)
		},
	}

	buf := make([]byte, 5)
	n, _ := pr.Read(buf)
	if n != 5 {
		t.Errorf("expected 5 bytes, got %d", n)
	}
	n, _ = pr.Read(buf)
	if n != 5 {
		t.Errorf("expected 5 bytes, got %d", n)
	}

	if len(calls) < 2 {
		t.Fatalf("expected at least 2 progress calls, got %d", len(calls))
	}
	if calls[0] != 5 {
		t.Errorf("first call: expected 5, got %d", calls[0])
	}
	if calls[1] != 10 {
		t.Errorf("second call: expected 10, got %d", calls[1])
	}
}

func TestFindAsset(t *testing.T) {
	rel := &Release{
		Assets: []Asset{
			{Name: "apm-go-linux-amd64"},
			{Name: "apm-go-windows-amd64.exe"},
			{Name: "SHA256SUMS"},
		},
	}

	a := findAsset(rel, "apm-go-linux-amd64")
	if a == nil || a.Name != "apm-go-linux-amd64" {
		t.Error("expected to find linux asset")
	}

	a = findAsset(rel, "nonexistent")
	if a != nil {
		t.Error("expected nil for nonexistent asset")
	}
}

// rewriteTransport rewrites all request URLs to go to the test server
// while preserving the path.
type rewriteTransport struct {
	base string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.base, "http://")
	return http.DefaultTransport.RoundTrip(req)
}
