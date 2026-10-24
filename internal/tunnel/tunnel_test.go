package tunnel_test

import (
	"os"
	"testing"

	"racing/internal/tunnel"
)

func TestScanURLFindsQuickTunnelURL(t *testing.T) {
	f, err := os.Open("testdata/quick-tunnel.log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := tunnel.ScanURL(f)
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://boundary-metals-painted-totally.trycloudflare.com"; got != want {
		t.Errorf("ScanURL = %q, want %q", got, want)
	}
}

func TestScanURLFailsWithoutTunnelURL(t *testing.T) {
	f, err := os.Open("testdata/quick-tunnel-failed.log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got, err := tunnel.ScanURL(f); err == nil {
		t.Errorf("ScanURL = %q, want error (api.trycloudflare.com is not a tunnel URL)", got)
	}
}

func TestAssetNameForSupportedPlatforms(t *testing.T) {
	cases := []struct{ goos, goarch, want string }{
		{"windows", "amd64", "cloudflared-windows-amd64.exe"},
		// Cloudflare publishes no windows/arm64 build; Windows on ARM runs amd64.
		{"windows", "arm64", "cloudflared-windows-amd64.exe"},
		{"linux", "amd64", "cloudflared-linux-amd64"},
		{"linux", "arm64", "cloudflared-linux-arm64"},
	}
	for _, c := range cases {
		got, err := tunnel.AssetName(c.goos, c.goarch)
		if err != nil || got != c.want {
			t.Errorf("AssetName(%s, %s) = %q, %v; want %q", c.goos, c.goarch, got, err, c.want)
		}
	}
}

func TestAssetNameRejectsUnsupportedPlatform(t *testing.T) {
	if got, err := tunnel.AssetName("darwin", "arm64"); err == nil {
		t.Errorf("AssetName(darwin, arm64) = %q, want error (release is a .tgz)", got)
	}
}
