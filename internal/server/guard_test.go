package server_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"racing/internal/servertest"
)

var wsUpgrade = http.Header{
	"Connection":            {"Upgrade"},
	"Upgrade":               {"websocket"},
	"Sec-Websocket-Version": {"13"},
	"Sec-Websocket-Key":     {"dGhlIHNhbXBsZSBub25jZQ=="},
}

func TestHostEndpointsRejectNonLoopback(t *testing.T) {
	h := servertest.Start(t)
	for _, remote := range []string{"192.168.1.50:5000", "203.0.113.9:443", "[2001:db8::1]:80"} {
		for _, tc := range []struct {
			path   string
			header http.Header
		}{{"/host", nil}, {"/ws/host", wsUpgrade}} {
			if code := h.ServeFrom(remote, tc.path, tc.header).Code; code != http.StatusForbidden {
				t.Errorf("%s from %s: status %d, want 403", tc.path, remote, code)
			}
		}
	}
}

func TestHostEndpointsRejectCloudflareForwarded(t *testing.T) {
	h := servertest.Start(t)
	cf := http.Header{"Cf-Connecting-Ip": {"198.51.100.7"}}

	if code := h.Get("/host", cf).StatusCode; code != http.StatusForbidden {
		t.Errorf("/host via Cloudflare: status %d, want 403", code)
	}

	url := "ws" + strings.TrimPrefix(h.URL("/ws/host"), "http")
	_, resp, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{HTTPHeader: cf})
	if err == nil {
		t.Fatal("host WS via Cloudflare: dial succeeded, want 403")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("host WS via Cloudflare: resp %v, want 403", resp)
	}
}

func TestHostEndpointsAllowLoopback(t *testing.T) {
	h := servertest.Start(t)
	for _, remote := range []string{"127.0.0.1:5000", "[::1]:5000"} {
		if code := h.ServeFrom(remote, "/host", nil).Code; code != http.StatusOK {
			t.Errorf("/host from %s: status %d, want 200", remote, code)
		}
	}
	if code := h.Get("/host", nil).StatusCode; code != http.StatusOK {
		t.Errorf("/host over loopback: status %d, want 200", code)
	}
	h.Host() // dial succeeds
}

func TestPlayIsPublic(t *testing.T) {
	h := servertest.Start(t)
	if code := h.ServeFrom("203.0.113.9:443", "/play", http.Header{"Cf-Connecting-Ip": {"203.0.113.9"}}).Code; code != http.StatusOK {
		t.Errorf("/play via Cloudflare: status %d, want 200", code)
	}
}
