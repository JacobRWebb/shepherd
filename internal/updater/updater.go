// Package updater implements `shepherd update`: it checks the latest GitHub
// release, downloads the matching release archive, verifies its checksum, and
// atomically replaces the running executable.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

var httpClient = &http.Client{Timeout: 60 * time.Second}

// Result describes the outcome of an update check/run.
type Result struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	UpToDate  bool   `json:"up_to_date"`
	Updated   bool   `json:"updated"`
	Asset     string `json:"asset,omitempty"`
	Path      string `json:"path,omitempty"`
	CheckOnly bool   `json:"check_only"`
}

// Update checks repo's latest release against current and, unless checkOnly,
// installs it over the running binary.
func Update(ctx context.Context, repo, current string, checkOnly bool) (Result, error) {
	tag, err := latestTag(ctx, repo)
	if err != nil {
		return Result{}, err
	}
	latest := strings.TrimPrefix(tag, "v")
	res := Result{Current: current, Latest: latest, CheckOnly: checkOnly}

	if !shouldUpdate(current, latest) {
		res.UpToDate = true
		return res, nil
	}
	if checkOnly {
		return res, nil
	}

	asset := assetName(latest)
	res.Asset = asset
	archiveURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, asset)

	archive, err := download(ctx, archiveURL)
	if err != nil {
		return res, err
	}
	if want, _ := expectedChecksum(ctx, repo, tag, asset); want != "" {
		got := fmt.Sprintf("%x", sha256.Sum256(archive))
		if !strings.EqualFold(got, want) {
			return res, fmt.Errorf("checksum mismatch for %s (got %s, want %s)", asset, got, want)
		}
	}

	bin, err := extractBinary(archive, strings.HasSuffix(asset, ".zip"))
	if err != nil {
		return res, err
	}

	exe, err := os.Executable()
	if err != nil {
		return res, err
	}
	if err := selfupdate.Apply(bytes.NewReader(bin), selfupdate.Options{}); err != nil {
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return res, fmt.Errorf("update failed and rollback failed: %v (rollback: %v)", err, rerr)
		}
		return res, fmt.Errorf("update failed (rolled back): %w", err)
	}
	res.Updated = true
	res.Path = exe
	return res, nil
}

func shouldUpdate(current, latest string) bool {
	current = strings.TrimPrefix(current, "v")
	if current == "" || current == "dev" {
		return true
	}
	return current != latest
}

func assetName(version string) string {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("shepherd_%s_%s_%s.%s", version, runtime.GOOS, runtime.GOARCH, ext)
}

func latestTag(ctx context.Context, repo string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%s has no published releases yet", repo)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("github API returned %s", resp.Status)
	}
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if r.TagName == "" {
		return "", fmt.Errorf("latest release has no tag")
	}
	return r.TagName, nil
}

func expectedChecksum(ctx context.Context, repo, tag, asset string) (string, error) {
	data, err := download(ctx, fmt.Sprintf("https://github.com/%s/releases/download/%s/checksums.txt", repo, tag))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == asset {
			return f[0], nil
		}
	}
	return "", nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func extractBinary(archive []byte, isZip bool) ([]byte, error) {
	if isZip {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if isBinaryName(f.Name) {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer func() { _ = rc.Close() }()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("shepherd binary not found in archive")
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if isBinaryName(h.Name) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("shepherd binary not found in archive")
}

func isBinaryName(name string) bool {
	base := name[strings.LastIndexAny(name, `/\`)+1:]
	return base == "shepherd" || base == "shepherd.exe"
}

// CheckInfo is the result of a cached update check.
type CheckInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
}

type checkCache struct {
	LatestTag string    `json:"latest_tag"`
	CheckedAt time.Time `json:"checked_at"`
}

const checkTTL = 24 * time.Hour

// CachedCheck returns whether a newer release is available, using a 24h on-disk
// cache to avoid hitting the network on every invocation. It is best-effort:
// any error yields Available=false and never surfaces. Dev builds are never
// flagged as out of date.
func CachedCheck(ctx context.Context, repo, current string) CheckInfo {
	info := CheckInfo{Current: current}
	cache := readCheckCache()
	latest := cache.LatestTag
	if cache.LatestTag == "" || time.Since(cache.CheckedAt) > checkTTL {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if tag, err := latestTag(cctx, repo); err == nil {
			latest = tag
			writeCheckCache(checkCache{LatestTag: tag, CheckedAt: time.Now()})
		}
	}
	if latest == "" {
		return info
	}
	lv := strings.TrimPrefix(latest, "v")
	info.Latest = lv
	cur := strings.TrimPrefix(current, "v")
	// Only nag clean release versions that differ from latest. Skip dev builds
	// and pseudo/pre-release/dirty versions (which contain "-" or "+") so a
	// local build isn't told to "update" to an older tag.
	if cur != "" && cur != "dev" && !strings.ContainsAny(cur, "-+") && cur != lv {
		info.Available = true
	}
	return info
}

func checkCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "shepherd", "update-check.json")
}

func readCheckCache() checkCache {
	var c checkCache
	p := checkCachePath()
	if p == "" {
		return c
	}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func writeCheckCache(c checkCache) {
	p := checkCachePath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	if b, err := json.Marshal(c); err == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}
