// Package tunnel manages a Cloudflare quick tunnel: it finds or downloads
// cloudflared, runs it against the local port, reports the public
// https://*.trycloudflare.com URL, and stops it on shutdown.
package tunnel

import (
	"bufio"
	"errors"
	"io"
	"regexp"
)

// ErrNoURL means cloudflared's output ended without a tunnel URL.
var ErrNoURL = errors.New("tunnel: cloudflared exited without a trycloudflare URL")

// quickURL matches a quick-tunnel hostname. Cloudflare's own API host
// (api.trycloudflare.com) appears in error output and is excluded below.
var quickURL = regexp.MustCompile(`https://([a-z0-9-]+)\.trycloudflare\.com`)

// ScanURL reads cloudflared log output until it finds the quick-tunnel URL.
func ScanURL(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if u, ok := matchURL(sc.Text()); ok {
			return u, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", ErrNoURL
}

func matchURL(line string) (string, bool) {
	for _, m := range quickURL.FindAllStringSubmatch(line, -1) {
		if m[1] != "api" {
			return m[0], true
		}
	}
	return "", false
}
