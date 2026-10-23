package server_test

import (
	"slices"
	"testing"
	"time"

	"racing/internal/bots"
	"racing/internal/clock"
	"racing/internal/server"
	"racing/internal/servertest"
)

// loadSpeedup runs the load test's real-time rounds this many times as
// fast as the wall clock, so a 15 s round takes 3 s.
const loadSpeedup = 5

// TestHundredBotsPlayFullRounds fills the room with bots on the default
// simulated network (latency, drops, stalls, varied shake rates) and plays
// a timed round and a race. Every bot must get a result that records its
// final count, the host must get snapshots at about 10 Hz, and no bot may
// hit an error.
func TestHundredBotsPlayFullRounds(t *testing.T) {
	if testing.Short() {
		t.Skip("load test")
	}
	h := servertest.StartWith(t, server.Options{
		Clock: clock.Scaled(servertest.Epoch, loadSpeedup),
		Bots:  &bots.Config{Sim: bots.DefaultSim, Seed: 42},
	})
	host := h.Host()
	host.Bots("fill")
	s := waitLive(t, host, func(s servertest.Snapshot) bool { return len(s.Players) == 100 })
	// Let the bots' clock-sync bursts finish: 5 round trips of ≤ 300 ms,
	// longer if a pong was dropped or stalled.
	syncedAt := s.ServerNow + 3000
	waitLive(t, host, func(s servertest.Snapshot) bool { return s.ServerNow >= syncedAt })

	rounds := []struct {
		name   string
		config map[string]any
	}{
		{"timed", map[string]any{"mode": "individual-timed", "durationMs": 15000}},
		{"race", map[string]any{"mode": "individual-race", "target": 100, "timeCapMs": 60000}},
	}
	for _, r := range rounds {
		host.SetConfig(r.config)
		host.Start()
		var times []int64
		final := waitLive(t, host, func(s servertest.Snapshot) bool {
			if s.Phase != "lobby" {
				times = append(times, s.ServerNow)
			}
			return s.Phase == "results"
		})
		checkCadence(t, r.name, times)
		checkResults(t, r.name, h.Server.Bots(), final.EndAt)
		host.Again()
	}
	if errs := h.Server.Bots().Errors(); len(errs) > 0 {
		t.Errorf("bot errors: %v", errs)
	}
}

// waitLive reads the host's live snapshots until one satisfies ok.
func waitLive(t *testing.T, host *servertest.Host, ok func(servertest.Snapshot) bool) servertest.Snapshot {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if s := host.WaitSnapshot(func(servertest.Snapshot) bool { return true }); ok(s) {
			return s
		}
	}
	t.Fatal("timed out waiting for a snapshot")
	return servertest.Snapshot{}
}

// checkCadence requires snapshots about every 100 ms of server time.
func checkCadence(t *testing.T, round string, times []int64) {
	t.Helper()
	if len(times) < 2 {
		t.Fatalf("%s: %d snapshots", round, len(times))
	}
	gaps := make([]int64, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		gaps = append(gaps, times[i]-times[i-1])
	}
	slices.Sort(gaps)
	mean := float64(times[len(times)-1]-times[0]) / float64(len(gaps))
	p95 := gaps[len(gaps)*95/100]
	t.Logf("%s: %d snapshots, mean gap %.1f ms, p95 %d ms, max %d ms", round, len(times), mean, p95, gaps[len(gaps)-1])
	if mean < 90 || mean > 115 || p95 > 250 {
		t.Errorf("%s: snapshot gaps mean %.1f ms p95 %d ms, want about 100 ms", round, mean, p95)
	}
}

// checkResults requires every bot to have a result, recording its final
// count unless an uplink stall around the end held its last reports past
// the grace period.
func checkResults(t *testing.T, round string, m *bots.Manager, endAt int64) {
	t.Helper()
	var stats []bots.Stats
	deadline := time.Now().Add(10 * time.Second)
	for {
		stats = m.Stats()
		if !slices.ContainsFunc(stats, func(s bots.Stats) bool { return s.Result == nil }) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(stats) != 100 {
		t.Errorf("%s: %d bots running, want 100", round, len(stats))
	}
	window := [2]time.Time{time.UnixMilli(endAt - 300), time.UnixMilli(endAt + 1500)}
	var exact, stalled, finished int
	for _, s := range stats {
		if s.Result == nil {
			t.Errorf("%s: %s got no result", round, s.Name)
			continue
		}
		if s.Result.Finished {
			finished++
		}
		if s.Result.TotalShakes == s.Final {
			exact++
			continue
		}
		if slices.ContainsFunc(s.Stalls, func(iv bots.Interval) bool {
			return iv.From.Before(window[1]) && iv.To.After(window[0])
		}) {
			stalled++
			continue
		}
		t.Errorf("%s: %s result %d shakes, counted %d (rate %.1f/s, latency %v)",
			round, s.Name, s.Result.TotalShakes, s.Final, s.Rate, s.Latency)
	}
	t.Logf("%s: %d exact, %d short after a stall at the end, %d finished", round, exact, stalled, finished)
	if round == "race" && finished < 15 {
		t.Errorf("race: %d finished, want at least 15", finished)
	}
}
