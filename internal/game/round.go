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
// Teams are the final team scores in team modes, nil otherwise.
type RoundResults struct {
	Standings []Standing
	Teams     []TeamScore
}

func (RoundEnded) event()   {}
func (RoundResults) event() {}

// rules is what varies between modes: when racing ends, which reports
// count, and how participants are ordered.
type rules interface {
	// ended reports whether racing is over at now, and when it ended.
	ended(r Round, now time.Time, in []*entry) (time.Time, bool)
	// counts reports whether a report taken at phone-elapsed time counts.
	counts(r Round, elapsed time.Duration) bool
	// less orders a ahead of b.
	less(a, b *entry) bool
	// target is the total that finishes the round for a player; 0 if the
	// mode has none.
	target(r Round) int
}

func rulesFor(m Mode) rules {
	switch m {
	case IndividualTimed, TeamTimed:
		return timedRules{}
	case IndividualRace:
		return raceRules{}
	case TeamRace:
		return teamRaceRules{}
	}
	panic("game: no rules for mode " + string(m))
}

// timedRules: everyone races for exactly Duration; most shakes wins.
type timedRules struct{}

func (timedRules) ended(r Round, now time.Time, _ []*entry) (time.Time, bool) {
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

func (timedRules) target(Round) int { return 0 }

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
		p.clearRound()
		p.inRound = true
	}
	e.phase = PhaseCountdown
	e.rules = rulesFor(e.config.Mode)
	e.round = Round{Config: e.config, StartAt: e.clock.Now().Add(StartLead)}
	e.standings, e.teamResults = nil, nil
	return e.round, nil
}

// End is the host's 結束: racing ends now, whatever the mode, and the
// grace period begins.
func (e *Engine) End() error {
	now := e.sync()
	if e.phase != PhaseRacing {
		return ErrWrongPhase
	}
	e.round.EndAt = now
	e.phase = PhaseGrace
	e.events = append(e.events, RoundEnded{})
	return nil
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
	e.standings, e.teamResults = nil, nil
	for _, p := range e.players {
		p.clearRound()
		p.inRound = false
	}
}

// Progress records a participant's cumulative report. Reports are
// accepted while racing and during grace, subject to light validation:
// totals never decrease, are clamped to the rate cap, and reports whose
// elapsed time is negative, implausibly far ahead of the server, or not
// counted by the mode's rules are ignored. In modes with a target, totals
// are clamped to it, and the first report that reaches it is the
// player's finish, timed by that report's phone-measured elapsed time.
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
	target := e.rules.target(e.round)
	if target > 0 {
		total = min(total, target)
	}
	if total <= p.Total {
		return
	}
	p.Total, p.reached = total, rep.Elapsed
	if target > 0 && total == target {
		p.Finished, p.Finish, p.finishedAt = true, rep.Elapsed, now
	}
	e.sync() // a finish, or a team average reaching the target, may end the round
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
			end, ok := e.rules.ended(e.round, now, e.participants())
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
			if e.config.Mode.Team() {
				e.teamResults = teamScores(e.participants())
			}
			e.events = append(e.events, RoundResults{Standings: slices.Clone(e.standings), Teams: slices.Clone(e.teamResults)})
		default:
			return now
		}
	}
}

// participants are the players taking part in the current round.
func (e *Engine) participants() []*entry {
	var in []*entry
	for _, p := range e.players {
		if p.inRound {
			in = append(in, p)
		}
	}
	return in
}

// ranking orders the round's participants.
func (e *Engine) ranking() []Standing {
	in := e.participants()
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
