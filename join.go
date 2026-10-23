package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"racing/internal/server"
	"racing/internal/tunnel"
)

// tunnelTimeout bounds cloudflared download plus tunnel bring-up.
const tunnelTimeout = 2 * time.Minute

// initialURLs decides the join URLs at startup from the flags and the LAN
// base URL, and whether a quick tunnel should be started.
func initialURLs(cfg config, lan string) (server.URLs, bool) {
	switch {
	case cfg.PublicURL != "":
		return server.URLs{Public: strings.TrimRight(cfg.PublicURL, "/"), LAN: lan, Mode: server.ModeCustom}, false
	case cfg.NoTunnel:
		return server.URLs{LAN: lan, Mode: server.ModeLAN}, false
	}
	return server.URLs{LAN: lan, Mode: server.ModePending}, true
}

// publishJoinURLs sets srv's join URLs and, unless the flags say otherwise,
// starts a quick tunnel in the background that upgrades them to the public
// URL, or falls back to LAN on failure. The returned closer (nil when no
// tunnel was started) stops cloudflared.
func publishJoinURLs(cfg config, srv *server.Server) io.Closer {
	var lan string
	if ips := lanIPs(); len(ips) > 0 {
		lan = "http://" + net.JoinHostPort(ips[0], fmt.Sprint(cfg.Port))
	}
	urls, wantTunnel := initialURLs(cfg, lan)
	srv.SetURLs(urls)
	if urls.Public != "" {
		log.Printf("players:     %s/play", urls.Public)
	}
	if !wantTunnel {
		return nil
	}
	log.Print("tunnel: starting Cloudflare quick tunnel…")
	return tunnel.Launch(tunnel.Locator{}, fmt.Sprintf("http://127.0.0.1:%d", cfg.Port), tunnelTimeout,
		func(public string, err error) {
			if err != nil {
				log.Printf("tunnel: %v; falling back to LAN (phones need HTTPS for the G-Sensor)", err)
				srv.SetURLs(server.URLs{LAN: lan, Mode: server.ModeFailed})
				return
			}
			log.Printf("players:     %s/play", public)
			srv.SetURLs(server.URLs{Public: public, LAN: lan, Mode: server.ModeTunnel})
		})
}
