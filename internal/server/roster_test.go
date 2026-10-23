package server_test

import (
	"fmt"
	"testing"
	"time"

	"racing/internal/servertest"
)

// raceAt starts a timed round like startRace and moves the clock to
// elapsedMs after the synced start; negative is still in the countdown.
func raceAt(t *testing.T, durationMs int, elapsedMs int64, names ...string) *race {
	t.Helper()
	r := &race{h: servertest.Start(t)}
	r.host = r.h.Host()
	for _, n := range names {
		p := r.h.Phone()
		r.ids = append(r.ids, p.Join(n).PlayerID)
		r.phones = append(r.phones, p)
	}
	r.host.SetConfig(map[string]any{"durationMs": durationMs})
	r.host.Start()
	r.startAt = r.phones[0].ExpectCountdown().StartAt
	r.advanceTo(elapsedMs)
	return r
}

func TestLateJoinerWaitsAndRacesNextRound(t *testing.T) {
	for _, phase := range []struct {
		name      string
		elapsedMs int64 // after the synced start; negative is countdown
	}{
		{"countdown", -1000},
		{"racing", 5000},
		{"grace", 15000 + 500},
	} {
		t.Run(phase.name, func(t *testing.T) {
			r := raceAt(t, 15000, phase.elapsedMs, "小明")

			late := r.h.Phone()
			id := late.Join("遲到").PlayerID
			late.ExpectWait()
			if p, _ := r.host.NextSnapshot(r.h).Player(id); !p.Waiting {
				t.Errorf("late joiner %+v, want waiting", p)
			}

			r.advanceTo(15000 + graceMs)
			if res := r.phones[0].ExpectResult(); res.Players != 1 {
				t.Errorf("result = %+v, want the late joiner unranked", res)
			}
			r.host.Again()
			late.ExpectConfig()
			r.host.Start()
			late.ExpectCountdown()
			if p, _ := r.host.NextSnapshot(r.h).Player(id); p.Waiting {
				t.Errorf("late joiner %+v still waiting in the next round", p)
			}
		})
	}
}

func TestLateJoinerGetsNoEndOrResult(t *testing.T) {
	r := startRace(t, 15000, "小明")
	r.advanceTo(5000)
	late := r.h.Phone()
	late.Join("遲到")
	late.ExpectWait()

	r.advanceTo(15000 + graceMs)
	r.phones[0].ExpectResult()
	r.host.Again()
	// After wait, the next round message must be the lobby config.
	for {
		m := late.Recv()
		if m.Type == "ping" {
			continue
		}
		if m.Type != "config" {
			t.Fatalf("late joiner got %s before the lobby config", m.Type)
		}
		break
	}
}

func TestRoomCapsAt100(t *testing.T) {
	h := servertest.Start(t)
	for i := range 100 {
		h.Phone().Join(fmt.Sprintf("p%d", i))
	}
	extra := h.Phone()
	extra.SendJoin("第101")
	if m := extra.Recv(); m.Type != "full" {
		t.Fatalf("101st join got %s, want full", m.Type)
	}
}

func TestReconnectIsAllowedWhenFull(t *testing.T) {
	h := servertest.Start(t)
	first := h.Phone().Join("p0").PlayerID
	for i := 1; i < 100; i++ {
		h.Phone().Join(fmt.Sprintf("p%d", i))
	}
	if w := h.Phone().Rejoin("p0", first); w.PlayerID != first {
		t.Errorf("rejoin id = %s, want %s", w.PlayerID, first)
	}
}

func TestDuplicateNicknamesGetSuffix(t *testing.T) {
	h := servertest.Start(t)
	var got []string
	for range 3 {
		got = append(got, h.Phone().Join("小明").Name)
	}
	got = append(got, h.Phone().Join(" 小明#2 ").Name)
	want := []string{"小明", "小明#2", "小明#3", "小明#2#2"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}

func TestKickRemovesPlayerAndTellsPhone(t *testing.T) {
	r := startRace(t, 15000, "小明", "小華")
	r.host.Kick(r.ids[0])

	r.phones[0].ExpectKicked()
	s := r.host.NextSnapshot(r.h)
	if _, ok := s.Player(r.ids[0]); ok || len(s.Players) != 1 {
		t.Errorf("roster after kick = %+v, want only 小華", s.Players)
	}
	r.advanceTo(15000 + graceMs)
	if res := r.phones[1].ExpectResult(); res.Players != 1 {
		t.Errorf("result = %+v, want 1 ranked", res)
	}

	// The stored playerId no longer restores the kicked player.
	if w := r.h.Phone().Rejoin("小明", r.ids[0]); w.PlayerID == r.ids[0] {
		t.Error("kicked player reconnected with the old id")
	}
}

func TestPlayerGoesOfflineAfterMissedHeartbeats(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	quiet := h.Phone()
	quietID := quiet.Join("沉默").PlayerID
	alive := h.Phone()
	aliveID := alive.Join("活著").PlayerID
	alive.SyncClock(5, 0)

	// Two heartbeats pass; only one phone answers them.
	for range 2 {
		h.Advance(servertest.HeartbeatInterval)
		g := alive.ExpectPing()
		alive.Pong(g.ID, g.ServerTs)
		alive.Echo()
	}
	h.Advance(4800 * time.Millisecond) // 34.8 s since the quiet phone joined
	s := host.NextSnapshot(h)          // 34.9 s
	if p, _ := s.Player(quietID); p.Offline {
		t.Fatalf("offline at 34.9 s, want online until 35 s")
	}
	s = host.NextSnapshot(h) // 35 s
	if p, _ := s.Player(quietID); !p.Offline {
		t.Errorf("quiet player %+v, want offline after 35 s", p)
	}
	if p, _ := s.Player(aliveID); p.Offline {
		t.Errorf("answering player %+v shown offline", p)
	}
	if len(s.Players) != 2 {
		t.Errorf("roster = %+v, want offline player kept", s.Players)
	}

	// Any message brings the player back.
	quiet.Warmup()
	quiet.Echo()
	if p, _ := host.NextSnapshot(h).Player(quietID); p.Offline {
		t.Errorf("player %+v still offline after a message", p)
	}
}

func TestAgainKeepsRoster(t *testing.T) {
	r := startRace(t, 15000, "小明", "小華")
	r.advanceTo(15000 + graceMs)
	r.phones[0].ExpectResult()
	r.host.Again()

	s := r.host.NextSnapshot(r.h)
	if s.Phase != "lobby" || len(s.Players) != 2 {
		t.Fatalf("after again: phase %q roster %+v, want lobby with 2", s.Phase, s.Players)
	}
	for _, p := range s.Players {
		if p.Total != 0 {
			t.Errorf("player %+v kept last round's total", p)
		}
	}
}

func TestResetReturnsToLobbyFromEveryPhase(t *testing.T) {
	for _, phase := range []struct {
		name      string
		elapsedMs int64
	}{
		{"countdown", -1000},
		{"racing", 5000},
		{"grace", 15000 + 500},
		{"results", 15000 + graceMs},
	} {
		t.Run(phase.name, func(t *testing.T) {
			r := raceAt(t, 15000, phase.elapsedMs, "小明", "小華")
			r.phones[0].Progress(10, max(phase.elapsedMs, 0))
			r.phones[0].Echo()
			if s := r.host.NextSnapshot(r.h); s.Phase != phase.name {
				t.Fatalf("phase = %q, want %q", s.Phase, phase.name)
			}

			r.host.Reset()

			s := r.host.NextSnapshot(r.h)
			if s.Phase != "lobby" || len(s.Players) != 2 || len(s.Top) != 0 {
				t.Fatalf("after reset: phase %q roster %d top %d, want lobby, 2, 0", s.Phase, len(s.Players), len(s.Top))
			}
			for _, p := range s.Players {
				if p.Total != 0 {
					t.Errorf("player %+v kept the abandoned round's total", p)
				}
			}
			pressedAt := r.h.NowMs()
			r.host.Start()
			if cd := r.phones[0].ExpectCountdown(); cd.StartAt != pressedAt+startLeadMs {
				t.Errorf("next round startAt %d, want %d", cd.StartAt, pressedAt+startLeadMs)
			}
		})
	}
}

func TestResetInLobbyKeepsRoster(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	h.Phone().Join("小明")
	host.Reset()
	if s := host.NextSnapshot(h); s.Phase != "lobby" || len(s.Players) != 1 {
		t.Errorf("after lobby reset: %q %+v", s.Phase, s.Players)
	}
}

func TestReconnectMidRaceKeepsIdentityAndScore(t *testing.T) {
	r := startRace(t, 30000, "小明", "小華")
	r.advanceTo(2000)
	r.phones[0].Progress(20, 2000)
	r.phones[0].Echo()

	// The page is refreshed: a new connection presents the stored playerId.
	again := r.h.Phone()
	w := again.Rejoin("隨便", r.ids[0])
	if w.PlayerID != r.ids[0] || w.Name != "小明" {
		t.Fatalf("welcome = %+v, want id %s name 小明", w, r.ids[0])
	}
	cd := again.ExpectCountdown()
	if cd.StartAt != r.startAt || cd.DurationMs != 30000 || cd.Total != 20 {
		t.Errorf("resume countdown = %+v, want startAt %d durationMs 30000 total 20", cd, r.startAt)
	}

	r.advanceTo(3000)
	again.Progress(30, 3000)
	again.Echo()
	s := r.host.NextSnapshot(r.h)
	if len(s.Players) != 2 {
		t.Fatalf("roster = %+v, want 2 players", s.Players)
	}
	if p, _ := s.Player(r.ids[0]); p.Total != 30 || p.Name != "小明" {
		t.Errorf("reconnected player = %+v, want 小明 with 30", p)
	}

	r.advanceTo(30000 + graceMs)
	if res := again.ExpectResult(); res.Rank != 1 || res.TotalShakes != 30 || res.Players != 2 {
		t.Errorf("result = %+v, want rank 1 of 2 with 30", res)
	}
}

func TestReconnectAfterTheRoundGetsResult(t *testing.T) {
	r := startRace(t, 15000, "小明")
	r.phones[0].Progress(12, 0)
	r.advanceTo(1000)
	r.phones[0].Progress(12, 1000)
	r.phones[0].Echo()
	r.advanceTo(15000 + graceMs)

	again := r.h.Phone()
	again.Rejoin("小明", r.ids[0])
	if res := again.ExpectResult(); res.Rank != 1 || res.TotalShakes != 12 {
		t.Errorf("result after reconnect = %+v, want rank 1 with 12", res)
	}
}

func TestReconnectKeepsClockOffset(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	p := h.Phone()
	id := p.Join("小明").PlayerID
	p.SyncClock(5, 2*time.Second) // phone clock runs 2 s ahead
	host.Start()
	startAt := p.ExpectCountdown().StartAt

	again := h.Phone()
	again.Rejoin("小明", id)
	if got := again.ExpectCountdown().StartAt; got != startAt {
		t.Errorf("resumed startAt %d, want %d on the phone's clock", got, startAt)
	}
}

func TestUnknownPlayerIDJoinsAsNewPlayer(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	w := h.Phone().Rejoin("小明", "no-such-player")
	if w.PlayerID == "no-such-player" || w.Name != "小明" {
		t.Errorf("welcome = %+v, want a fresh id for 小明", w)
	}
	if s := host.NextSnapshot(h); len(s.Players) != 1 {
		t.Errorf("roster = %+v, want 1 player", s.Players)
	}
}
