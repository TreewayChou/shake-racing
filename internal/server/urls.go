package server

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// TunnelMode says how the public URL was obtained.
type TunnelMode string

const (
	TunnelPending TunnelMode = "pending" // quick tunnel still starting
	TunnelActive  TunnelMode = "tunnel"  // public URL is a quick tunnel
	TunnelCustom  TunnelMode = "custom"  // public URL came from -public-url
	TunnelLAN     TunnelMode = "lan"     // -no-tunnel: LAN only, by choice
	TunnelFailed  TunnelMode = "failed"  // tunnel failed: LAN fallback
	TunnelLost    TunnelMode = "lost"    // tunnel was up, then cloudflared exited: warn only
)

// URLs are the base URLs (scheme://host[:port], no path) players can reach.
type URLs struct {
	Public string
	LAN    string
	Mode   TunnelMode
}

// QRSize is the side length, in pixels, of the join QR code.
const QRSize = 512

// SetURLs publishes the join URLs: hosts are sent a urls message and the
// QR code at /qr.png switches to the new join URL (public, else LAN).
func (s *Server) SetURLs(u URLs) {
	msg := urlsMsg{Type: "urls", Public: playURL(u.Public), LAN: playURL(u.LAN), TunnelMode: string(u.Mode)}
	join := msg.Public
	if join == "" {
		join = msg.LAN
	}
	var qr []byte
	if join != "" {
		var err error
		if qr, err = qrcode.Encode(join, qrcode.Medium, QRSize); err != nil {
			log.Printf("server: QR for %q: %v", join, err)
		}
	}
	b := mustJSON(msg)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.urls, s.qr = b, qr
	for h := range s.hosts {
		h.send(b)
	}
}

func playURL(base string) string {
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/play"
}

// urlsLocked is the current urls message.
func (s *Server) urlsLocked() []byte {
	if s.urls == nil {
		return mustJSON(urlsMsg{Type: "urls"})
	}
	return s.urls
}

func (s *Server) serveQR(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	qr := s.qr
	s.mu.Unlock()
	if qr == nil {
		http.Error(w, "no join URL yet", http.StatusServiceUnavailable)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", "image/png")
	hd.Set("Cache-Control", "no-store")
	hd.Set("Content-Length", strconv.Itoa(len(qr)))
	w.Write(qr)
}
