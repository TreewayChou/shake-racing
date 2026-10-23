package game

import (
	"errors"
	"slices"
	"time"
)

// Phases of a round: lobby → countdown → racing → grace → results.
const (
	PhaseCountdown Phase = "countdown"
	PhaseRacing    Phase = "racing"
	PhaseGrace     Phase = "grace"
	PhaseResults   Phase = "results"
)

const (
	// StartLead is how far ahead of Start the synced start moment is set,
	// so every phone has time to receive it and count down.
	StartLead = 3500 * time.Millisecond

	// Grace is how long after racing ends late progress is still accepted,
	// so slow networks can deliver their final totals.
	Grace = 1500 * time.Millisecond

	// ElapsedTolerance is how far a phone's elapsedMs may run ahead of the
	// time the server has observed since the start (clock-sync error).
	// Reports further ahead are implausible and ignored.
	ElapsedTolerance = time.Second

	// RateSlack is the number of shakes a report may exceed the rate cap by
	// before it is clamped.
	RateSlack = 3

	// TopN is the length of the live leaderboard.
	TopN = 15
)

// MaxShakesPerSecond is the most a phone can count: one shake per detector
// cooldown (12.5/s with the 80 ms default).
var MaxShakesPerSecond = 1000 / float64(DefaultDetector.CooldownMs)

// ErrNoPlayers is returned when Start is pressed with nobody joined.
var ErrNoPlayers = errors.New("game: no players to race")

// Round describes the round in progress.
type Round struct {
	Config  Config
	StartAt time.Time // the synced start moment, on the server clock
	EndAt   time.Time // when racing ended; zero until then
}

// Report is a phone's cumulative progress since the synced start.
type Report struct {
	Total   int           // shakes counted since StartAt
	Elapsed time.Duration // phone-measured time since StartAt
}

// Standing is a player's place in a ranking.
type Standing struct {
	Player
	Rank int // 1-based
}

// Event is something the transport must tell clients about. Drain them
// with Events after every command.
type Event interface{ event() }

// RoundEnded: racing is over and the grace period has begun.
type RoundEnded struct{}

// RoundResults: the round is ranked. Standings cover every participant.
type RoundResults struct{ Standings []Standing }

func (RoundEnded) event()   {}
func (RoundResults) event() {}

// rules is what varies between modes: when racing ends, which reports
// count, and how participants are ordered.
type rules interface {
	// ended reports whether racing is over at now, and when it ended.
	ended(r Round, now time.Time) (time.Time, bool)
	// counts reports whether a report taken at phone-elapsed time counts.
	counts(r Round, elapsed time.Duration) bool
	// less orders a ahead of b.
	less(a, b *entry) bool
}

func rulesFor(m Mode) rules {
	switch m {
	case IndividualTimed:
		return timedRules{}
	}
	panic("game: no rules for mode " + string(m))
}

// timedRules: everyone races for exactly Duration; most shakes wins.
type timedRules struct{}

func (timedRules) ended(r Round, now time.Time) (time.Time, bool) {
	end := r.StartAt.Add(r.Config.Duration)
	return end, !now.Before(end)
}

func (timedRules) counts(r Round, elapsed time.Duration) bool {
	return elapsed <= r.Config.Duration
}

// less ranks by total; equal totals go to whoever reached it first on
// their own clock, then to the earlier joiner.
func (timedRules) less(a, b *entry) bool {
	if a.Total != b.Total {
		return a.Total > b.Total
	}
	if a.reached != b.reached {
		return a.reached < b.reached
	}
	return a.seq < b.seq
}

// Start begins a round from the lobby: the countdown runs until StartAt.
// Everyone in the roster takes part.
func (e *Engine) Start() (Round, error) {
	e.sync()
	if e.phase != PhaseLobby {
		return Round{}, ErrWrongPhase
	}
	if len(e.players) == 0 {
		return Round{}, ErrNoPlayers
	}
	for _, p := range e.players {
		p.Total, p.reached, p.inRound = 0, 0, true
	}
	e.phase = PhaseCountdown
	e.rules = rulesFor(e.config.Mode)
	e.round = Round{Config: e.config, StartAt: e.clock.Now().Add(StartLead)}
	e.standings = nil
	return e.round, nil
}

// Again returns from results to the lobby with the same roster.
func (e *Engine) Again() error {
	e.sync()
	if e.phase != PhaseResults {
		return ErrWrongPhase
	}
	e.toLobby()
	return nil
}

// toLobby ends the round, keeping the roster.
func (e *Engine) toLobby() {
	e.phase = PhaseLobby
	e.round = Round{}
	e.standings = nil
	for _, p := range e.players {
		p.Total, p.reached, p.inRound = 0, 0, false
	}
}

// Progress records a participant's cumulative report. Reports are
// accepted while racing and during grace, subject to light validation:
// totals never decrease, are clamped to the rate cap, and reports whose
// elapsed time is negative, implausibly far ahead of the server, or not
// counted by the mode's rules are ignored.
func (e *Engine) Progress(id string, rep Report) {
	now := e.sync()
	if e.phase != PhaseRacing && e.phase != PhaseGrace {
		return
	}
	p := e.find(id)
	if p == nil || !p.inRound {
		return
	}
	observed := now.Sub(e.round.StartAt)
	if rep.Elapsed < 0 || rep.Elapsed > observed+ElapsedTolerance || !e.rules.counts(e.round, rep.Elapsed) {
		return
	}
	total := min(rep.Total, int(MaxShakesPerSecond*rep.Elapsed.Seconds())+RateSlack)
	if total <= p.Total {
		return
	}
	p.Total, p.reached = total, rep.Elapsed
}

// Events returns and clears the pending events.
func (e *Engine) Events() []Event {
	ev := e.events
	e.events = nil
	return ev
}

// sync advances the phase state machine to the clock's current time,
// running through every boundary that has passed, and returns that time.
func (e *Engine) sync() (now time.Time) {
	now = e.clock.Now()
	for {
		switch e.phase {
		case PhaseCountdown:
			if now.Before(e.round.StartAt) {
				return now
			}
			e.phase = PhaseRacing
		case PhaseRacing:
			end, ok := e.rules.ended(e.round, now)
			if !ok {
				return now
			}
			e.round.EndAt = end
			e.phase = PhaseGrace
			e.events = append(e.events, RoundEnded{})
		case PhaseGrace:
			if now.Before(e.round.EndAt.Add(Grace)) {
				return now
			}
			e.phase = PhaseResults
			e.standings = e.ranking()
			e.events = append(e.events, RoundResults{Standings: slices.Clone(e.standings)})
		default:
			return now
		}
	}
}

// ranking orders the round's participants.
func (e *Engine) ranking() []Standing {
	var in []*entry
	for _, p := range e.players {
		if p.inRound {
			in = append(in, p)
		}
	}
	slices.SortStableFunc(in, func(a, b *entry) int {
		switch {
		case e.rules.less(a, b):
			return -1
		case e.rules.less(b, a):
			return 1
		}
		return 0
	})
	out := make([]Standing, len(in))
	for i, p := range in {
		out[i] = Standing{Player: p.Player, Rank: i + 1}
	}
	return out
}

// top is the live (or, in results, final) leaderboard.
func (e *Engine) top() []Standing {
	var s []Standing
	switch e.phase {
	case PhaseLobby:
		return nil
	case PhaseResults:
		s = e.standings
	default:
		s = e.ranking()
	}
	return slices.Clone(s[:min(len(s), TopN)])
}
