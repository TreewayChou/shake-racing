// Package kiosk finds a Chromium-family browser and opens the host screen
// in it as a full-screen app window whose exit the caller can observe.
package kiosk

import (
	"os"
	"os/exec"
	"runtime"
)

// System is the slice of the operating system that browser discovery
// looks at. Tests replace it; DefaultSystem uses the real one.
type System struct {
	GOOS     string
	LookPath func(name string) (string, error)
	Exists   func(path string) bool
	Getenv   func(key string) string
}

// DefaultSystem returns a System backed by the running OS.
func DefaultSystem() System {
	return System{
		GOOS:     runtime.GOOS,
		LookPath: exec.LookPath,
		Exists: func(path string) bool {
			fi, err := os.Stat(path)
			return err == nil && !fi.IsDir()
		},
		Getenv: os.Getenv,
	}
}

// candidate is one browser: the PATH names and install paths that identify it.
type candidate struct {
	names []string
	files []string
}

func candidates(sys System) []candidate {
	switch sys.GOOS {
	case "windows":
		pf, pf86, local := sys.Getenv("ProgramFiles"), sys.Getenv("ProgramFiles(x86)"), sys.Getenv("LOCALAPPDATA")
		under := func(rel string, roots ...string) []string {
			var out []string
			for _, r := range roots {
				if r != "" {
					out = append(out, r+`\`+rel)
				}
			}
			return out
		}
		return []candidate{
			{names: []string{"chrome"}, files: under(`Google\Chrome\Application\chrome.exe`, pf, pf86, local)},
			{names: []string{"chromium"}, files: under(`Chromium\Application\chrome.exe`, local, pf, pf86)},
			{names: []string{"msedge"}, files: under(`Microsoft\Edge\Application\msedge.exe`, pf86, pf, local)},
		}
	case "linux":
		return []candidate{
			{names: []string{"chromium", "chromium-browser"}, files: []string{"/usr/bin/chromium", "/usr/bin/chromium-browser", "/snap/bin/chromium"}},
			{names: []string{"google-chrome", "google-chrome-stable", "chrome"}, files: []string{"/usr/bin/google-chrome", "/opt/google/chrome/chrome"}},
		}
	}
	return nil
}

// FindBrowser returns the first supported browser in preference order:
// Chrome, Chromium, Edge on Windows; Chromium, Chrome on Linux. For each
// browser PATH is searched before the well-known install locations.
func FindBrowser(sys System) (string, bool) {
	for _, c := range candidates(sys) {
		for _, n := range c.names {
			if p, err := sys.LookPath(n); err == nil {
				return p, true
			}
		}
		for _, f := range c.files {
			if sys.Exists(f) {
				return f, true
			}
		}
	}
	return "", false
}

// Args returns the command line that opens url as a full-screen app window
// with audio autoplay allowed. profileDir is a dedicated user-data-dir, so the
// launched process is a separate browser instance owned by us even when the
// user's own browser is already running.
func Args(url, profileDir string) []string {
	return []string{
		"--user-data-dir=" + profileDir,
		"--app=" + url,
		"--start-fullscreen",
		"--autoplay-policy=no-user-gesture-required",
		"--no-first-run",
		"--no-default-browser-check",
	}
}
