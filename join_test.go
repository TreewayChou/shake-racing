package main

import (
	"testing"

	"racing/internal/server"
)

func TestInitialURLs(t *testing.T) {
	lan := "http://192.168.1.5:3000"
	cases := []struct {
		name       string
		cfg        config
		want       server.URLs
		wantTunnel bool
	}{
		{"default starts a tunnel", config{Port: 3000},
			server.URLs{LAN: lan, Mode: server.ModePending}, true},
		{"-public-url overrides the tunnel", config{Port: 3000, PublicURL: "https://game.example.com/"},
			server.URLs{Public: "https://game.example.com", LAN: lan, Mode: server.ModeCustom}, false},
		{"-public-url wins over -no-tunnel", config{Port: 3000, NoTunnel: true, PublicURL: "https://game.example.com"},
			server.URLs{Public: "https://game.example.com", LAN: lan, Mode: server.ModeCustom}, false},
		{"-no-tunnel is LAN mode", config{Port: 3000, NoTunnel: true},
			server.URLs{LAN: lan, Mode: server.ModeLAN}, false},
	}
	for _, c := range cases {
		got, tunnel := initialURLs(c.cfg, lan)
		if got != c.want || tunnel != c.wantTunnel {
			t.Errorf("%s: initialURLs = %+v, %v; want %+v, %v", c.name, got, tunnel, c.want, c.wantTunnel)
		}
	}
}
