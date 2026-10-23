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

	"racing/internal/bots"
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
	h := StartWith(t, server.Options{Clock: fc, Bots: &bots.Config{Sim: bots.Sim{RateMin: BotRate, RateMax: BotRate}, Seed: 1}})
	h.Clock = fc
	return h
}

// StartWith launches a server with the given options on a loopback port,
// with its bot controls enabled. Harness.Clock is nil; tests that need a
// fake clock use Start.
func StartWith(t testing.TB, opts server.Options) *Harness {
	t.Helper()
	srv := server.New(opts)
	hs := httptest.NewServer(srv)
	srv.SetBotURL(hs.URL)
	t.Cleanup(func() {
		srv.Close()
		hs.Close()
	})
	return &Harness{T: t, Server: srv, HTTP: hs}
}

// URL returns the absolute http URL for path.
func (h *Harness) URL(path string) string { return h.HTTP.URL + path }

// Advance moves the fake clock forward.
func (h *Harness) Advance(d time.Duration) { h.Clock.Advance(d) }

// TickInterval is the host snapshot cadence.
const TickInterval = server.BroadcastInterval

// HeartbeatInterval is how often the server pings every phone.
const HeartbeatInterval = server.HeartbeatInterval

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
	PlayerID  string   `json:"playerId"`
	Name      string   `json:"name"` // as shown on the host, deduplicated
	Team      string   `json:"team"` // red, blue, green or yellow: auto-balanced on join
	Constants Detector `json:"constants"`
}

// Detector is the phone shake-detector tuning supplied by the server.
type Detector struct {
	Accel      float64 `json:"accel"`      // |acceleration| threshold, m/s²
	Delta      float64 `json:"delta"`      // fallback |Δ accelerationIncludingGravity| threshold
	Rearm      float64 `json:"rearm"`      // re-arm below this fraction of the threshold
	CooldownMs int     `json:"cooldownMs"` // minimum gap between counted shakes
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

// Warmup sends a lobby warm-up shake.
func (p *Phone) Warmup() {
	p.t.Helper()
	p.Send(map[string]any{"type": "warmup"})
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
	Phase      string           `json:"phase"`
	Mode       string           `json:"mode"`
	DurationMs int64            `json:"durationMs"`
	Target     int              `json:"target"`
	TimeCapMs  int64            `json:"timeCapMs"`
	ServerNow  int64            `json:"serverNow"` // server clock, Unix ms
	StartAt    int64            `json:"startAt"`   // server clock, Unix ms; 0 in the lobby
	EndAt      int64            `json:"endAt"`     // racing end, server clock; 0 until known
	GraceMs    int64            `json:"graceMs"`
	Players    []SnapshotPlayer `json:"players"`
	Top        []SnapshotPlayer `json:"top"` // live (or, in results, final) ranking, at most 15

	// Team modes only.
	Teams []TeamScore      `json:"teams"` // the four lanes in order red, blue, green, yellow
	MVP   []SnapshotPlayer `json:"mvp"`   // individual Top 5 across all teams, live or final
}

// TeamScore is one team lane: score is the average shakes per member.
type TeamScore struct {
	Team    string  `json:"team"`
	Members int     `json:"members"` // round participants (roster size in the lobby)
	Total   int     `json:"total"`
	Avg     float64 `json:"avg"`
	Rank    int     `json:"rank"` // 1 = leading or winning; 0 for a team with nobody
}

// SnapshotPlayer is one roster entry in a snapshot.
type SnapshotPlayer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Total int    `json:"total"`
	Rank  int    `json:"rank,omitempty"` // set in top
	Team  string `json:"team"`           // red, blue, green or yellow

	Offline bool `json:"offline,omitempty"` // missed 2 heartbeats; still in the roster
	Waiting bool `json:"waiting,omitempty"` // joined mid-round; races in the next one

	Finished  bool  `json:"finished"`  // race: reached the target
	ElapsedMs int64 `json:"elapsedMs"` // race: phone-measured finish time

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

// ExpectWarmup waits for the next warmup{id} event, skipping snapshots,
// and returns the player id.
func (h *Host) ExpectWarmup() string {
	h.t.Helper()
	var w struct {
		ID string `json:"id"`
	}
	h.Expect("warmup").Decode(h.t, &w)
	return w.ID
}

// URLs is the host's urls{public, lan, tunnelMode} message: the /play URLs
// players can join at, and how the public one was obtained.
type URLs struct {
	Public     string `json:"public"`
	LAN        string `json:"lan"`
	TunnelMode string `json:"tunnelMode"`
}

// ExpectURLs waits for the next urls message, skipping others.
func (h *Host) ExpectURLs() URLs {
	h.t.Helper()
	var u URLs
	h.Expect("urls").Decode(h.t, &u)
	return u
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
