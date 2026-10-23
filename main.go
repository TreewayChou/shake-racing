// Command racing runs the 百人搖搖競速 shake-racing game server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"racing/internal/kiosk"
	"racing/internal/server"
)

type config struct {
	Port      int
	NoTunnel  bool
	NoKiosk   bool
	PublicURL string
}

func parseFlags(args []string, output io.Writer) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("racing", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.IntVar(&cfg.Port, "port", 3000, "HTTP port to listen on")
	fs.BoolVar(&cfg.NoTunnel, "no-tunnel", false, "LAN only: do not start a Cloudflare quick tunnel")
	fs.BoolVar(&cfg.NoKiosk, "no-kiosk", false, "do not open the host screen in a kiosk browser window")
	fs.StringVar(&cfg.PublicURL, "public-url", "", "public base URL to advertise instead of a tunnel URL")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return config{}, fmt.Errorf("invalid -port %d", cfg.Port)
	}
	return cfg, nil
}

func main() {
	cfg, err := parseFlags(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "racing:", err)
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

func run(cfg config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", listenAddr(cfg))
	if err != nil {
		return err
	}
	srv := server.New(server.Options{})
	srv.SetBotURL("http://" + ln.Addr().String()) // bots take the phones' path, over loopback
	hs := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}

	log.Printf("host screen: http://localhost:%d/host", cfg.Port)
	tun := publishJoinURLs(cfg, srv, func() string { return serveLAN(hs, cfg.Port) })
	if !wantsTunnel(cfg) {
		for _, ip := range lanIPs() {
			log.Printf("players (LAN): http://%s/play", net.JoinHostPort(ip, fmt.Sprint(cfg.Port)))
		}
	}

	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()

	var browser *kiosk.Browser
	var browserExited <-chan struct{}
	if !cfg.NoKiosk {
		if browser = openKiosk(cfg.Port); browser != nil {
			browserExited = browser.Exited()
		}
	}

	reason, serveErr := waitForStop(ctx, browserExited, errc)
	if serveErr != nil {
		shutdown(tun) // never leave cloudflared orphaned
		return serveErr
	}
	log.Printf("shutting down (%s)", reason)
	// Closed in order: kiosk window, tunnel, HTTP server, game server.
	closers := []io.Closer{
		tun,
		closerFunc(func() error {
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return hs.Shutdown(shutCtx)
		}),
		closerFunc(func() error { srv.Close(); return nil }),
	}
	if browser != nil {
		closers = append([]io.Closer{browser}, closers...)
	}
	return shutdown(closers...)
}

// openKiosk opens the host screen in a dedicated full-screen browser window.
// It returns nil, after printing the host URL, when no browser can be started.
func openKiosk(port int) *kiosk.Browser {
	hostURL := fmt.Sprintf("http://127.0.0.1:%d/host", port)
	path, ok := kiosk.FindBrowser(kiosk.DefaultSystem())
	if !ok {
		log.Printf("no Chrome/Chromium/Edge found; open the host screen manually: %s", hostURL)
		return nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	b, err := kiosk.Launch(path, hostURL, filepath.Join(cache, "racing", "kiosk-profile"))
	if err != nil {
		log.Printf("could not start %s (%v); open the host screen manually: %s", path, err, hostURL)
		return nil
	}
	log.Printf("kiosk: %s", path)
	return b
}

// serveLAN starts hs on each LAN address as a fallback when the server was
// bound to loopback only, and returns the first LAN base URL ("" if none).
// It binds each address explicitly because a wildcard bind would clash with
// the existing loopback listener on Linux.
func serveLAN(hs *http.Server, port int) string {
	var first string
	for _, ip := range lanIPs() {
		ln, err := net.Listen("tcp", net.JoinHostPort(ip, fmt.Sprint(port)))
		if err != nil {
			log.Printf("LAN fallback: %v", err)
			continue
		}
		go hs.Serve(ln)
		url := "http://" + ln.Addr().String()
		log.Printf("players (LAN): %s/play", url)
		if first == "" {
			first = url
		}
	}
	return first
}

// lanIPs returns this machine's non-loopback IPv4 addresses.
func lanIPs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []string
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() {
			ips = append(ips, n.IP.String())
		}
	}
	return ips
}
