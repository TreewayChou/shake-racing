package tunnel_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"racing/internal/tunnel"
)

// The test binary doubles as a fake cloudflared: with FAKE_CLOUDFLARED set
// it replays a captured log and then behaves as that mode says.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_CLOUDFLARED"); mode != "" {
		os.Exit(fakeCloudflared(mode))
	}
	os.Exit(m.Run())
}

func fakeCloudflared(mode string) int {
	want := "tunnel --no-autoupdate --url http://localhost:3000"
	if got := strings.Join(os.Args[1:], " "); got != want {
		fmt.Fprintf(os.Stderr, "fake cloudflared: args %q, want %q\n", got, want)
		return 2
	}
	switch mode {
	case "ok":
		log, _ := os.ReadFile("testdata/quick-tunnel.log")
		os.Stderr.Write(log)
		select {} // run until killed
	case "drop": // up, then the connection dies
		log, _ := os.ReadFile("testdata/quick-tunnel.log")
		os.Stderr.Write(log)
		time.Sleep(200 * time.Millisecond)
		return 1
	case "fail":
		log, _ := os.ReadFile("testdata/quick-tunnel-failed.log")
		os.Stderr.Write(log)
		return 1
	case "silent":
		select {}
	}
	return 3
}

func startFake(t *testing.T, mode string, timeout time.Duration) (*tunnel.Tunnel, error) {
	t.Helper()
	t.Setenv("FAKE_CLOUDFLARED", mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return tunnel.Start(ctx, exe, "http://localhost:3000")
}

func TestStartReportsURLAndStopEndsProcess(t *testing.T) {
	tun, err := startFake(t, "ok", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://boundary-metals-painted-totally.trycloudflare.com"; tun.URL() != want {
		t.Errorf("URL = %q, want %q", tun.URL(), want)
	}
	select {
	case <-tun.Done():
		t.Fatal("cloudflared exited before Stop")
	default:
	}
	tun.Stop()
	select {
	case <-tun.Done():
	default:
		t.Fatal("Stop returned while cloudflared still running")
	}
	tun.Stop() // idempotent
}

func TestStartFailsWhenCloudflaredExitsWithoutURL(t *testing.T) {
	tun, err := startFake(t, "fail", 10*time.Second)
	if err == nil {
		tun.Stop()
		t.Fatal("Start succeeded, want error")
	}
	if !strings.Contains(err.Error(), "api.trycloudflare.com") {
		t.Errorf("error %q does not carry cloudflared's last output", err)
	}
}

// fakeLaunch is a Launch of the fake cloudflared and what it reported.
type fakeLaunch struct {
	bg   *tunnel.Background
	urls chan string
	errs chan error
	lost chan struct{}
}

func launchFake(t *testing.T, mode string) fakeLaunch {
	t.Helper()
	t.Setenv("FAKE_CLOUDFLARED", mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	loc := tunnel.Locator{LookPath: func(string) (string, error) { return exe, nil }}
	f := fakeLaunch{urls: make(chan string, 1), errs: make(chan error, 1), lost: make(chan struct{}, 1)}
	f.bg = tunnel.Launch(loc, "http://localhost:3000", 10*time.Second, func(url string, err error) {
		if err != nil {
			f.errs <- err
		} else {
			f.urls <- url
		}
	}, func() { f.lost <- struct{}{} })
	return f
}

func TestLaunchReportsURLAndCloseStopsCloudflared(t *testing.T) {
	f := launchFake(t, "ok")
	select {
	case u := <-f.urls:
		if want := "https://boundary-metals-painted-totally.trycloudflare.com"; u != want {
			t.Errorf("reported %q, want %q", u, want)
		}
	case err := <-f.errs:
		t.Fatal(err)
	}
	var c io.Closer = f.bg
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.bg.Tunnel().Done():
	default:
		t.Fatal("Close returned while cloudflared still running")
	}
	select {
	case <-f.lost:
		t.Error("stopping the tunnel on Close reported it lost")
	default:
	}
}

func TestLaunchReportsFailure(t *testing.T) {
	f := launchFake(t, "fail")
	defer f.bg.Close()
	if err := <-f.errs; err == nil {
		t.Fatal("want failure report")
	}
}

func TestLaunchReportsTunnelLostWhenCloudflaredExitsAfterTheURL(t *testing.T) {
	f := launchFake(t, "drop")
	defer f.bg.Close()
	select {
	case <-f.urls:
	case err := <-f.errs:
		t.Fatal(err)
	}
	select {
	case <-f.lost:
	case <-time.After(10 * time.Second):
		t.Fatal("cloudflared exited but the tunnel was not reported lost")
	}
}

func TestStartGivesUpAndKillsCloudflaredAtDeadline(t *testing.T) {
	if _, err := startFake(t, "silent", 500*time.Millisecond); err == nil {
		t.Fatal("Start succeeded, want deadline error")
	}
}
