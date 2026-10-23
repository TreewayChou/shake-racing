package server_test

import (
	"testing"

	"racing/internal/servertest"
)

// The harness's bots have an ideal network (no latency, drops or stalls)
// and shake exactly servertest.BotRate times a second.

func TestAddBotsJoinsTenPlayers(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()

	host.Bots("add")

	s := host.WaitPlayers(h, 10)
	if len(s.Players) != 10 {
		t.Fatalf("players = %d, want 10", len(s.Players))
	}
}

func TestFillBotsStopsAtTheHundredCap(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	for _, n := range []string{"甲", "乙", "丙"} {
		h.Phone().Join(n)
	}

	host.Bots("fill")
	host.WaitPlayers(h, 100)
	host.Bots("fill")
	host.Bots("add")

	late := h.Phone()
	late.SendJoin("遲到")
	late.Expect("full")
	if s := host.NextSnapshot(h); len(s.Players) != 100 {
		t.Errorf("players = %d after filling again, want 100", len(s.Players))
	}
	if errs := h.Server.Bots().Errors(); len(errs) > 0 {
		t.Errorf("bot errors: %v", errs)
	}
}

func TestClearBotsLeavesRealPlayersUntouched(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phone := h.Phone()
	me := phone.Join("小明")
	host.Bots("add")
	host.Bots("add")
	host.WaitPlayers(h, 21)

	host.Bots("clear")

	s := host.WaitPlayers(h, 1)
	if s.Players[0].ID != me.PlayerID {
		t.Errorf("left in roster: %+v, want only 小明", s.Players)
	}
	phone.Warmup() // still connected and in the roster
	if id := host.ExpectWarmup(); id != me.PlayerID {
		t.Errorf("warmup from %q, want %q", id, me.PlayerID)
	}
	// Bots can be added again after a clear.
	host.Bots("add")
	host.WaitPlayers(h, 11)
}

func TestBotsPlayATimedRound(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	host.SetConfig(map[string]any{"mode": "individual-timed", "durationMs": 15000})
	host.Bots("add")
	host.WaitPlayers(h, 10)

	host.Start()

	s := host.WaitPhase(h, "results")
	if len(s.Top) != 10 {
		t.Fatalf("ranked %d, want 10", len(s.Top))
	}
	// Like a phone, a bot stops counting at end. The fake clock moves on
	// while bots answer clock-sync pings, which can skew a bot's clock by
	// a tick (100 ms, one shake) and so cost it its last shake.
	want := 15 * servertest.BotRate
	for _, p := range s.Top {
		if p.Total < want-1 || p.Total > want {
			t.Errorf("%s total = %d, want %d (or one less)", p.Name, p.Total, want)
		}
	}
}

func TestBotsFinishARace(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	host.SetConfig(map[string]any{"mode": "individual-race", "target": 100})
	host.Bots("add")
	host.WaitPlayers(h, 10)

	host.Start()

	s := host.WaitPhase(h, "results")
	if len(s.Top) != 10 {
		t.Fatalf("ranked %d, want 10", len(s.Top))
	}
	for _, p := range s.Top {
		if !p.Finished || p.Total != 100 || p.ElapsedMs != 100*1000/servertest.BotRate {
			t.Errorf("%s = %+v, want finished 100 in %d ms", p.Name, p, 100*1000/servertest.BotRate)
		}
	}
}
