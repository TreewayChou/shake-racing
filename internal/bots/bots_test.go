package bots_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"racing/internal/bots"
	"racing/internal/clock"
)

// fakeServer is a scripted phone endpoint: it welcomes every bot and
// records what each one sends.
type fakeServer struct {
	t   *testing.T
	url string

	mu    sync.Mutex
	conns []*botConn
	added chan *botConn
}

type botConn struct {
	ws  *websocket.Conn
	got chan received
}

type received struct {
	at  time.Time // wall clock
	typ string
	raw json.RawMessage
}

func startFakeServer(t *testing.T) *fakeServer {
	fs := &fakeServer{t: t, added: make(chan *botConn, 100)}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		bc := &botConn{ws: ws, got: make(chan received, 1000)}
		fs.added <- bc
		for {
			_, data, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var env struct{ Type string }
			json.Unmarshal(data, &env)
			if env.Type == "join" {
				bc.send(t, map[string]any{"type": "welcome", "playerId": "p"})
			}
			bc.got <- received{time.Now(), env.Type, data}
		}
	}))
	t.Cleanup(hs.Close)
	fs.url = "ws" + strings.TrimPrefix(hs.URL, "http")
	return fs
}

// next waits for the next bot to connect.
func (fs *fakeServer) next() *botConn {
	fs.t.Helper()
	select {
	case bc := <-fs.added:
		return bc
	case <-time.After(5 * time.Second):
		fs.t.Fatal("no bot connected")
		return nil
	}
}

func (bc *botConn) send(t *testing.T, v any) {
	b, _ := json.Marshal(v)
	if err := bc.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Errorf("send: %v", err)
	}
}

// expect returns the next message of type typ, skipping others.
func (bc *botConn) expect(t *testing.T, typ string) received {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case r := <-bc.got:
			if r.typ == typ {
				return r
			}
		case <-timeout:
			t.Fatalf("no %s from bot", typ)
		}
	}
}

func newManager(t *testing.T, url string, c clock.Clock, cfg bots.Config) *bots.Manager {
	m := bots.New(url, c, cfg)
	t.Cleanup(func() {
		m.Close()
		if errs := m.Errors(); len(errs) > 0 {
			t.Errorf("bot errors: %v", errs)
		}
	})
	return m
}

// statsByName waits until n bots have been welcomed and returns their
// stats sorted by name.
func statsByName(t *testing.T, m *bots.Manager, n int) []bots.Stats {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for m.Pending() > 0 || len(m.Stats()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d bots pending", m.Pending())
		}
		time.Sleep(time.Millisecond)
	}
	s := m.Stats()
	slices.SortFunc(s, func(a, b bots.Stats) int { return strings.Compare(a.Name, b.Name) })
	return s
}

func TestBotsDrawLatencyAndRateFromTheSeed(t *testing.T) {
	fs := startFakeServer(t)
	draw := func(seed uint64) []bots.Stats {
		m := newManager(t, fs.url, clock.Real(), bots.Config{Sim: bots.DefaultSim, Seed: seed})
		m.Add(20)
		return statsByName(t, m, 20)
	}
	a, b, c := draw(7), draw(7), draw(8)

	for i := range a {
		if a[i].Latency != b[i].Latency || a[i].Rate != b[i].Rate {
			t.Errorf("%s: seed 7 drew %v %.2f/s then %v %.2f/s", a[i].Name, a[i].Latency, a[i].Rate, b[i].Latency, b[i].Rate)
		}
		if l := a[i].Latency; l < 30*time.Millisecond || l > 300*time.Millisecond {
			t.Errorf("%s latency %v, want 30-300 ms", a[i].Name, l)
		}
		if r := a[i].Rate; r < 4 || r > 11 {
			t.Errorf("%s rate %.2f/s, want 4-11", a[i].Name, r)
		}
	}
	if slices.EqualFunc(a, c, func(x, y bots.Stats) bool { return x.Rate == y.Rate }) {
		t.Error("seeds 7 and 8 drew the same rates")
	}
}

func TestBotAnswersPingsAfterTheRoundTripLatency(t *testing.T) {
	fs := startFakeServer(t)
	sim := bots.Sim{LatencyMin: 200 * time.Millisecond, LatencyMax: 200 * time.Millisecond, RateMin: 5, RateMax: 5}
	m := newManager(t, fs.url, clock.Real(), bots.Config{Sim: sim, Seed: 1})
	m.Add(1)
	bc := fs.next()
	bc.expect(t, "join")

	sent := time.Now()
	bc.send(t, map[string]any{"type": "ping", "id": 1, "serverTs": sent.UnixMilli()})
	if rtt := bc.expect(t, "pong").at.Sub(sent); rtt < 200*time.Millisecond {
		t.Errorf("pong after %v, want at least the 200 ms round trip", rtt)
	}
}

// racing starts a timed round at once on bc and returns the progress
// reports received over d.
func racing(t *testing.T, bc *botConn, d time.Duration) []received {
	t.Helper()
	bc.send(t, map[string]any{"type": "countdown", "startAt": time.Now().UnixMilli(), "durationMs": 60000})
	var out []received
	end := time.After(d)
	for {
		select {
		case r := <-bc.got:
			if r.typ == "progress" {
				out = append(out, r)
			}
		case <-end:
			return out
		}
	}
}

func TestBotDropsSendsAtTheDropRate(t *testing.T) {
	fs := startFakeServer(t)
	sim := bots.Sim{DropRate: 0.5, RateMin: 5, RateMax: 5}
	m := newManager(t, fs.url, clock.Real(), bots.Config{Sim: sim, Seed: 3})
	m.Add(1)
	bc := fs.next()
	bc.expect(t, "join")
	statsByName(t, m, 1)

	got := racing(t, bc, 3*time.Second)
	s := m.Stats()[0]

	if s.Dropped == 0 || s.Dropped == s.Sent {
		t.Errorf("dropped %d of %d sends, want some but not all", s.Dropped, s.Sent)
	}
	// Every send not dropped arrives; one may still be in flight.
	if n, want := len(got), s.Sent-s.Dropped; n > want || n < want-1 {
		t.Errorf("received %d reports, want the %d not dropped", n, want)
	}
}

func TestBotStallsHoldItsUplink(t *testing.T) {
	fs := startFakeServer(t)
	sim := bots.Sim{
		StallsPerMinute: 60 * 1000, // a stall starts on the first tick
		StallMin:        time.Second, StallMax: time.Second,
		RateMin: 5, RateMax: 5,
	}
	m := newManager(t, fs.url, clock.Real(), bots.Config{Sim: sim, Seed: 1})
	m.Add(1)
	bc := fs.next()
	bc.expect(t, "join")
	statsByName(t, m, 1)

	got := racing(t, bc, 1500*time.Millisecond)

	if len(got) < 2 {
		t.Fatalf("got %d progress reports, want several", len(got))
	}
	// Reports sent during the stall arrive together when it ends.
	first := got[0].at
	var burst int
	for _, r := range got {
		if r.at.Sub(first) < 50*time.Millisecond {
			burst++
		}
	}
	if burst < 3 {
		t.Errorf("%d reports arrived together after the stall, want the held ones (≥3)", burst)
	}
	if s := m.Stats()[0]; len(s.Stalls) == 0 || s.Stalls[0].To.Sub(s.Stalls[0].From) != time.Second {
		t.Errorf("stalls = %v, want a 1 s stall", s.Stalls)
	}
}

func TestBotKeepsShakingPastTheTargetInATeamRace(t *testing.T) {
	fs := startFakeServer(t)
	m := newManager(t, fs.url, clock.Real(), bots.Config{Sim: bots.Sim{RateMin: 5, RateMax: 5}, Seed: 1})
	m.Add(1)
	bc := fs.next()
	bc.expect(t, "join")
	statsByName(t, m, 1)

	// A team race to 20, started 10 s ago: a team's average decides the
	// end, so a bot, like a phone, has no finish of its own.
	bc.send(t, map[string]any{"type": "config", "mode": "team-race", "target": 20})
	bc.send(t, map[string]any{"type": "countdown", "startAt": time.Now().Add(-10 * time.Second).UnixMilli(), "target": 20})
	var p struct {
		Total    int  `json:"totalShakes"`
		Finished bool `json:"finished"`
	}
	json.Unmarshal(bc.expect(t, "progress").raw, &p)
	if p.Total <= 20 || p.Finished {
		t.Errorf("progress = %+v, want about 50 shakes and no finish", p)
	}
}
