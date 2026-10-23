package servertest

import (
	"sync/atomic"
	"time"
)

// Client-side reading of the round protocol: configuration, clock sync,
// countdown, progress and results.

var echoSeq atomic.Int64

// Echo sends echo{id} and waits for the server to reply with the same id.
// Messages on one connection are handled in order, so on return every
// message this client sent earlier has been processed. Other messages
// received meanwhile are discarded.
func (c *Client) Echo() {
	c.t.Helper()
	id := echoSeq.Add(1)
	c.Send(map[string]any{"type": "echo", "id": id})
	for {
		var e struct {
			ID int64 `json:"id"`
		}
		c.Expect("echo").Decode(c.t, &e)
		if e.ID == id {
			return
		}
	}
}

// NowMs is the fake clock's current time in Unix milliseconds.
func (h *Harness) NowMs() int64 { return h.Clock.Now().UnixMilli() }

// Config is the round configuration pushed to phones.
type Config struct {
	Mode       string `json:"mode"`
	DurationMs int64  `json:"durationMs"` // timed modes
	Target     int    `json:"target"`     // race modes: shakes to finish
	TimeCapMs  int64  `json:"timeCapMs"`  // individual race: longest a round may run
}

// ExpectConfig waits for the next config message.
func (p *Phone) ExpectConfig() Config {
	p.t.Helper()
	var c Config
	p.Expect("config").Decode(p.t, &c)
	return c
}

// Ping is a clock-sync probe from the server.
type Ping struct {
	ID       int64 `json:"id"`
	ServerTs int64 `json:"serverTs"`
}

// ExpectPing waits for the next ping.
func (p *Phone) ExpectPing() Ping {
	p.t.Helper()
	var g Ping
	p.Expect("ping").Decode(p.t, &g)
	return g
}

// Pong answers ping id with the phone's local clock reading.
func (p *Phone) Pong(id, clientTs int64) {
	p.t.Helper()
	p.Send(map[string]any{"type": "pong", "id": id, "clientTs": clientTs})
}

// SyncClock answers n pings as a phone whose clock runs offset ahead of
// the server's, with no network delay, then waits until they are handled.
func (p *Phone) SyncClock(n int, offset time.Duration) {
	p.t.Helper()
	for range n {
		g := p.ExpectPing()
		p.Pong(g.ID, g.ServerTs+offset.Milliseconds())
	}
	p.Echo()
}

// Countdown announces the round's absolute start moment.
type Countdown struct {
	StartAt    int64 `json:"startAt"`     // on the receiving phone's own clock, Unix ms
	DurationMs int64 `json:"durationMs"`  // timed modes
	Target     int   `json:"target"`      // race modes
	TimeCapMs  int64 `json:"timeCapMs"`   // individual race
	Total      int   `json:"totalShakes"` // accepted so far; non-zero when resuming after a reconnect
	FinishedMs int64 `json:"finishedMs"`  // race finish time kept, when resuming after a reconnect

}

// ExpectCountdown waits for the next countdown message.
func (p *Phone) ExpectCountdown() Countdown {
	p.t.Helper()
	var c Countdown
	p.Expect("countdown").Decode(p.t, &c)
	return c
}

// Progress sends a cumulative progress report without waiting.
func (p *Phone) Progress(totalShakes int, elapsedMs int64) {
	p.t.Helper()
	p.Send(map[string]any{"type": "progress", "totalShakes": totalShakes, "elapsedMs": elapsedMs})
}

// Finish sends the race finish report: the target total and the phone's
// local elapsed time at the moment it was reached. It does not wait.
func (p *Phone) Finish(totalShakes int, elapsedMs int64) {
	p.t.Helper()
	p.Send(map[string]any{"type": "progress", "totalShakes": totalShakes, "elapsedMs": elapsedMs, "finished": true})
}

// ExpectEnd waits for the time-up end message.
func (p *Phone) ExpectEnd() {
	p.t.Helper()
	p.Expect("end")
}

// Result is a phone's final standing.
type Result struct {
	Rank        int   `json:"rank"`
	TotalShakes int   `json:"totalShakes"`
	Players     int   `json:"players"`
	Finished    bool  `json:"finished"`  // race: reached the target
	ElapsedMs   int64 `json:"elapsedMs"` // race: the phone's finish time

	// Team modes only.
	Team     string  `json:"team"`
	TeamRank int     `json:"teamRank"` // 1 = the winning team
	TeamAvg  float64 `json:"teamAvg"`
}

// ExpectResult waits for the phone's result.
func (p *Phone) ExpectResult() Result {
	p.t.Helper()
	var r Result
	p.Expect("result").Decode(p.t, &r)
	return r
}

// SetConfig sends setConfig with the given fields and waits until it is
// handled.
func (h *Host) SetConfig(fields map[string]any) {
	h.t.Helper()
	m := map[string]any{"type": "setConfig"}
	for k, v := range fields {
		m[k] = v
	}
	h.Send(m)
	h.Echo()
}

// Start presses Start and waits until it is handled.
func (h *Host) Start() {
	h.t.Helper()
	h.Send(map[string]any{"type": "start"})
	h.Echo()
}

// End presses 結束 and waits until it is handled.
func (h *Host) End() {
	h.t.Helper()
	h.Send(map[string]any{"type": "end"})
	h.Echo()
}

// Again presses 再來一場 and waits until it is handled.
func (h *Host) Again() {
	h.t.Helper()
	h.Send(map[string]any{"type": "again"})
	h.Echo()
}

// NextSnapshot advances one broadcast interval and returns the snapshot
// taken at the new time, skipping older ones.
func (h *Host) NextSnapshot(hs *Harness) Snapshot {
	h.t.Helper()
	hs.Tick()
	now := hs.NowMs()
	return h.WaitSnapshot(func(s Snapshot) bool { return s.ServerNow >= now })
}
