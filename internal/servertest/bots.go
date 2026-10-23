package servertest

import (
	"time"
)

// Client-side reading of the bot controls: bots{action} from the host.

// BotRate is how many times a second the harness's bots shake. They have
// an ideal network: no latency, dropped sends or stalls.
const BotRate = 10

// Bots sends bots{action} ("add", "fill" or "clear") and waits until it
// is handled. Bots join asynchronously afterwards.
func (h *Host) Bots(action string) {
	h.t.Helper()
	h.Send(map[string]any{"type": "bots", "action": action})
	h.Echo()
}

// WaitPlayers ticks until a snapshot shows n players in the roster, and
// returns it. The wall-clock bound only guards against hangs.
func (h *Host) WaitPlayers(hs *Harness, n int) Snapshot {
	h.t.Helper()
	deadline := time.Now().Add(Timeout)
	for {
		s := h.NextSnapshot(hs)
		if len(s.Players) == n {
			return s
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("roster has %d players, want %d", len(s.Players), n)
		}
	}
}
