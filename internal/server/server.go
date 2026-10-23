// Package server is the transport layer: HTTP pages, the phone and host
// WebSocket endpoints, and the 10 Hz host broadcaster. All game rules live
// in package game; the server serialises access to the engine.
package server

import (
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

	mu     sync.Mutex
	engine *game.Engine
	hosts  map[*conn]struct{}

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
		engine: game.New(opts.Clock),
		hosts:  make(map[*conn]struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
	s.mux = http.NewServeMux()
	s.mux.Handle("GET /play", page(web.Play))
	s.mux.Handle("GET /host", hostOnly(page(web.Host)))
	s.mux.HandleFunc("GET /ws/play", s.servePhone)
	s.mux.Handle("GET /ws/host", hostOnly(http.HandlerFunc(s.serveHost)))
	s.mux.Handle("GET /{$}", http.RedirectHandler("/play", http.StatusFound))

	ticker := opts.Clock.NewTicker(BroadcastInterval)
	s.wg.Add(1)
	go s.broadcast(ticker)
	return s
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
			msg := s.snapshotLocked()
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

func (s *Server) snapshotLocked() []byte {
	snap := s.engine.Snapshot()
	msg := snapshotMsg{Type: "snapshot", Phase: string(snap.Phase), Players: make([]snapshotPlayer, len(snap.Players))}
	for i, p := range snap.Players {
		msg.Players[i] = snapshotPlayer{ID: p.ID, Name: p.Name}
	}
	return mustJSON(msg)
}

func (s *Server) servePhone(w http.ResponseWriter, r *http.Request) {
	c, err := s.accept(w, r)
	if err != nil {
		return
	}
	defer c.close()
	for {
		typ, data, err := c.read()
		if err != nil {
			return
		}
		switch typ {
		case "join":
			var m joinMsg
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			s.mu.Lock()
			p, err := s.engine.Join(m.Name)
			s.mu.Unlock()
			if errors.Is(err, game.ErrInvalidName) {
				c.send(mustJSON(errorMsg{Type: "error", Reason: "name"}))
				continue
			}
			c.send(mustJSON(welcomeMsg{Type: "welcome", PlayerID: p.ID}))
		}
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
	c.send(s.snapshotLocked())
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.hosts, c)
		s.mu.Unlock()
	}()
	for {
		if _, _, err := c.read(); err != nil {
			return
		}
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
		return env.Type, data, nil
	}
}

func (c *conn) close() {
	c.cancel()
	<-c.done
	c.ws.Close(websocket.StatusNormalClosure, "")
}
