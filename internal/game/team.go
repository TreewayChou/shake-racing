package game

import (
	"math"
	"math/rand/v2"
	"slices"
	"time"
)

// Team is one of the four 分隊 colours. Every player has one from the
// moment they join, whatever the mode, so switching to a team mode needs
// no rejoin.
type Team string

const (
	Red    Team = "red"
	Blue   Team = "blue"
	Green  Team = "green"
	Yellow Team = "yellow"
)

// Teams are the four teams in lane order, which is also the tie-break
// when two teams are equally small or score the same average.
var Teams = []Team{Red, Blue, Green, Yellow}

// MVPN is the length of the individual MVP list in team modes.
const MVPN = 5

// TeamScore is a team's standing: its score is the average shakes per
// member, so team size does not decide the winner.
type TeamScore struct {
	Team    Team
	Members int // round participants; the roster in the lobby
	Total   int
	Avg     float64
	Rank    int // 1-based; 0 with no members
}

// teamScores scores players by team, in lane order. Teams rank by
// average, in every team mode; equal averages go to the earlier lane.
func teamScores(players []*entry) []TeamScore {
	out := make([]TeamScore, len(Teams))
	for i, t := range Teams {
		out[i].Team = t
	}
	for _, p := range players {
		for i := range out {
			if out[i].Team == p.Team {
				out[i].Members++
				out[i].Total += p.Total
			}
		}
	}
	for i := range out {
		if out[i].Members > 0 {
			out[i].Avg = float64(out[i].Total) / float64(out[i].Members)
		}
	}
	for i := range out {
		if out[i].Members == 0 {
			continue
		}
		out[i].Rank = 1
		for j := range out {
			if out[j].Members > 0 && (out[j].Avg > out[i].Avg || j < i && out[j].Avg == out[i].Avg) {
				out[i].Rank++
			}
		}
	}
	return out
}

// teams is the live (or, in results, final) team scoreboard; nil outside
// team modes.
func (e *Engine) teams() []TeamScore {
	switch {
	case !e.config.Mode.Team():
		return nil
	case e.phase == PhaseLobby:
		return teamScores(e.players)
	case e.phase == PhaseResults:
		return slices.Clone(e.teamResults)
	}
	return teamScores(e.participants())
}

// teamRaceRules: racing ends the moment the first team's average reaches
// Target. Reports still count through the grace period, and teams then
// rank by final average, so a team that got there first can be
// overtaken. Players rank individually as in a timed round, for the MVP.
type teamRaceRules struct{}

func (teamRaceRules) ended(r Round, now time.Time, in []*entry) (time.Time, bool) {
	for _, ts := range teamScores(in) {
		if ts.Members > 0 && ts.Avg >= float64(r.Config.Target) {
			return now, true
		}
	}
	return time.Time{}, false
}

func (teamRaceRules) limit(Round) time.Duration { return math.MaxInt64 } // no time cap
func (teamRaceRules) less(a, b *entry) bool     { return timedRules{}.less(a, b) }
func (teamRaceRules) target(Round) int          { return 0 }

// Shuffle is the host's 🔀 重新隨機分隊: the roster is dealt into the four
// teams in random order, so sizes end within 1. Allowed only in the lobby.
func (e *Engine) Shuffle() error {
	e.sync()
	if e.phase != PhaseLobby {
		return ErrWrongPhase
	}
	order := rand.Perm(len(e.players))
	for i, j := range order {
		e.players[j].Team = Teams[i%len(Teams)]
	}
	return nil
}

// teamSizes counts the roster per team.
func (e *Engine) teamSizes() map[Team]int {
	n := make(map[Team]int, len(Teams))
	for _, p := range e.players {
		n[p.Team]++
	}
	return n
}

// smallestTeam is the team with the fewest players in the roster; ties go
// to the earlier lane.
func (e *Engine) smallestTeam() Team {
	n := e.teamSizes()
	best := Teams[0]
	for _, t := range Teams[1:] {
		if n[t] < n[best] {
			best = t
		}
	}
	return best
}

// largestTeam is the team with the most players in the roster; ties go to
// the earlier lane.
func (e *Engine) largestTeam() Team {
	n := e.teamSizes()
	best := Teams[0]
	for _, t := range Teams[1:] {
		if n[t] > n[best] {
			best = t
		}
	}
	return best
}

// rebalance moves players, the latest joiner of the largest team each
// time, to the smallest team until sizes are within 1. It returns the
// players moved, in roster order.
func (e *Engine) rebalance() []*entry {
	var moved []*entry
	for {
		from, to := e.largestTeam(), e.smallestTeam()
		n := e.teamSizes()
		if n[from]-n[to] <= 1 {
			break
		}
		var last *entry
		for _, p := range e.players {
			if p.Team == from && (last == nil || p.seq > last.seq) {
				last = p
			}
		}
		last.Team = to
		if !slices.Contains(moved, last) {
			moved = append(moved, last)
		}
	}
	slices.SortFunc(moved, func(a, b *entry) int { return slices.Index(e.players, a) - slices.Index(e.players, b) })
	return moved
}
