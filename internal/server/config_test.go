package server_test

import (
	"testing"

	"racing/internal/servertest"
)

func TestPhoneReceivesDefaultConfigOnJoin(t *testing.T) {
	h := servertest.Start(t)
	phone := h.Phone()
	phone.Join("小明")

	got := phone.ExpectConfig()
	if want := (servertest.Config{Mode: "individual-timed", DurationMs: 30000}); got != want {
		t.Errorf("config = %+v, want %+v", got, want)
	}
}

func TestDurationChangeInLobbyReachesPhones(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phone := h.Phone()
	phone.Join("小明")
	phone.ExpectConfig()

	host.SetConfig(map[string]any{"durationMs": 45000})

	if got := phone.ExpectConfig(); got.DurationMs != 45000 {
		t.Errorf("config durationMs = %d, want 45000", got.DurationMs)
	}
	if s := host.NextSnapshot(h); s.DurationMs != 45000 || s.Mode != "individual-timed" {
		t.Errorf("snapshot mode/duration = %q/%d, want individual-timed/45000", s.Mode, s.DurationMs)
	}
}

func TestOnlyOfferedDurationsAreAccepted(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()

	for _, ms := range []int{15000, 60000, 20000, 0, -15000, 120000} {
		host.SetConfig(map[string]any{"durationMs": ms})
	}

	if s := host.NextSnapshot(h); s.DurationMs != 60000 {
		t.Errorf("durationMs = %d, want 60000 (last offered value)", s.DurationMs)
	}
}
