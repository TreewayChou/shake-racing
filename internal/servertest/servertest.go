// Package servertest is the primary test seam: a real in-process server
// driven by a fake clock, plus clients that speak the wire protocol as a
// phone or as the host. Its message types are an independent, client-side
// reading of the protocol, so tests do not share structs with the server.
package servertest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"racing/internal/clock"
	"racing/internal/server"
)

// Epoch is the fake clock's start time.
var Epoch = time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC)

// Timeout bounds every wait for a message. It only guards against hangs;
// no test outcome depends on wall-clock timing.
var Timeout = 5 * time.Second

// Harness is a running server under test.
type Harness struct {
	T      testing.TB
	Clock  *clock.Fake
	Server *server.Server
	HTTP   *httptest.Server
}

// Start launches a server on a loopback port with a fake clock. It is shut
// down when the test ends.
func Start(t testing.TB) *Harness {
	t.Helper()
	fc := clock.NewFake(Epoch)
	srv := server.New(server.Options{Clock: fc})
	hs := httptest.NewServer(srv)
	t.Cleanup(func() {
		srv.Close()
		hs.Close()
	})
	return &Harness{T: t, Clock: fc, Server: srv, HTTP: hs}
}

// URL returns the absolute http URL for path.
func (h *Harness) URL(path string) string { return h.HTTP.URL + path }

// Advance moves the fake clock forward.
func (h *Harness) Advance(d time.Duration) { h.Clock.Advance(d) }

// Tick advances the clock by one broadcast interval.
func (h *Harness) Tick() { h.Clock.Advance(server.BroadcastInterval) }

// Client is a raw protocol client.
type Client struct {
	t  testing.TB
	ws *websocket.Conn
}

func (h *Harness) dial(path string) *Client {
	h.T.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	url := "ws" + strings.TrimPrefix(h.URL(path), "http")
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		h.T.Fatalf("dial %s: %v", path, err)
	}
	ws.SetReadLimit(1 << 20)
	h.T.Cleanup(func() { ws.Close(websocket.StatusNormalClosure, "") })
	return &Client{t: h.T, ws: ws}
}

// Send writes v as one JSON message.
func (c *Client) Send(v any) {
	c.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		c.t.Fatalf("marshal: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		c.t.Fatalf("send: %v", err)
	}
}

// Message is a received message: its type tag and raw JSON.
type Message struct {
	Type string
	Raw  json.RawMessage
}

// Decode unmarshals the message into v.
func (m Message) Decode(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal(m.Raw, v); err != nil {
		t.Fatalf("decode %s: %v", m.Type, err)
	}
}

// Recv returns the next message.
func (c *Client) Recv() Message {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	_, data, err := c.ws.Read(ctx)
	if err != nil {
		c.t.Fatalf("recv: %v", err)
	}
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		c.t.Fatalf("recv: bad JSON %q: %v", data, err)
	}
	return Message{Type: env.Type, Raw: data}
}

// Expect returns the next message whose type is typ, skipping others.
func (c *Client) Expect(typ string) Message {
	c.t.Helper()
	for {
		if m := c.Recv(); m.Type == typ {
			return m
		}
	}
}

// Phone is a client on the phone endpoint.
type Phone struct{ *Client }

// Phone connects a new phone.
func (h *Harness) Phone() *Phone {
	h.T.Helper()
	return &Phone{h.dial("/ws/play")}
}

// Welcome is the server's reply to a successful join.
type Welcome struct {
	PlayerID string `json:"playerId"`
}

// Error is a server rejection.
type Error struct {
	Reason string `json:"reason"`
}

// SendJoin sends join{name} without waiting for a reply.
func (p *Phone) SendJoin(name string) {
	p.t.Helper()
	p.Send(map[string]any{"type": "join", "name": name})
}

// Join sends join{name} and requires a welcome.
func (p *Phone) Join(name string) Welcome {
	p.t.Helper()
	p.SendJoin(name)
	m := p.Recv()
	if m.Type != "welcome" {
		p.t.Fatalf("join %q: got %s, want welcome: %s", name, m.Type, m.Raw)
	}
	var w Welcome
	m.Decode(p.t, &w)
	return w
}

// Host is a client on the host endpoint.
type Host struct{ *Client }

// Host connects the big screen.
func (h *Harness) Host() *Host {
	h.T.Helper()
	return &Host{h.dial("/ws/host")}
}

// Snapshot is the host's periodic view of the room.
type Snapshot struct {
	Phase   string           `json:"phase"`
	Players []SnapshotPlayer `json:"players"`
}

// SnapshotPlayer is one roster entry in a snapshot.
type SnapshotPlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// WaitSnapshot reads snapshots until one satisfies ok, and returns it.
func (h *Host) WaitSnapshot(ok func(Snapshot) bool) Snapshot {
	h.t.Helper()
	for {
		var s Snapshot
		h.Expect("snapshot").Decode(h.t, &s)
		if ok(s) {
			return s
		}
	}
}

// ServeFrom sends a GET for path straight to the server's handler as if it
// came from remoteAddr ("ip:port"), which a loopback listener cannot fake.
func (h *Harness) ServeFrom(remoteAddr, path string, header http.Header) *httptest.ResponseRecorder {
	h.T.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr
	for k, vs := range header {
		req.Header[k] = vs
	}
	rec := httptest.NewRecorder()
	h.Server.ServeHTTP(rec, req)
	return rec
}

// Get performs a GET with the given headers, without transparent gzip
// decoding, and returns the response. The body is closed at test end.
func (h *Harness) Get(path string, header http.Header) *http.Response {
	h.T.Helper()
	req, err := http.NewRequest(http.MethodGet, h.URL(path), nil)
	if err != nil {
		h.T.Fatal(err)
	}
	for k, vs := range header {
		req.Header[k] = vs
	}
	tr := &http.Transport{DisableCompression: true}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		h.T.Fatalf("GET %s: %v", path, err)
	}
	h.T.Cleanup(func() { resp.Body.Close(); tr.CloseIdleConnections() })
	return resp
}
