// Package server is the transport layer: HTTP pages, the phone and host
// WebSocket endpoints, and the 10 Hz host broadcaster. All game rules live
// in package game; the server serialises access to the engine.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"racing/internal/clock"
	"racing/internal/game"
	"racing/internal/web"
)

// BroadcastInterval is how often hosts receive a snapshot.
const BroadcastInterval = 100 * time.Millisecond

// Options configures a Server.
type Options struct {
	// Clock drives the engine and broadcaster. Defaults to the wall clock.
	Clock clock.Clock
}

// Server serves the game over HTTP and WebSocket. Create with New and
// release with Close.
type Server struct {
	mux *http.ServeMux

	clock clock.Clock

	mu      sync.Mutex
	engine  *game.Engine
	hosts   map[*conn]struct{}
	phones  map[*phone]struct{} // joined phones
	pingSeq int64
	urls    []byte // current urls message; nil until SetURLs
	qr      []byte // PNG of the join URL; nil until one is known

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New returns a running server; its broadcaster starts immediately.
func New(opts Options) *Server {
	if opts.Clock == nil {
		opts.Clock = clock.Real()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		clock:  opts.Clock,
		engine: game.New(opts.Clock),
		hosts:  make(map[*conn]struct{}),
		phones: make(map[*phone]struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
	s.mux = http.NewServeMux()
	s.mux.Handle("GET /play", page(playPage(game.DefaultDetector)))
	s.mux.Handle("GET /host", hostOnly(page(web.Host)))
	s.mux.HandleFunc("GET /qr.png", s.serveQR)
	s.mux.HandleFunc("GET /ws/play", s.servePhone)
	s.mux.Handle("GET /ws/host", hostOnly(http.HandlerFunc(s.serveHost)))
	s.mux.Handle("GET /{$}", http.RedirectHandler("/play", http.StatusFound))

	ticker := opts.Clock.NewTicker(BroadcastInterval)
	s.wg.Add(1)
	go s.broadcast(ticker)
	hb := opts.Clock.NewTicker(HeartbeatInterval)
	s.wg.Add(1)
	go s.heartbeat(hb)
	return s
}

// constantsPlaceholder marks where the phone page expects its detector
// constants: `var K=__K__;`.
var constantsPlaceholder = []byte("__K__")

// playPage returns the phone page with the detector constants injected.
func playPage(d game.Detector) []byte {
	if bytes.Count(web.Play, constantsPlaceholder) != 1 {
		panic("server: play.html must contain exactly one __K__ placeholder")
	}
	return bytes.Replace(web.Play, constantsPlaceholder, mustJSON(newDetectorMsg(d)), 1)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// Close stops the broadcaster and closes all WebSocket connections.
func (s *Server) Close() {
	s.cancel()
	s.wg.Wait()
}

func (s *Server) broadcast(t clock.Ticker) {
	defer s.wg.Done()
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C():
			s.mu.Lock()
			msg := s.snapshotLocked() // also advances the round's phases
			s.dispatchLocked()
			hosts := make([]*conn, 0, len(s.hosts))
			for h := range s.hosts {
				hosts = append(hosts, h)
			}
			s.mu.Unlock()
			for _, h := range hosts {
				h.send(msg)
			}
		}
	}
}

func (s *Server) servePhone(w http.ResponseWriter, r *http.Request) {
	c, err := s.accept(w, r)
	if err != nil {
		return
	}
	defer c.close()
	ph := &phone{c: c} // ph.id is set once this connection has joined
	defer func() {
		s.mu.Lock()
		delete(s.phones, ph)
		s.mu.Unlock()
	}()
	for {
		typ, data, err := c.read()
		if err != nil {
			return
		}
		switch typ {
		case "warmup":
			if ph.id != "" {
				s.warmup(ph.id)
			}
		case "join":
			var m joinMsg
			if ph.id != "" || json.Unmarshal(data, &m) != nil {
				continue
			}
			s.mu.Lock()
			p, err := s.engine.Join(m.Name)
			if err == nil {
				ph.id = p.ID
				s.phones[ph] = struct{}{}
				c.send(mustJSON(welcomeMsg{Type: "welcome", PlayerID: p.ID, Constants: newDetectorMsg(game.DefaultDetector)}))
				c.send(mustJSON(newConfigMsg(s.engine.Config())))
				s.startClockSyncLocked(ph)
			}
			s.mu.Unlock()
			if errors.Is(err, game.ErrInvalidName) {
				c.send(mustJSON(errorMsg{Type: "error", Reason: "name"}))
			}
		default:
			if ph.id != "" {
				s.phoneMessage(ph, typ, data)
			}
		}
	}
}

// warmup relays a lobby warm-up shake to every host immediately, outside
// the 10 Hz snapshot cadence, so the player's card pulses right away.
func (s *Server) warmup(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.engine.Warmup(id) {
		return
	}
	msg := mustJSON(warmupMsg{Type: "warmup", ID: id})
	for h := range s.hosts {
		h.send(msg)
	}
}

func (s *Server) serveHost(w http.ResponseWriter, r *http.Request) {
	c, err := s.accept(w, r)
	if err != nil {
		return
	}
	defer c.close()
	s.mu.Lock()
	s.hosts[c] = struct{}{}
	c.send(s.urlsLocked())
	c.send(s.snapshotLocked())
	s.dispatchLocked()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.hosts, c)
		s.mu.Unlock()
	}()
	for {
		typ, data, err := c.read()
		if err != nil {
			return
		}
		s.hostMessage(typ, data)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// conn wraps a WebSocket with a buffered, single-writer outbound queue so
// a slow client never blocks the broadcaster.
type conn struct {
	ws     *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	out    chan []byte
	done   chan struct{}
}

const outQueue = 64

func (s *Server) accept(w http.ResponseWriter, r *http.Request) (*conn, error) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(s.ctx)
	c := &conn{ws: ws, ctx: ctx, cancel: cancel, out: make(chan []byte, outQueue), done: make(chan struct{})}
	go c.writeLoop()
	return c, nil
}

func (c *conn) writeLoop() {
	defer close(c.done)
	for {
		select {
		case <-c.ctx.Done():
			return
		case b := <-c.out:
			if err := c.ws.Write(c.ctx, websocket.MessageText, b); err != nil {
				c.cancel()
				return
			}
		}
	}
}

// send queues b; if the client is too far behind, the message is dropped.
func (c *conn) send(b []byte) {
	select {
	case c.out <- b:
	default:
		log.Printf("server: dropping message to slow client")
	}
}

// read returns the next message's type and raw JSON.
func (c *conn) read() (string, []byte, error) {
	for {
		_, data, err := c.ws.Read(c.ctx)
		if err != nil {
			return "", nil, err
		}
		var env envelope
		if json.Unmarshal(data, &env) != nil || env.Type == "" {
			continue
		}
		if env.Type == "echo" {
			// Round-trip probe: replying from the read loop proves every
			// earlier message on this connection has been handled.
			c.send(mustJSON(echoMsg{Type: "echo", ID: env.ID}))
			continue
		}
		return env.Type, data, nil
	}
}

func (c *conn) close() {
	c.cancel()
	<-c.done
	c.ws.Close(websocket.StatusNormalClosure, "")
}
