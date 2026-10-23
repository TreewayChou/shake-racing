package main

import (
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"racing/internal/server"
	"racing/internal/tunnel"
)

// tunnelTimeout bounds cloudflared download plus tunnel bring-up.
const tunnelTimeout = 2 * time.Minute

// wantsTunnel reports whether the flags call for a Cloudflare quick tunnel.
func wantsTunnel(cfg config) bool {
	return cfg.PublicURL == "" && !cfg.NoTunnel
}

// listenAddr is the address the HTTP server binds at startup. With a quick
// tunnel, cloudflared is the only network client and reaches us over
// loopback, so we skip listening on the LAN; that also avoids the Windows
// Firewall prompt for every new build. LAN listening starts on tunnel
// failure instead (see publishJoinURLs).
func listenAddr(cfg config) string {
	if wantsTunnel(cfg) {
		return hostPort("127.0.0.1", cfg.Port)
	}
	return fmt.Sprintf(":%d", cfg.Port)
}

// initialURLs decides the join URLs at startup from the flags and the LAN
// base URL. While the tunnel is pending (Mode == TunnelPending, the only
// case wantsTunnel wants one started) the server is loopback only, so no
// LAN URL is advertised.
func initialURLs(cfg config, lan string) server.URLs {
	switch {
	case cfg.PublicURL != "":
		return server.URLs{Public: strings.TrimRight(cfg.PublicURL, "/"), LAN: lan, Mode: server.TunnelCustom}
	case cfg.NoTunnel:
		return server.URLs{LAN: lan, Mode: server.TunnelLAN}
	}
	return server.URLs{Mode: server.TunnelPending}
}

// publishJoinURLs sets srv's join URLs and, unless the flags say otherwise,
// starts a quick tunnel in the background that upgrades them to the public
// URL. On tunnel failure it calls serveLAN to start listening on the LAN and
// falls back to the LAN URL that returns. The returned closer (nil when no
// tunnel was started) stops cloudflared.
func publishJoinURLs(cfg config, srv *server.Server, serveLAN func() string) io.Closer {
	var lan string
	if ips := lanIPs(); len(ips) > 0 {
		lan = "http://" + hostPort(ips[0], cfg.Port)
	}
	urls := initialURLs(cfg, lan)
	srv.SetURLs(urls)
	if urls.Public != "" {
		log.Printf("players:     %s/play", urls.Public)
	}
	if urls.Mode != server.TunnelPending {
		return nil
	}
	log.Print("tunnel: starting Cloudflare quick tunnel…")
	var public string // set by the report, read by lost: same goroutine
	return tunnel.Launch(tunnel.Locator{}, fmt.Sprintf("http://127.0.0.1:%d", cfg.Port), tunnelTimeout,
		func(url string, err error) {
			if err != nil {
				log.Printf("tunnel: %v; falling back to LAN (phones need HTTPS for the G-Sensor)", err)
				srv.SetURLs(server.URLs{LAN: serveLAN(), Mode: server.TunnelFailed})
				return
			}
			public = url
			log.Printf("players:     %s/play", public)
			srv.SetURLs(server.URLs{Public: public, Mode: server.TunnelActive})
		},
		func() {
			// Mid-event there is no safe switch to another URL: warn only.
			log.Print("tunnel: cloudflared exited; restart the program to get a new public URL")
			srv.SetURLs(server.URLs{Public: public, Mode: server.TunnelLost})
		})
}
