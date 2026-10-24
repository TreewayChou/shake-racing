package server_test

import (
	"testing"
	"time"

	"racing/internal/servertest"
)

// The spec's start lead: the countdown ends about 3.5 s after Start.
const startLeadMs = 3500

func TestStartBroadcastsAbsoluteCountdown(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phone := h.Phone()
	phone.Join("小明")
	pressedAt := h.NowMs()

	host.Start()

	cd := phone.ExpectCountdown()
	if cd.StartAt != pressedAt+startLeadMs || cd.DurationMs != 30000 {
		t.Errorf("countdown = %+v, want startAt %d durationMs 30000", cd, pressedAt+startLeadMs)
	}
	s := host.NextSnapshot(h)
	if s.Phase != "countdown" || s.StartAt != pressedAt+startLeadMs {
		t.Errorf("snapshot phase %q startAt %d, want countdown %d", s.Phase, s.StartAt, pressedAt+startLeadMs)
	}
}

const graceMs = 1500

func TestTimedRoundPhaseFlow(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phone := h.Phone()
	phone.Join("小明")
	host.SetConfig(map[string]any{"durationMs": 15000})
	if s := host.NextSnapshot(h); s.Phase != "lobby" {
		t.Fatalf("phase = %q, want lobby", s.Phase)
	}
	host.Start()
	startAt := phone.ExpectCountdown().StartAt

	// Each step ends one tick (100 ms) before a phase boundary, then the
	// next snapshot lands exactly on it.
	steps := []struct {
		before, after string
		at            int64 // boundary on the server clock
	}{
		{"countdown", "racing", startAt},
		{"racing", "grace", startAt + 15000},
		{"grace", "results", startAt + 15000 + graceMs},
	}
	for _, st := range steps {
		h.Advance(time.Duration(st.at-h.NowMs())*time.Millisecond - 2*servertest.TickInterval)
		if s := host.NextSnapshot(h); s.Phase != st.before {
			t.Fatalf("100 ms before %s: phase %q, want %q", st.after, s.Phase, st.before)
		}
		if s := host.NextSnapshot(h); s.Phase != st.after {
			t.Fatalf("at %s boundary: phase %q, want %q", st.after, s.Phase, st.after)
		}
		if st.after == "grace" {
			if s := host.NextSnapshot(h); s.EndAt != startAt+15000 || s.GraceMs != graceMs {
				t.Errorf("grace snapshot endAt %d graceMs %d, want %d %d", s.EndAt, s.GraceMs, startAt+15000, graceMs)
			}
		}
	}
	phone.ExpectEnd()
	if r := phone.ExpectResult(); r.Rank != 1 || r.Players != 1 {
		t.Errorf("result = %+v, want rank 1 of 1", r)
	}
}

func TestStartNeedsAPlayer(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()

	host.Start()

	if s := host.NextSnapshot(h); s.Phase != "lobby" {
		t.Errorf("phase = %q after Start with nobody joined, want lobby", s.Phase)
	}
}

func TestConfigIsFrozenOutsideTheLobby(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	h.Phone().Join("小明")
	host.Start()

	host.SetConfig(map[string]any{"durationMs": 15000})

	if s := host.NextSnapshot(h); s.DurationMs != 30000 {
		t.Errorf("durationMs = %d after mid-round setConfig, want 30000", s.DurationMs)
	}
}
