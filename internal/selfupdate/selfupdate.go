package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/apm-go/apm/internal/semver"
	"github.com/apm-go/apm/internal/version"
)

const (
	defaultRepo     = "gn00678465/apm-go"
	apiBaseURL      = "https://api.github.com"
	binaryName      = "apm-go"
	oldBinarySuffix = ".old"
)

var osExecutable = os.Executable

// Release is a GitHub release.
type Release struct {
	TagName    string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset is a downloadable file attached to a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// UpdateResult is the outcome of a self-update check or apply.
type UpdateResult struct {
	Current  string
	Latest   string
	UpToDate bool
}

// ProgressFunc is called during download with bytes read and total size.
// total is -1 when Content-Length is unknown.
type ProgressFunc func(bytesRead, total int64)

// Updater checks for and applies self-updates from GitHub Releases.
type Updater struct {
	Repo       string
	HTTPClient *http.Client
	OnProgress ProgressFunc
}

// CheckLatest queries GitHub for the latest stable release and compares
// it against the running binary's version.
func (u *Updater) CheckLatest(ctx context.Context) (*UpdateResult, *Release, error) {
	current := version.Version
	if current == "dev" {
		return nil, nil, fmt.Errorf("self-update is not available for development builds; use install.sh or install.ps1")
	}

	rel, err := u.fetchLatestRelease(ctx)
	if err != nil {
		return nil, nil, err
	}

	latest := semver.StripVPrefix(rel.TagName)
	cmp := semver.CompareVersions(current, latest)

	return &UpdateResult{
		Current:  current,
		Latest:   latest,
		UpToDate: cmp >= 0,
	}, rel, nil
}

// Apply downloads the release asset for this platform, verifies its
// SHA256 checksum, and replaces the running binary.
func (u *Updater) Apply(ctx context.Context, rel *Release) (string, error) {
	assetName := PlatformAssetName()
	asset := findAsset(rel, assetName)
	if asset == nil {
		return "", fmt.Errorf("release %s has no asset %q for this platform", rel.TagName, assetName)
	}

	checksumAsset := findAsset(rel, "SHA256SUMS")
	if checksumAsset == nil {
		return "", fmt.Errorf("release %s has no SHA256SUMS asset", rel.TagName)
	}

	expectedHash, err := u.fetchExpectedHash(ctx, checksumAsset.BrowserDownloadURL, assetName)
	if err != nil {
		return "", err
	}

	tmpDir, err := os.MkdirTemp("", "apm-go-self-update-*")
	if err != nil {
		return "", fmt.Errorf("create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpBinary := filepath.Join(tmpDir, assetName)
	actualHash, err := u.downloadAsset(ctx, asset.BrowserDownloadURL, asset.Size, tmpBinary)
	if err != nil {
		return "", err
	}

	if actualHash != expectedHash {
		return "", fmt.Errorf("SHA256 checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	currentBinary, err := osExecutable()
	if err != nil {
		return "", fmt.Errorf("locate current binary: %w", err)
	}
	currentBinary, err = filepath.EvalSymlinks(currentBinary)
	if err != nil {
		return "", fmt.Errorf("resolve current binary path: %w", err)
	}

	if err := replaceBinary(currentBinary, tmpBinary); err != nil {
		return "", err
	}

	return currentBinary, nil
}

func (u *Updater) client() *http.Client {
	if u.HTTPClient != nil {
		return u.HTTPClient
	}
	return http.DefaultClient
}

func (u *Updater) repo() string {
	if u.Repo != "" {
		return u.Repo
	}
	return defaultRepo
}

func (u *Updater) fetchLatestRelease(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", apiBaseURL, u.repo())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := u.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases found for %s", u.repo())
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return &rel, nil
}

func (u *Updater) fetchExpectedHash(ctx context.Context, url, assetName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create checksum request: %w", err)
	}

	resp, err := u.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("download SHA256SUMS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download SHA256SUMS: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read SHA256SUMS: %w", err)
	}

	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == assetName {
			return strings.ToLower(parts[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum entry for %s in SHA256SUMS", assetName)
}

func (u *Updater) downloadAsset(ctx context.Context, url string, size int64, dest string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}

	resp, err := u.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download binary: HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength
	if total <= 0 && size > 0 {
		total = size
	}

	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	var reader io.Reader = resp.Body
	if u.OnProgress != nil {
		reader = &progressReader{r: resp.Body, total: total, fn: u.OnProgress}
	}
	reader = io.TeeReader(reader, hasher)

	if _, err := io.Copy(f, reader); err != nil {
		return "", fmt.Errorf("download binary: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// PlatformAssetName returns the expected asset filename for this OS/arch.
func PlatformAssetName() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("%s-%s-%s%s", binaryName, runtime.GOOS, runtime.GOARCH, ext)
}

func findAsset(rel *Release, name string) *Asset {
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i]
		}
	}
	return nil
}

// progressReader wraps an io.Reader and calls fn after each Read.
type progressReader struct {
	r     io.Reader
	read  int64
	total int64
	fn    ProgressFunc
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.read += int64(n)
	pr.fn(pr.read, pr.total)
	return n, err
}
