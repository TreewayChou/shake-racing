package kiosk

import (
	"errors"
	"slices"
	"testing"
)

// fakeSystem builds a System where only the given PATH names and files exist.
func fakeSystem(goos string, onPath map[string]string, files ...string) System {
	env := map[string]string{
		"ProgramFiles":      `C:\Program Files`,
		"ProgramFiles(x86)": `C:\Program Files (x86)`,
		"LOCALAPPDATA":      `C:\Users\u\AppData\Local`,
	}
	return System{
		GOOS: goos,
		LookPath: func(name string) (string, error) {
			if p, ok := onPath[name]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		Exists: func(path string) bool { return slices.Contains(files, path) },
		Getenv: func(k string) string { return env[k] },
	}
}

func TestFindWindowsPrefersChromeOverEdge(t *testing.T) {
	sys := fakeSystem("windows", nil,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Google\Chrome\Application\chrome.exe`)
	got, ok := FindBrowser(sys)
	if !ok || got != `C:\Program Files\Google\Chrome\Application\chrome.exe` {
		t.Errorf("FindBrowser = %q, %v; want Chrome", got, ok)
	}
}

func TestFindWindowsChromeInLocalAppData(t *testing.T) {
	sys := fakeSystem("windows", nil, `C:\Users\u\AppData\Local\Google\Chrome\Application\chrome.exe`)
	got, ok := FindBrowser(sys)
	if !ok || got != `C:\Users\u\AppData\Local\Google\Chrome\Application\chrome.exe` {
		t.Errorf("FindBrowser = %q, %v", got, ok)
	}
}

func TestFindWindowsChromiumBeforeEdge(t *testing.T) {
	sys := fakeSystem("windows", nil,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Users\u\AppData\Local\Chromium\Application\chrome.exe`)
	got, _ := FindBrowser(sys)
	if got != `C:\Users\u\AppData\Local\Chromium\Application\chrome.exe` {
		t.Errorf("FindBrowser = %q, want Chromium", got)
	}
}

func TestFindWindowsEdgeFallback(t *testing.T) {
	sys := fakeSystem("windows", nil, `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`)
	got, ok := FindBrowser(sys)
	if !ok || got != `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe` {
		t.Errorf("FindBrowser = %q, %v; want Edge", got, ok)
	}
}

func TestFindWindowsOnPath(t *testing.T) {
	sys := fakeSystem("windows", map[string]string{"msedge": `D:\edge\msedge.exe`})
	got, ok := FindBrowser(sys)
	if !ok || got != `D:\edge\msedge.exe` {
		t.Errorf("FindBrowser = %q, %v", got, ok)
	}
}

func TestFindLinuxPrefersChromiumOnPath(t *testing.T) {
	sys := fakeSystem("linux", map[string]string{
		"google-chrome":    "/usr/bin/google-chrome",
		"chromium-browser": "/usr/bin/chromium-browser",
	})
	got, ok := FindBrowser(sys)
	if !ok || got != "/usr/bin/chromium-browser" {
		t.Errorf("FindBrowser = %q, %v; want chromium-browser", got, ok)
	}
}

func TestFindLinuxChromeFallbackAndSnap(t *testing.T) {
	sys := fakeSystem("linux", map[string]string{"google-chrome-stable": "/opt/google/chrome/chrome"})
	if got, _ := FindBrowser(sys); got != "/opt/google/chrome/chrome" {
		t.Errorf("FindBrowser = %q", got)
	}
	sys = fakeSystem("linux", nil, "/snap/bin/chromium")
	if got, _ := FindBrowser(sys); got != "/snap/bin/chromium" {
		t.Errorf("FindBrowser = %q, want snap chromium", got)
	}
}

func TestFindNothing(t *testing.T) {
	for _, goos := range []string{"windows", "linux", "darwin"} {
		if got, ok := FindBrowser(fakeSystem(goos, nil)); ok {
			t.Errorf("%s: FindBrowser = %q, want none", goos, got)
		}
	}
}

func TestArgs(t *testing.T) {
	got := Args("http://127.0.0.1:3000/host", `C:\cache\racing\kiosk`)
	for _, want := range []string{
		`--user-data-dir=C:\cache\racing\kiosk`,
		"--app=http://127.0.0.1:3000/host",
		"--start-fullscreen",
		"--autoplay-policy=no-user-gesture-required",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("Args missing %q: %v", want, got)
		}
	}
}
