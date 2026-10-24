package tunnel_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"racing/internal/tunnel"
)

func notOnPath(string) (string, error) { return "", exec.ErrNotFound }

// releaseServer serves fake release assets and counts downloads.
func releaseServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cloudflared-linux-arm64" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		w.Write([]byte("fake cloudflared binary"))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestLocatePrefersPath(t *testing.T) {
	l := tunnel.Locator{
		LookPath: func(name string) (string, error) {
			if name != "cloudflared" {
				t.Errorf("LookPath(%q), want cloudflared", name)
			}
			return "/usr/bin/cloudflared", nil
		},
		ExeDir:   t.TempDir(),
		CacheDir: t.TempDir(),
	}
	got, err := l.Locate(context.Background())
	if err != nil || got != "/usr/bin/cloudflared" {
		t.Errorf("Locate = %q, %v; want /usr/bin/cloudflared", got, err)
	}
}

func TestLocateFindsBinaryNextToExecutable(t *testing.T) {
	exeDir := t.TempDir()
	adjacent := filepath.Join(exeDir, "cloudflared.exe")
	if err := os.WriteFile(adjacent, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := tunnel.Locator{LookPath: notOnPath, ExeDir: exeDir, CacheDir: t.TempDir(), GOOS: "windows", GOARCH: "amd64"}
	got, err := l.Locate(context.Background())
	if err != nil || got != adjacent {
		t.Errorf("Locate = %q, %v; want %q", got, err, adjacent)
	}
}

func TestLocateDownloadsMatchingAssetOnceAndCachesIt(t *testing.T) {
	srv, hits := releaseServer(t)
	cache := t.TempDir()
	l := tunnel.Locator{
		LookPath: notOnPath, ExeDir: t.TempDir(), CacheDir: cache,
		GOOS: "linux", GOARCH: "arm64", DownloadURL: srv.URL + "/",
	}
	first, err := l.Locate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cache, "cloudflared"); first != want {
		t.Errorf("downloaded to %q, want %q", first, want)
	}
	body, err := os.ReadFile(first)
	if err != nil || string(body) != "fake cloudflared binary" {
		t.Errorf("cached file = %q, %v", body, err)
	}

	second, err := l.Locate(context.Background())
	if err != nil || second != first {
		t.Errorf("second Locate = %q, %v; want cached %q", second, err, first)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("downloads = %d, want 1", n)
	}
}

func TestLocateFailedDownloadLeavesNoCachedFile(t *testing.T) {
	srv, _ := releaseServer(t)
	cache := t.TempDir()
	l := tunnel.Locator{
		LookPath: notOnPath, ExeDir: t.TempDir(), CacheDir: cache,
		GOOS: "linux", GOARCH: "amd64", DownloadURL: srv.URL + "/", // 404
	}
	if _, err := l.Locate(context.Background()); err == nil {
		t.Fatal("Locate succeeded against a 404")
	}
	if _, err := os.Stat(filepath.Join(cache, "cloudflared")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cache holds a binary after failed download: %v", err)
	}
}
