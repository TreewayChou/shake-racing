package servertest

// Client-side reading of the team protocol: reshuffle, team aggregates,
// MVP and team results.

// Shuffle presses 🔀 重新隨機分隊 and waits until it is handled.
func (h *Host) Shuffle() {
	h.t.Helper()
	h.Send(map[string]any{"type": "shuffle"})
	h.Echo()
}

// ExpectTeam waits for team{team}: the phone's team after a reshuffle.
func (p *Phone) ExpectTeam() string {
	p.t.Helper()
	var m struct {
		Team string `json:"team"`
	}
	p.Expect("team").Decode(p.t, &m)
	return m.Team
}
