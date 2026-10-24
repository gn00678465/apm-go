package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/selfupdate"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/version"
)

func TestSelfUpdateCmd_DevBuild(t *testing.T) {
	orig := version.Version
	version.Version = "dev"
	defer func() { version.Version = orig }()

	ux.Init()
	root := buildRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"self-update"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for dev build")
	}
	if !strings.Contains(err.Error(), "development builds") {
		t.Errorf("expected 'development builds' in error, got: %s", err)
	}
}

// Without a terminal, self-update prints the download once and its result
// once, whatever the number of reads (issue #131 item c53-1: before, one
// "Downloading v0.4.0... N / M (P%)" line per read, 1220 lines for an 18.9 MB
// asset).
func TestSelfUpdate_NoTTY_PrintsNoProgressLines(t *testing.T) {
	asset := selfupdate.PlatformAssetName()
	body := bytes.Repeat([]byte("apm-go"), 200_000)
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/gn00678465/apm-go/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.4.0","assets":[{"name":%q,"browser_download_url":%q,"size":%d},{"name":"SHA256SUMS","browser_download_url":%q}]}`,
				asset, "http://"+r.Host+"/dl/"+asset, len(body), "http://"+r.Host+"/dl/SHA256SUMS")
		case "/dl/SHA256SUMS":
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
		case "/dl/" + asset:
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			for off := 0; off < len(body); off += 4096 {
				w.Write(body[off:min(off+4096, len(body))])
				w.(http.Flusher).Flush()
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "apm-go")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := os.WriteFile(binary, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(selfupdate.SetReleasesForTest(srv.URL, binary))
	orig := version.Version
	version.Version = "0.3.0"
	t.Cleanup(func() { version.Version = orig })
	ux.Init()

	var runErr error
	stdout, stderr := captureInitOutput(t, func() { runErr = runSelfUpdate(false) })

	if runErr != nil {
		t.Fatalf("self-update: %v\nstdout:\n%s", runErr, stdout)
	}
	want := " i Checking for updates...\n i Downloading v0.4.0...\n + Updated v0.3.0 -> v0.4.0\n i Binary: " + binary + "\n"
	if stdout != want || stderr != "" {
		t.Errorf("stdout =\n%s\nstderr =\n%s\nwant stdout\n%s", stdout, stderr, want)
	}
	if got, err := os.ReadFile(binary); err != nil || !bytes.Equal(got, body) {
		t.Errorf("binary not replaced with the asset (err %v)", err)
	}
}

func TestSelfUpdateCmd_HelpText(t *testing.T) {
	ux.Init()
	root := buildRootCmd()

	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetArgs([]string{"self-update", "--help"})

	err := root.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Update the apm-go binary") {
		t.Errorf("expected help text to contain 'Update the apm-go binary', got: %s", out)
	}
	if !strings.Contains(out, "--check") {
		t.Errorf("expected help text to contain '--check', got: %s", out)
	}
}
