package server_test

import (
	"bytes"
	"image/png"
	"io"
	"net/http"
	"testing"

	qrcode "github.com/skip2/go-qrcode"

	"racing/internal/server"
	"racing/internal/servertest"
)

func TestHostReceivesURLsOnConnectAndOnChange(t *testing.T) {
	h := servertest.Start(t)
	h.Server.SetURLs(server.URLs{LAN: "http://192.168.1.5:3000", Mode: server.ModePending})
	host := h.Host()

	got := host.ExpectURLs()
	want := servertest.URLs{LAN: "http://192.168.1.5:3000/play", TunnelMode: "pending"}
	if got != want {
		t.Errorf("urls on connect = %+v, want %+v", got, want)
	}

	h.Server.SetURLs(server.URLs{Public: "https://abc-def.trycloudflare.com", LAN: "http://192.168.1.5:3000", Mode: server.ModeTunnel})
	got = host.ExpectURLs()
	want = servertest.URLs{Public: "https://abc-def.trycloudflare.com/play", LAN: "http://192.168.1.5:3000/play", TunnelMode: "tunnel"}
	if got != want {
		t.Errorf("urls after tunnel up = %+v, want %+v", got, want)
	}
}

func TestHostIsToldWhenTheTunnelIsLost(t *testing.T) {
	h := servertest.Start(t)
	h.Server.SetURLs(server.URLs{Public: "https://abc-def.trycloudflare.com", Mode: server.ModeTunnel})
	host := h.Host()
	host.ExpectURLs()

	h.Server.SetURLs(server.URLs{Public: "https://abc-def.trycloudflare.com", Mode: server.ModeLost})
	got := host.ExpectURLs()
	if want := (servertest.URLs{Public: "https://abc-def.trycloudflare.com/play", TunnelMode: "lost"}); got != want {
		t.Errorf("urls after the tunnel is lost = %+v, want %+v", got, want)
	}
}

// qrOf is the PNG the QR endpoint should serve for a join URL.
func qrOf(t *testing.T, url string) []byte {
	t.Helper()
	b, err := qrcode.Encode(url, qrcode.Medium, 512)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func getQR(t *testing.T, h *servertest.Harness) (*http.Response, []byte) {
	t.Helper()
	resp := h.Get("/qr.png", nil)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func TestQREncodesPublicPlayURL(t *testing.T) {
	h := servertest.Start(t)
	h.Server.SetURLs(server.URLs{Public: "https://abc-def.trycloudflare.com", LAN: "http://192.168.1.5:3000", Mode: server.ModeTunnel})

	resp, body := getQR(t, h)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("GET /qr.png = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if _, err := png.Decode(bytes.NewReader(body)); err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	if !bytes.Equal(body, qrOf(t, "https://abc-def.trycloudflare.com/play")) {
		t.Error("QR does not encode the public /play URL")
	}
}

func TestQRFallsBackToLANWhenTunnelFails(t *testing.T) {
	h := servertest.Start(t)
	h.Server.SetURLs(server.URLs{LAN: "http://192.168.1.5:3000", Mode: server.ModeFailed})

	_, body := getQR(t, h)
	if !bytes.Equal(body, qrOf(t, "http://192.168.1.5:3000/play")) {
		t.Error("QR does not encode the LAN /play URL")
	}
}

func TestQRUnavailableWithoutAnyURL(t *testing.T) {
	h := servertest.Start(t)
	if resp, _ := getQR(t, h); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("GET /qr.png with no URLs = %d, want 503", resp.StatusCode)
	}
}
