package kiosk

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for a browser: when
// KIOSK_FAKE_BROWSER is set it records its args and then exits or hangs.
func TestMain(m *testing.M) {
	switch os.Getenv("KIOSK_FAKE_BROWSER") {
	case "exit":
		if f := os.Getenv("KIOSK_FAKE_ARGS"); f != "" {
			_ = os.WriteFile(f, []byte(strings.Join(os.Args[1:], "\n")), 0o644)
		}
		os.Exit(0)
	case "hang":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeBrowser(t *testing.T, mode string) string {
	t.Helper()
	t.Setenv("KIOSK_FAKE_BROWSER", mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func waitExited(t *testing.T, b *Browser) {
	t.Helper()
	select {
	case <-b.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("Exited() not signalled")
	}
}

func TestLaunchPassesKioskArgsAndSignalsExit(t *testing.T) {
	exe := fakeBrowser(t, "exit")
	argsFile := t.TempDir() + "/args"
	t.Setenv("KIOSK_FAKE_ARGS", argsFile)
	profile := t.TempDir()

	b, err := Launch(exe, "http://127.0.0.1:3000/host", profile)
	if err != nil {
		t.Fatal(err)
	}
	waitExited(t, b)

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--app=http://127.0.0.1:3000/host", "--user-data-dir=" + profile} {
		if !strings.Contains(string(got), want) {
			t.Errorf("browser args %q missing %q", got, want)
		}
	}
}

func TestCloseKillsBrowser(t *testing.T) {
	exe := fakeBrowser(t, "hang")
	b, err := Launch(exe, "http://127.0.0.1:3000/host", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.Exited():
		t.Fatal("exited before Close")
	case <-time.After(100 * time.Millisecond):
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	waitExited(t, b)
	if err := b.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
}

func TestLaunchMissingBinary(t *testing.T) {
	if _, err := Launch(t.TempDir()+"/no-such-browser", "http://x/host", t.TempDir()); err == nil {
		t.Error("want error for missing browser binary")
	}
}
