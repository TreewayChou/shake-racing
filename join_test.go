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
		{"default starts a tunnel and hides the loopback-only LAN URL", config{Port: 3000},
			server.URLs{Mode: server.ModePending}, true},
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

// Only modes where phones or a proxy reach the server over the network
// listen on all interfaces; the quick tunnel connects via loopback, which
// keeps the Windows Firewall "allow access" prompt from appearing.
func TestListenAddr(t *testing.T) {
	cases := []struct {
		name string
		cfg  config
		want string
	}{
		{"tunnel mode is loopback only", config{Port: 3000}, "127.0.0.1:3000"},
		{"-no-tunnel listens on all interfaces", config{Port: 3000, NoTunnel: true}, ":3000"},
		{"-public-url listens on all interfaces", config{Port: 3000, PublicURL: "https://game.example.com"}, ":3000"},
	}
	for _, c := range cases {
		if got := listenAddr(c.cfg); got != c.want {
			t.Errorf("%s: listenAddr = %q; want %q", c.name, got, c.want)
		}
	}
}
