package game

import (
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

	crossed int // team race: 1 for the first team to reach the target, 2 next…; 0 if not yet
}

// ahead reports whether a ranks ahead of b: in a team race, teams that
// reached the target rank first, in the order they got there; then by
// average; then in lane order (a and b are given in lane order).
func (a TeamScore) ahead(b TeamScore) bool {
	switch {
	case a.crossed != b.crossed && a.crossed > 0 && (b.crossed == 0 || a.crossed < b.crossed):
		return true
	case a.crossed != b.crossed:
		return false
	}
	return a.Avg > b.Avg
}

// teamScores scores players by team, in lane order. crossed holds the
// team race's crossing order, nil in other modes.
func teamScores(players []*entry, crossed map[Team]int) []TeamScore {
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
		out[i].crossed = crossed[out[i].Team]
	}
	for i := range out {
		if out[i].Members == 0 {
			continue
		}
		out[i].Rank = 1
		for j := range out {
			if out[j].Members > 0 && (out[j].ahead(out[i]) || j < i && !out[i].ahead(out[j])) {
				out[i].Rank++
			}
		}
	}
	return out
}

// noteCrossings records, in a team race, each team whose average has
// reached the target, in the order they get there.
func (e *Engine) noteCrossings() {
	if e.round.Config.Mode != TeamRace {
		return
	}
	for _, ts := range teamScores(e.participants(), nil) {
		if _, ok := e.crossed[ts.Team]; !ok && ts.Members > 0 && ts.Avg >= float64(e.round.Config.Target) {
			e.crossed[ts.Team] = len(e.crossed) + 1
		}
	}
}

// teams is the live (or, in results, final) team scoreboard; nil outside
// team modes.
func (e *Engine) teams() []TeamScore {
	switch {
	case !e.config.Mode.Team():
		return nil
	case e.phase == PhaseLobby:
		return teamScores(e.players, nil)
	case e.phase == PhaseResults:
		return slices.Clone(e.teamResults)
	}
	return teamScores(e.participants(), e.crossed)
}

// teamRaceRules: racing ends the moment the first team's average reaches
// Target, and that team wins. Players rank individually as in a timed
// round, for the MVP.
type teamRaceRules struct{}

func (teamRaceRules) ended(r Round, now time.Time, in []*entry) (time.Time, bool) {
	for _, ts := range teamScores(in, nil) {
		if ts.Members > 0 && ts.Avg >= float64(r.Config.Target) {
			return now, true
		}
	}
	return time.Time{}, false
}

func (teamRaceRules) counts(Round, time.Duration) bool { return true }
func (teamRaceRules) less(a, b *entry) bool            { return timedRules{}.less(a, b) }
func (teamRaceRules) target(Round) int                 { return 0 }

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

// smallestTeam is the team with the fewest players in the roster; ties go
// to the earlier lane.
func (e *Engine) smallestTeam() Team {
	n := make(map[Team]int, len(Teams))
	for _, p := range e.players {
		n[p.Team]++
	}
	best := Teams[0]
	for _, t := range Teams[1:] {
		if n[t] < n[best] {
			best = t
		}
	}
	return best
}
