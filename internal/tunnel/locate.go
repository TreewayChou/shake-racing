package tunnel

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// DefaultDownloadURL is the base URL of the latest cloudflared release.
const DefaultDownloadURL = "https://github.com/cloudflare/cloudflared/releases/latest/download/"

// AssetName returns the cloudflared GitHub release asset for a platform.
func AssetName(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "windows/amd64", "windows/arm64":
		// No windows/arm64 build is published; Windows on ARM emulates amd64.
		return "cloudflared-windows-amd64.exe", nil
	case "linux/amd64":
		return "cloudflared-linux-amd64", nil
	case "linux/arm64":
		return "cloudflared-linux-arm64", nil
	}
	return "", fmt.Errorf("tunnel: no cloudflared download for %s/%s", goos, goarch)
}

// Locator finds a cloudflared binary: on PATH, next to the executable, in
// the cache directory, or else by downloading the matching release into
// the cache directory. Zero fields take the defaults for this process.
type Locator struct {
	LookPath    func(string) (string, error) // default exec.LookPath
	ExeDir      string                       // default: directory of os.Executable
	CacheDir    string                       // default: os.UserCacheDir()/racing
	GOOS        string                       // default runtime.GOOS
	GOARCH      string                       // default runtime.GOARCH
	DownloadURL string                       // default DefaultDownloadURL
	Client      *http.Client                 // default http.DefaultClient
}

// Locate returns the path of a usable cloudflared binary.
func (l Locator) Locate(ctx context.Context) (string, error) {
	l = l.withDefaults()
	if p, err := l.LookPath("cloudflared"); err == nil {
		return p, nil
	}
	name := "cloudflared"
	if l.GOOS == "windows" {
		name += ".exe"
	}
	for _, dir := range []string{l.ExeDir, l.CacheDir} {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	if l.CacheDir == "" {
		return "", fmt.Errorf("tunnel: cloudflared not found and no cache directory")
	}
	asset, err := AssetName(l.GOOS, l.GOARCH)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(l.CacheDir, name)
	if err := l.download(ctx, l.DownloadURL+asset, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func (l Locator) withDefaults() Locator {
	if l.LookPath == nil {
		l.LookPath = exec.LookPath
	}
	if l.ExeDir == "" {
		if exe, err := os.Executable(); err == nil {
			l.ExeDir = filepath.Dir(exe)
		}
	}
	if l.CacheDir == "" {
		if dir, err := os.UserCacheDir(); err == nil {
			l.CacheDir = filepath.Join(dir, "racing")
		}
	}
	if l.GOOS == "" {
		l.GOOS = runtime.GOOS
	}
	if l.GOARCH == "" {
		l.GOARCH = runtime.GOARCH
	}
	if l.DownloadURL == "" {
		l.DownloadURL = DefaultDownloadURL
	}
	if l.Client == nil {
		l.Client = http.DefaultClient
	}
	return l
}

// download fetches url into dst via a temporary file, so an interrupted
// download never leaves a truncated binary in the cache.
func (l Locator) download(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := l.Client.Do(req)
	if err != nil {
		return fmt.Errorf("tunnel: download cloudflared: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tunnel: download cloudflared: %s: %s", url, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "cloudflared-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, resp.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("tunnel: download cloudflared: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
