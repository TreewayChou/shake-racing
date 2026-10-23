// Package bots simulates players for rehearsals and load tests. Each bot
// is a real WebSocket client of the phone endpoint that speaks the same
// protocol as the phone page (join, ping/pong clock sync, cumulative
// progress, finish) over a simulated imperfect network.
package bots

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/coder/websocket"

	"racing/internal/clock"
)

// Sim describes a bot's simulated phone and network. Each bot draws its
// own latency and shake rate uniformly from the ranges.
type Sim struct {
	// LatencyMin..LatencyMax is the round-trip time; half is added to
	// every message in each direction.
	LatencyMin, LatencyMax time.Duration
	// DropRate is the chance that a pong or progress send is lost.
	DropRate float64
	// StallsPerMinute is how often, on average, the uplink stalls; a
	// stall holds every outgoing message for StallMin..StallMax.
	StallsPerMinute    float64
	StallMin, StallMax time.Duration
	// RateMin..RateMax is shakes per second.
	RateMin, RateMax float64
}

// DefaultSim is a realistic crowd of phones on 4G/5G.
var DefaultSim = Sim{
	LatencyMin: 30 * time.Millisecond, LatencyMax: 300 * time.Millisecond,
	DropRate:        0.05,
	StallsPerMinute: 1,
	StallMin:        time.Second, StallMax: 3 * time.Second,
	RateMin: 4, RateMax: 11,
}

// Config configures a Manager.
type Config struct {
	Sim Sim
	// Seed makes every bot's random draws reproducible; bot i uses the
	// stream (Seed, i). Zero picks a random seed.
	Seed uint64
}

// ProgressInterval is how often a racing bot sends its cumulative total,
// like the phone page.
const ProgressInterval = 150 * time.Millisecond

// Manager runs a set of bots against one phone endpoint.
type Manager struct {
	url   string
	clock clock.Clock
	cfg   Config

	mu      sync.Mutex
	bots    map[*Bot]context.CancelFunc
	pending int // bots not yet welcomed
	seq     int // bots ever started; numbers names and random streams
	errs    []error
	wg      sync.WaitGroup
}

// New returns a manager whose bots dial url (a ws:// phone endpoint) and
// read time from c.
func New(url string, c clock.Clock, cfg Config) *Manager {
	if cfg.Seed == 0 {
		cfg.Seed = rand.Uint64()
	}
	return &Manager{url: url, clock: c, cfg: cfg, bots: make(map[*Bot]context.CancelFunc)}
}

// Add starts n bots. They connect and join asynchronously.
func (m *Manager) Add(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for range n {
		m.seq++
		rng := rand.New(rand.NewPCG(m.cfg.Seed, uint64(m.seq)))
		b := newBot(m, fmt.Sprintf("機器人%d", m.seq), m.cfg.Sim, rng)
		ctx, cancel := context.WithCancel(context.Background())
		m.bots[b] = cancel
		m.pending++
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			b.run(ctx)
			cancel()
			m.mu.Lock()
			delete(m.bots, b)
			if !b.welcomed {
				m.pending--
			}
			m.mu.Unlock()
		}()
	}
}

// Pending is the number of bots started but not yet in the roster.
func (m *Manager) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pending
}

// Clear disconnects every bot. It does not wait for them to exit.
func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cancel := range m.bots {
		cancel()
	}
}

// Close disconnects every bot and waits for them to exit.
func (m *Manager) Close() {
	m.Clear()
	m.wg.Wait()
}

// Errors returns the protocol or connection errors bots have hit.
func (m *Manager) Errors() []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]error(nil), m.errs...)
}

// Stats returns a view of every running bot.
func (m *Manager) Stats() []Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Stats, 0, len(m.bots))
	for b := range m.bots {
		out = append(out, b.stats)
	}
	return out
}

func (m *Manager) fail(b *Bot, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errs = append(m.errs, fmt.Errorf("%s: %w", b.stats.Name, err))
}

func (m *Manager) welcomed(b *Bot, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b.welcomed = true
	b.stats.ID = id
	m.pending--
}

func (m *Manager) update(b *Bot, f func(*Stats)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(&b.stats)
}

// Stats describes one bot: its simulated parameters and what it did in
// its latest round.
type Stats struct {
	Name    string
	ID      string // player id; empty until welcomed
	Latency time.Duration
	Rate    float64 // shakes per second

	Sent, Dropped int        // pong and progress sends
	Stalls        []Interval // uplink stalls, on the bot's clock

	Final  int // shakes counted in the latest round
	Result *Result
}

// Interval is a span of time on the bot's clock.
type Interval struct{ From, To time.Time }

// Result is the standing the server reported for the latest round.
type Result struct {
	Rank, TotalShakes int
	Finished          bool // race: reached the target
}

// Bot is one simulated player. Its fields are owned by its run goroutine,
// except stats and welcomed, which are guarded by Manager.mu.
type Bot struct {
	m     *Manager
	sim   Sim
	rng   *rand.Rand
	stats Stats

	welcomed bool

	stallUntil time.Time
	out        chan outMsg
	race       *race  // nil outside a round
	mode       string // from the latest config
}

// finishAt is the total that finishes a round for the bot: the target in
// an individual race. In a team race the team average decides the end,
// so, like a phone, the bot keeps counting.
func (b *Bot) finishAt(target int) int {
	if b.mode == "team-race" {
		return 0
	}
	return target
}

type outMsg struct {
	b  []byte
	at time.Time // write no earlier than this
}

// race is the bot's view of a round, on its own clock.
type race struct {
	startAt  time.Time
	duration time.Duration // timed modes: counting stops here
	target   int           // race modes: finish on reaching it
	base     int           // shakes kept from before a reconnect
	locked   bool          // end received: counting has stopped
	lockAt   time.Duration
	done     bool // result received
}

func newBot(m *Manager, name string, sim Sim, rng *rand.Rand) *Bot {
	b := &Bot{m: m, sim: sim, rng: rng, out: make(chan outMsg, 256)}
	b.stats = Stats{
		Name:    name,
		Latency: sim.LatencyMin + time.Duration(rng.Float64()*float64(sim.LatencyMax-sim.LatencyMin)),
		Rate:    sim.RateMin + rng.Float64()*(sim.RateMax-sim.RateMin),
	}
	return b
}

// sleep waits d on the manager's clock.
func (b *Bot) sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := b.m.clock.NewTicker(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C():
		return true
	}
}

type inMsg struct {
	Type     string `json:"type"`
	Mode     string `json:"mode"` // config
	PlayerID string `json:"playerId"`

	ID       int64 `json:"id"` // ping
	StartAt  int64 `json:"startAt"`
	Duration int64 `json:"durationMs"`
	Target   int   `json:"target"`
	Total    int   `json:"totalShakes"`
	Rank     int   `json:"rank"`
	Finished bool  `json:"finished"`
}

func (b *Bot) run(ctx context.Context) {
	ws, _, err := websocket.Dial(ctx, b.m.url, nil)
	if err != nil {
		if ctx.Err() == nil {
			b.m.fail(b, err)
		}
		return
	}
	defer ws.CloseNow()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	in := make(chan inMsg, 64)
	go b.readLoop(ctx, cancel, ws, in)
	go b.writeLoop(ctx, cancel, ws)

	b.send(map[string]any{"type": "join", "name": b.stats.Name}, false)

	tick := b.m.clock.NewTicker(ProgressInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-in:
			if !ok {
				return
			}
			if !b.handle(m) {
				return
			}
		case <-tick.C():
			b.maybeStall()
			b.progress()
		}
	}
}

// handle reacts to one server message and reports whether to keep going.
func (b *Bot) handle(m inMsg) bool {
	now := b.m.clock.Now()
	switch m.Type {
	case "welcome":
		b.m.welcomed(b, m.PlayerID)
	case "ping":
		b.send(map[string]any{"type": "pong", "id": m.ID, "clientTs": now.UnixMilli()}, true)
	case "config":
		b.mode = m.Mode
		b.race = nil
	case "countdown":
		b.race = &race{
			startAt:  time.UnixMilli(m.StartAt),
			duration: time.Duration(m.Duration) * time.Millisecond,
			target:   b.finishAt(m.Target),
			base:     m.Total,
		}
		b.m.update(b, func(s *Stats) { s.Final, s.Result = 0, nil })
	case "end":
		if r := b.race; r != nil && !r.locked {
			r.locked, r.lockAt = true, b.elapsed(now)
		}
	case "result":
		if r := b.race; r != nil {
			r.done = true
		}
		b.m.update(b, func(s *Stats) { s.Result = &Result{Rank: m.Rank, TotalShakes: m.Total, Finished: m.Finished} })
	case "kicked", "full":
		return false
	case "error":
		b.m.fail(b, fmt.Errorf("server error message"))
		return false
	}
	return true
}

// elapsed is the time since the start, capped where counting stops.
func (b *Bot) elapsed(now time.Time) time.Duration {
	r := b.race
	e := now.Sub(r.startAt)
	if r.locked {
		e = r.lockAt
	}
	if r.duration > 0 && e > r.duration {
		e = r.duration
	}
	return e
}

// progress sends the cumulative total while a round is racing, until the
// result arrives, like the phone page.
func (b *Bot) progress() {
	r := b.race
	now := b.m.clock.Now()
	if r == nil || r.done || now.Before(r.startAt) {
		return
	}
	e := b.elapsed(now)
	total := r.base + int(b.stats.Rate*e.Seconds())
	finished := false
	if r.target > 0 && total >= r.target {
		total, finished = r.target, true
		e = time.Duration(math.Ceil(float64(r.target-r.base)/b.stats.Rate*1000)) * time.Millisecond
	}
	b.m.update(b, func(s *Stats) { s.Final = total })
	msg := map[string]any{"type": "progress", "totalShakes": total, "elapsedMs": e.Milliseconds()}
	if finished {
		msg["finished"] = true
	}
	b.send(msg, true)
}

// maybeStall starts an uplink stall with the configured frequency.
func (b *Bot) maybeStall() {
	now := b.m.clock.Now()
	if b.sim.StallsPerMinute <= 0 || now.Before(b.stallUntil) {
		return
	}
	p := b.sim.StallsPerMinute / float64(time.Minute/ProgressInterval)
	if b.rng.Float64() >= p {
		return
	}
	d := b.sim.StallMin + time.Duration(b.rng.Float64()*float64(b.sim.StallMax-b.sim.StallMin))
	b.stallUntil = now.Add(d)
	b.m.update(b, func(s *Stats) { s.Stalls = append(s.Stalls, Interval{now, b.stallUntil}) })
}

// send queues v for the uplink: lossy sends may be dropped, and every
// send is delayed by half the latency and held through any stall.
func (b *Bot) send(v any, lossy bool) {
	if lossy {
		dropped := b.rng.Float64() < b.sim.DropRate
		b.m.update(b, func(s *Stats) {
			s.Sent++
			if dropped {
				s.Dropped++
			}
		})
		if dropped {
			return
		}
	}
	data, _ := json.Marshal(v)
	at := b.m.clock.Now().Add(b.stats.Latency / 2)
	if at.Before(b.stallUntil) {
		at = b.stallUntil
	}
	select {
	case b.out <- outMsg{data, at}:
	default:
		b.m.fail(b, fmt.Errorf("uplink queue full"))
	}
}

func (b *Bot) writeLoop(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn) {
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-b.out:
			if !b.sleep(ctx, m.at.Sub(b.m.clock.Now())) {
				return
			}
			if err := ws.Write(ctx, websocket.MessageText, m.b); err != nil {
				if ctx.Err() == nil {
					b.m.fail(b, err)
				}
				return
			}
		}
	}
}

// readLoop delivers server messages after half the latency.
func (b *Bot) readLoop(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn, in chan<- inMsg) {
	defer close(in)
	defer cancel()
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			if ctx.Err() == nil && websocket.CloseStatus(err) == -1 {
				b.m.fail(b, err)
			}
			return
		}
		var m inMsg
		if err := json.Unmarshal(data, &m); err != nil {
			b.m.fail(b, err)
			return
		}
		if !b.sleep(ctx, b.stats.Latency/2) {
			return
		}
		select {
		case in <- m:
		case <-ctx.Done():
			return
		}
	}
}
