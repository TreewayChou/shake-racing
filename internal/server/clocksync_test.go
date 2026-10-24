package server_test

import (
	"testing"
	"time"

	"racing/internal/servertest"
)

// syncSamples is how many pings a phone answers right after joining; the
// server bursts pings so even a phone that joins just before Start has
// several samples.
const syncSamples = 5

// answerPing answers one ping as a phone whose clock is offset ahead of
// the server's, with the reply taking out on the way there and back on
// the way back (both simulated on the fake clock).
func answerPing(h *servertest.Harness, p *servertest.Phone, offset, out, back time.Duration) {
	g := p.ExpectPing()
	h.Advance(out + back)
	p.Pong(g.ID, g.ServerTs+(offset+out).Milliseconds())
}

func TestPhonesStartTogetherDespiteClockOffsets(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()

	ahead := h.Phone()
	ahead.Join("快")
	behind := h.Phone()
	behind.Join("慢")

	const aheadOff, behindOff = 5 * time.Second, -3 * time.Second
	for range syncSamples {
		answerPing(h, ahead, aheadOff, 10*time.Millisecond, 10*time.Millisecond)
	}
	// One badly asymmetric sample (slow uplink), then decent ones: the
	// server must trust the lowest-round-trip samples.
	answerPing(h, behind, behindOff, 350*time.Millisecond, 50*time.Millisecond)
	for range syncSamples - 1 {
		answerPing(h, behind, behindOff, 25*time.Millisecond, 15*time.Millisecond)
	}
	ahead.Echo()
	behind.Echo()

	host.Start()
	startAt := host.NextSnapshot(h).StartAt

	for _, tc := range []struct {
		name   string
		p      *servertest.Phone
		offset time.Duration
	}{{"ahead", ahead, aheadOff}, {"behind", behind, behindOff}} {
		local := tc.p.ExpectCountdown().StartAt
		// The moment the phone will start, expressed on the server clock.
		got := local - tc.offset.Milliseconds()
		if d := got - startAt; d < -20 || d > 20 {
			t.Errorf("%s phone starts %d ms off the server start, want within ±20 ms", tc.name, d)
		}
	}
}

func TestHeartbeatPingsEvery15s(t *testing.T) {
	h := servertest.Start(t)
	p := h.Phone()
	p.Join("小明")
	p.SyncClock(syncSamples, 0)

	h.Advance(15 * time.Second)

	g := p.ExpectPing()
	if g.ServerTs != h.NowMs() {
		t.Errorf("heartbeat ping serverTs = %d, want %d", g.ServerTs, h.NowMs())
	}
}
