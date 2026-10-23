package server

import (
	"slices"

	"racing/internal/game"
)

// joinLocked admits ph as a new player or, when m carries the playerId of
// one in the roster, as that player reconnecting. A reconnecting phone
// replaces any older connection for the same player and is brought back
// into the round: countdown with its kept score while racing, result
// once ranked. A new player joining mid-round gets wait.
func (s *Server) joinLocked(ph *phone, m joinMsg) error {
	p, err := s.engine.Join(m.Name, m.PlayerID)
	if err != nil {
		return err
	}
	// Deliver what Join's clock sync produced before ph is reachable, so
	// ph is told the current state below exactly once.
	s.dispatchLocked()
	for old := range s.phones {
		if old.id == p.ID {
			delete(s.phones, old)
			old.c.cancel()
		}
	}
	ph.id = p.ID
	s.phones[ph] = struct{}{}
	ph.c.send(mustJSON(welcomeMsg{Type: "welcome", PlayerID: p.ID, Name: p.Name, Team: string(p.Team), Constants: newDetectorMsg(game.DefaultDetector)}))
	ph.c.send(mustJSON(newConfigMsg(s.engine.Config())))
	s.startClockSyncLocked(ph)
	ph.samples = s.clocks[p.ID] // a reconnect keeps its clock estimate

	if p.Waiting {
		ph.c.send(mustJSON(signalMsg{Type: "wait"}))
		return nil
	}
	snap := s.engine.Snapshot()
	switch snap.Phase {
	case game.PhaseCountdown, game.PhaseRacing, game.PhaseGrace:
		r := snap.Round
		ph.c.send(mustJSON(countdownMsg{
			Type:        "countdown",
			StartAt:     ph.toLocal(r.StartAt),
			modeOptions: newModeOptions(r.Config),
			Total:       p.Total,
			FinishedMs:  p.Finish.Milliseconds(),
		}))
		if snap.Phase == game.PhaseGrace {
			ph.c.send(mustJSON(endMsg{Type: "end"}))
		}
	case game.PhaseResults:
		if st, n, ok := s.engine.Result(p.ID); ok {
			ph.c.send(mustJSON(newResultMsg(st, n, snap.Teams)))
		}
	}
	return nil
}

// kickLocked removes the given players and tells their phones; phones of
// players moved by the lobby rebalance are told their new team.
func (s *Server) kickLocked(ids ...string) {
	kicked, moved := s.engine.Kick(ids...)
	for _, id := range kicked {
		delete(s.clocks, id)
	}
	for ph := range s.phones {
		if slices.Contains(kicked, ph.id) {
			ph.c.send(mustJSON(signalMsg{Type: "kicked"}))
			delete(s.phones, ph)
		}
	}
	s.sendTeamsLocked(moved)
}

// sendTeamsLocked tells each player's phone its team.
func (s *Server) sendTeamsLocked(players []game.Player) {
	for ph := range s.phones {
		for _, p := range players {
			if p.ID == ph.id {
				ph.c.send(mustJSON(teamMsg{Type: "team", Team: string(p.Team)}))
			}
		}
	}
}
