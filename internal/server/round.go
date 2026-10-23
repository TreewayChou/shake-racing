package server

import (
	"encoding/json"
	"slices"
	"time"

	"racing/internal/clock"
	"racing/internal/game"
)

// HeartbeatInterval is how often every phone is pinged. Each ping is also
// a clock-sync sample.
const HeartbeatInterval = 15 * time.Second

const (
	syncBurst   = 5 // pings sent back to back right after a join
	syncSamples = 8 // most recent samples kept per phone
)

// phone is a phone connection; id is empty until it has joined. Its
// fields are guarded by Server.mu.
type phone struct {
	c  *conn
	id string

	pings   map[int64]time.Time // outstanding ping id -> sent at (server clock)
	samples []syncSample        // most recent last
}

// syncSample is one NTP-style ping/pong measurement.
type syncSample struct {
	rtt    time.Duration
	offset time.Duration // phone clock minus server clock
}

// offset is the phone's estimated clock offset: the sample with the lowest
// round trip has the least room for path asymmetry, so it wins.
func (ph *phone) offset() time.Duration {
	if len(ph.samples) == 0 {
		return 0
	}
	best := ph.samples[0]
	for _, s := range ph.samples[1:] {
		if s.rtt < best.rtt {
			best = s
		}
	}
	return best.offset
}

// toLocal converts a server-clock moment to Unix ms on the phone's clock.
func (ph *phone) toLocal(t time.Time) int64 { return t.Add(ph.offset()).UnixMilli() }

// phoneMessage handles a message from a joined phone.
func (s *Server) phoneMessage(ph *phone, typ string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.engine.Seen(ph.id)
	switch typ {
	case "pong":
		var m pongMsg
		if json.Unmarshal(data, &m) != nil {
			return
		}
		s.pongLocked(ph, m)
	case "progress":
		var m progressMsg
		if json.Unmarshal(data, &m) != nil {
			return
		}
		s.engine.Progress(ph.id, game.Report{Total: m.TotalShakes, Elapsed: time.Duration(m.ElapsedMs) * time.Millisecond})
	}
	s.dispatchLocked()
}

// startClockSyncLocked begins estimating ph's clock offset with a burst
// of pings; each pong triggers the next until the burst is done.
func (s *Server) startClockSyncLocked(ph *phone) {
	ph.pings = make(map[int64]time.Time)
	s.pingLocked(ph)
}

func (s *Server) pingLocked(ph *phone) {
	s.pingSeq++
	now := s.clock.Now()
	// Drop pings that were never answered so the map cannot grow.
	for id, at := range ph.pings {
		if now.Sub(at) > 2*HeartbeatInterval {
			delete(ph.pings, id)
		}
	}
	ph.pings[s.pingSeq] = now
	ph.c.send(mustJSON(pingMsg{Type: "ping", ID: s.pingSeq, ServerTs: now.UnixMilli()}))
}

func (s *Server) pongLocked(ph *phone, m pongMsg) {
	sent, ok := ph.pings[m.ID]
	if !ok {
		return
	}
	delete(ph.pings, m.ID)
	recv := s.clock.Now()
	mid := sent.Add(recv.Sub(sent) / 2)
	ph.samples = append(ph.samples, syncSample{
		rtt:    recv.Sub(sent),
		offset: time.UnixMilli(m.ClientTs).Sub(mid),
	})
	if n := len(ph.samples); n > syncSamples {
		ph.samples = ph.samples[n-syncSamples:]
	}
	if _, ok := s.engine.Player(ph.id); ok {
		s.clocks[ph.id] = ph.samples
	}
	if len(ph.samples) < syncBurst {
		s.pingLocked(ph)
	}
}

// heartbeat pings every joined phone each HeartbeatInterval.
func (s *Server) heartbeat(t clock.Ticker) {
	defer s.wg.Done()
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C():
			s.mu.Lock()
			for ph := range s.phones {
				s.pingLocked(ph)
			}
			s.mu.Unlock()
		}
	}
}

// hostMessage handles a host command.
func (s *Server) hostMessage(typ string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch typ {
	case "setConfig":
		var m setConfigMsg
		if json.Unmarshal(data, &m) != nil {
			return
		}
		c := s.engine.Config()
		if m.Mode != nil {
			c.Mode = game.Mode(*m.Mode)
		}
		if m.DurationMs != nil {
			c.Duration = time.Duration(*m.DurationMs) * time.Millisecond
		}
		if m.Target != nil {
			c.Target = *m.Target
		}
		if m.TimeCapMs != nil {
			c.TimeCap = time.Duration(*m.TimeCapMs) * time.Millisecond
		}
		if s.engine.SetConfig(c) != nil {
			return
		}
		s.sendPhonesLocked(mustJSON(newConfigMsg(c)))
	case "start":
		r, err := s.engine.Start()
		if err != nil {
			return
		}
		for ph := range s.phones {
			ph.c.send(mustJSON(countdownMsg{
				Type:        "countdown",
				StartAt:     ph.toLocal(r.StartAt),
				modeOptions: newModeOptions(r.Config),
			}))
		}
	case "end":
		s.engine.End()
	case "bots":
		s.botsLocked(data)
	case "kick":
		var m kickMsg
		if json.Unmarshal(data, &m) == nil {
			s.kickLocked(m.ID)
		}
	case "reset":
		s.engine.Reset()
		s.sendPhonesLocked(mustJSON(newConfigMsg(s.engine.Config())))
	case "shuffle":
		if s.engine.Shuffle() == nil {
			for ph := range s.phones {
				if p, ok := s.engine.Player(ph.id); ok {
					ph.c.send(mustJSON(teamMsg{Type: "team", Team: string(p.Team)}))
				}
			}
		}
	case "again":
		if s.engine.Again() == nil {
			s.sendPhonesLocked(mustJSON(newConfigMsg(s.engine.Config())))
		}
	}
	s.dispatchLocked()
}

func (s *Server) sendPhonesLocked(b []byte) {
	for ph := range s.phones {
		ph.c.send(b)
	}
}

// dispatchLocked delivers the engine's pending events to phones.
func (s *Server) dispatchLocked() {
	for _, ev := range s.engine.Events() {
		switch ev := ev.(type) {
		case game.RoundEnded:
			b := mustJSON(endMsg{Type: "end"})
			for ph := range s.phones {
				if slices.Contains(ev.Participants, ph.id) {
					ph.c.send(b)
				}
			}
		case game.RoundResults:
			byID := make(map[string]game.Standing, len(ev.Standings))
			for _, st := range ev.Standings {
				byID[st.ID] = st
			}
			for ph := range s.phones {
				if st, ok := byID[ph.id]; ok {
					ph.c.send(mustJSON(newResultMsg(st, len(ev.Standings), ev.Teams)))
				}
			}
		}
	}
}

// snapshotLocked builds the host snapshot. Taking it advances the round's
// phases, so callers dispatch events afterwards.
func (s *Server) snapshotLocked() []byte {
	snap := s.engine.Snapshot()
	msg := snapshotMsg{
		Type:       "snapshot",
		Phase:      string(snap.Phase),
		Mode:       string(snap.Config.Mode),
		DurationMs: snap.Config.Duration.Milliseconds(),
		Target:     snap.Config.Target,
		TimeCapMs:  snap.Config.TimeCap.Milliseconds(),
		ServerNow:  snap.At.UnixMilli(),
		GraceMs:    game.Grace.Milliseconds(),
		Players:    make([]snapshotPlayer, len(snap.Players)),
		Top:        make([]snapshotPlayer, len(snap.Top)),
	}
	if !snap.Round.StartAt.IsZero() {
		msg.StartAt = snap.Round.StartAt.UnixMilli()
	}
	if !snap.Round.EndAt.IsZero() {
		msg.EndAt = snap.Round.EndAt.UnixMilli()
	}
	for i, p := range snap.Players {
		msg.Players[i] = snapshotPlayer{ID: p.ID, Name: p.Name, Team: string(p.Team), Total: p.Total, Offline: p.Offline, Waiting: p.Waiting}.withFinish(p)
	}
	for i, st := range snap.Top {
		msg.Top[i] = snapshotPlayer{ID: st.ID, Name: st.Name, Team: string(st.Team), Total: st.Total, Rank: st.Rank}.withFinish(st.Player)
	}
	msg.Teams = newTeamScores(snap.Teams)
	if snap.MVP != nil {
		msg.MVP = msg.Top[:len(snap.MVP)] // the MVP list is the head of the ranking
	}
	return mustJSON(msg)
}
