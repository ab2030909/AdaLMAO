// Package update implements a conservative self-updater: GitHub Releases only, SHA-256 verified,
// refuses anything unverifiable, keeps the old binary on failure. It never runs downloaded scripts.
package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is "owner/name" on GitHub. Override at build time with -ldflags "-X .../update.Repo=owner/name".
var Repo = "ab2030909/AdaLMAO"

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

func apiBase() string {
	if v := os.Getenv("ADALMAO_UPDATE_API"); v != "" { // used by tests
		return v
	}
	return "https://api.github.com"
}

var client = &http.Client{Timeout: 60 * time.Second}

// ErrNoRelease means the repository has no published release.
var ErrNoRelease = errors.New("no releases have been published yet")

// Latest fetches the latest published release.
func Latest() (*Release, error) {
	resp, err := client.Get(apiBase() + "/repos/" + Repo + "/releases/latest")
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoRelease
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	var r Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, err
	}
	if r.Tag == "" {
		return nil, ErrNoRelease
	}
	return &r, nil
}

func parse(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

// Newer reports whether latest is a higher version than current.
func Newer(current, latest string) bool {
	a, b := parse(current), parse(latest)
	for i := range a {
		if b[i] != a[i] {
			return b[i] > a[i]
		}
	}
	return false
}

// AssetName is the release asset expected for this platform.
func AssetName() string {
	n := "adalmao-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return n
}

func allowedURL(raw string) bool {
	if os.Getenv("ADALMAO_UPDATE_API") != "" {
		return true // test server
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "github.com" || strings.HasSuffix(h, ".github.com") || strings.HasSuffix(h, ".githubusercontent.com")
}

func download(rawURL string, limit int64) ([]byte, error) {
	if !allowedURL(rawURL) {
		return nil, fmt.Errorf("refusing to download from %s (only https GitHub hosts are allowed)", rawURL)
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("download too large")
	}
	return b, nil
}

func find(r *Release, name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// ExpectedHash extracts the SHA-256 for name from a checksums.txt body ("<hash>  <name>").
func ExpectedHash(sums, name string) (string, bool) {
	for _, l := range strings.Split(sums, "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// Apply downloads, verifies and installs the release binary over exe. On any failure exe is untouched.
func Apply(r *Release, exe string) (string, error) {
	name := AssetName()
	bin, ok := find(r, name)
	if !ok {
		return "", fmt.Errorf("release %s has no asset %q for this platform", r.Tag, name)
	}
	sumAsset, ok := find(r, "checksums.txt")
	if !ok {
		return "", fmt.Errorf("release %s has no checksums.txt; refusing an unverifiable update", r.Tag)
	}
	sums, err := download(sumAsset.URL, 1<<20)
	if err != nil {
		return "", fmt.Errorf("checksums: %w", err)
	}
	want, ok := ExpectedHash(string(sums), name)
	if !ok {
		return "", fmt.Errorf("checksums.txt has no entry for %s; refusing", name)
	}
	data, err := download(bin.URL, 100<<20)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	if got := hex.EncodeToString(h[:]); got != want {
		return "", fmt.Errorf("checksum mismatch for %s (expected %s, got %s); update aborted", name, want, got)
	}
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return "", err
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("could not move the current binary aside: %w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe) // restore
		_ = os.Remove(tmp)
		return "", fmt.Errorf("could not install the new binary (old one restored): %w", err)
	}
	return old, nil
}
