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
	"syscall"
	"time"

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

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		return err
	}
	srv := server.New(server.Options{})
	defer srv.Close()
	hs := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}

	// Tunnel and kiosk arrive in later milestones; until then the server
	// always runs LAN-only and headless.
	log.Printf("host screen: http://localhost:%d/host", cfg.Port)
	if cfg.PublicURL != "" {
		log.Printf("players:     %s/play", cfg.PublicURL)
	}
	for _, ip := range lanIPs() {
		log.Printf("players (LAN): http://%s/play", net.JoinHostPort(ip, fmt.Sprint(cfg.Port)))
	}

	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Print("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return hs.Shutdown(shutCtx)
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
