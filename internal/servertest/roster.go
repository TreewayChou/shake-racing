package servertest

// Client-side reading of the roster protocol: reconnect, late join,
// capacity, kick, liveness and reset.

// SendRejoin sends join{name, playerId} without waiting for a reply: a
// phone that stored its playerId reconnecting after a refresh or drop.
func (p *Phone) SendRejoin(name, playerID string) {
	p.t.Helper()
	p.Send(map[string]any{"type": "join", "name": name, "playerId": playerID})
}

// Rejoin sends join{name, playerId} and requires a welcome.
func (p *Phone) Rejoin(name, playerID string) Welcome {
	p.t.Helper()
	p.SendRejoin(name, playerID)
	m := p.Recv()
	if m.Type != "welcome" {
		p.t.Fatalf("rejoin %q: got %s, want welcome: %s", name, m.Type, m.Raw)
	}
	var w Welcome
	m.Decode(p.t, &w)
	return w
}

// ExpectWait waits for wait: the round is under way, so this phone joins
// the next one.
func (p *Phone) ExpectWait() {
	p.t.Helper()
	p.Expect("wait")
}

// ExpectKicked waits for kicked: the host removed this player.
func (p *Phone) ExpectKicked() {
	p.t.Helper()
	p.Expect("kicked")
}

// Kick removes a player from the roster and waits until it is handled.
func (h *Host) Kick(id string) {
	h.t.Helper()
	h.Send(map[string]any{"type": "kick", "id": id})
	h.Echo()
}

// Reset returns the room to the lobby from any phase and waits until it
// is handled.
func (h *Host) Reset() {
	h.t.Helper()
	h.Send(map[string]any{"type": "reset"})
	h.Echo()
}

// Player returns the roster entry with the given id, or false.
func (s Snapshot) Player(id string) (SnapshotPlayer, bool) {
	for _, p := range s.Players {
		if p.ID == id {
			return p, true
		}
	}
	return SnapshotPlayer{}, false
}
