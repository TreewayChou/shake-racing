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
