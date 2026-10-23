package server

import (
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
	ph.c.send(mustJSON(welcomeMsg{Type: "welcome", PlayerID: p.ID, Name: p.Name, Constants: newDetectorMsg(game.DefaultDetector)}))
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
			ph.c.send(mustJSON(newResultMsg(st, n)))
		}
	}
	return nil
}

// kickLocked removes player id and tells its phone.
func (s *Server) kickLocked(id string) {
	if !s.engine.Kick(id) {
		return
	}
	delete(s.clocks, id)
	for ph := range s.phones {
		if ph.id == id {
			ph.c.send(mustJSON(signalMsg{Type: "kicked"}))
			delete(s.phones, ph)
		}
	}
}
