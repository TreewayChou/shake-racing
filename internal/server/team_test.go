package server_test

import (
	"fmt"
	"testing"
	"time"

	"racing/internal/servertest"
)

var lanes = []string{"red", "blue", "green", "yellow"}

// teamSizes counts the snapshot's players per team.
func teamSizes(s servertest.Snapshot) map[string]int {
	n := map[string]int{}
	for _, p := range s.Players {
		n[p.Team]++
	}
	return n
}

func TestJoinsAreBalancedAcrossTheFourTeams(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	got := map[string]string{} // player id -> team from welcome
	for i := range 10 {
		w := h.Phone().Join(fmt.Sprintf("p%d", i))
		got[w.PlayerID] = w.Team
		// Each join goes to a smallest team: sizes stay within 1.
		n := map[string]int{}
		for _, team := range got {
			n[team]++
		}
		lo, hi := 1<<30, 0
		for _, l := range lanes {
			lo, hi = min(lo, n[l]), max(hi, n[l])
		}
		if hi-lo > 1 || len(n) > 4 {
			t.Fatalf("after %d joins team sizes = %v, want 4 teams within 1", i+1, n)
		}
	}
	s := host.NextSnapshot(h)
	for _, p := range s.Players {
		if p.Team != got[p.ID] {
			t.Errorf("%s: snapshot team %q, welcome said %q", p.Name, p.Team, got[p.ID])
		}
	}
	if n := teamSizes(s); n["red"] != 3 || n["blue"] != 3 || n["green"] != 2 || n["yellow"] != 2 {
		t.Errorf("10 players: team sizes = %v, want red 3 blue 3 green 2 yellow 2", n)
	}
}

func TestReshuffleRebalancesAndTellsEveryPhone(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phones := map[string]*servertest.Phone{}
	var ids []string
	for i := range 12 {
		p := h.Phone()
		id := p.Join(fmt.Sprintf("p%d", i)).PlayerID
		phones[id] = p
		ids = append(ids, id)
	}
	// Players 0, 4 and 8 went to red: kicking them mid-round leaves red
	// empty, and nothing rebalances until the reshuffle.
	host.Start()
	for _, i := range []int{0, 4, 8} {
		host.Kick(ids[i])
		delete(phones, ids[i])
	}
	host.Reset()
	if n := teamSizes(host.NextSnapshot(h)); n["red"] != 0 {
		t.Fatalf("after kicks red has %d, want 0", n["red"])
	}

	host.Shuffle()

	s := host.NextSnapshot(h)
	n := teamSizes(s)
	for _, l := range lanes {
		if n[l] < 2 || n[l] > 3 {
			t.Errorf("after reshuffle team sizes = %v, want 9 players within 1 across 4 teams", n)
			break
		}
	}
	for _, p := range s.Players {
		if got := phones[p.ID].ExpectTeam(); got != p.Team {
			t.Errorf("%s: phone told %q, snapshot says %q", p.Name, got, p.Team)
		}
	}
}

// assertBalanced fails unless team sizes in s are within 1 and every
// phone told of a new team agrees with the snapshot.
func assertBalanced(t *testing.T, s servertest.Snapshot, told map[string]string) {
	t.Helper()
	n := teamSizes(s)
	lo, hi := 1<<30, 0
	for _, l := range lanes {
		lo, hi = min(lo, n[l]), max(hi, n[l])
	}
	if hi-lo > 1 {
		t.Errorf("team sizes = %v, want within 1", n)
	}
	for _, p := range s.Players {
		if team, ok := told[p.ID]; ok && team != p.Team {
			t.Errorf("%s: phone told %q, snapshot says %q", p.Name, team, p.Team)
		}
	}
}

// expectTeams reads each phone's queued messages up to an echo barrier and
// returns the last team each was told, by player id.
func expectTeams(t *testing.T, phones map[string]*servertest.Phone) map[string]string {
	t.Helper()
	told := map[string]string{}
	for id, p := range phones {
		p.Send(map[string]any{"type": "echo", "id": -1})
		for m := p.Recv(); m.Type != "echo"; m = p.Recv() {
			if m.Type == "team" {
				var tm struct {
					Team string `json:"team"`
				}
				m.Decode(t, &tm)
				told[id] = tm.Team
			}
		}
	}
	return told
}

func TestKickInTheLobbyRebalancesTeamsAndTellsMovedPhones(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phones := map[string]*servertest.Phone{}
	var ids []string
	for i := range 8 {
		p := h.Phone()
		id := p.Join(fmt.Sprintf("p%d", i)).PlayerID
		phones[id] = p
		ids = append(ids, id)
	}
	// p0 and p4 are red: kicking both would leave red empty.
	for _, i := range []int{0, 4} {
		host.Kick(ids[i])
		phones[ids[i]].ExpectKicked()
		delete(phones, ids[i])
	}

	s := host.NextSnapshot(h)
	told := expectTeams(t, phones)
	assertBalanced(t, s, told)
	if len(told) == 0 {
		t.Error("no phone was told of a new team")
	}
}

func TestClearingBotsInTheLobbyRebalancesTeams(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phones := map[string]*servertest.Phone{}
	var ids []string
	for i := range 8 {
		p := h.Phone()
		id := p.Join(fmt.Sprintf("p%d", i)).PlayerID
		phones[id] = p
		ids = append(ids, id)
	}
	host.Bots("add")
	host.WaitPlayers(h, 18)
	// Kicked mid-round, red's p0 and p4 are not replaced; back in the lobby
	// red is down to its bots.
	host.Start()
	for _, i := range []int{0, 4} {
		host.Kick(ids[i])
		delete(phones, ids[i])
	}
	host.Reset()

	host.Bots("clear")
	s := host.WaitPlayers(h, 6)
	assertBalanced(t, s, expectTeams(t, phones))
}

// startTeamRound joins p0…p(n-1) (so p0 red, p1 blue, p2 green, p3 yellow,
// p4 red, …), configures a team mode and advances to the synced start.
func startTeamRound(t *testing.T, n int, cfg map[string]any) *race {
	t.Helper()
	r := &race{h: servertest.Start(t)}
	r.host = r.h.Host()
	for i := range n {
		p := r.h.Phone()
		r.ids = append(r.ids, p.Join(fmt.Sprintf("p%d", i)).PlayerID)
		r.phones = append(r.phones, p)
	}
	r.host.SetConfig(cfg)
	r.host.Start()
	r.startAt = r.phones[0].ExpectCountdown().StartAt
	r.advanceTo(0)
	return r
}

// report sends each phone's total at elapsedMs and waits until handled.
func (r *race) report(elapsedMs int64, totals ...int) {
	for i, n := range totals {
		r.phones[i].Progress(n, elapsedMs)
	}
	for i := range totals {
		r.phones[i].Echo()
	}
}

func teamOf(s servertest.Snapshot, team string) servertest.TeamScore {
	for _, ts := range s.Teams {
		if ts.Team == team {
			return ts
		}
	}
	return servertest.TeamScore{}
}

func TestTeamModeConfigReachesPhones(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phone := h.Phone()
	phone.Join("小明")
	phone.ExpectConfig()

	host.SetConfig(map[string]any{"mode": "team-timed", "durationMs": 45000})
	if got, want := phone.ExpectConfig(), (servertest.Config{Mode: "team-timed", DurationMs: 45000}); got != want {
		t.Errorf("team timed config = %+v, want %+v", got, want)
	}
	host.SetConfig(map[string]any{"mode": "team-race", "target": 300})
	if got, want := phone.ExpectConfig(), (servertest.Config{Mode: "team-race", Target: 300}); got != want {
		t.Errorf("team race config = %+v, want %+v", got, want)
	}
	s := host.NextSnapshot(h)
	if s.Mode != "team-race" || len(s.Teams) != 4 {
		t.Fatalf("snapshot mode %q with %d teams, want team-race with 4 lanes", s.Mode, len(s.Teams))
	}
	for i, ts := range s.Teams {
		if ts.Team != lanes[i] {
			t.Errorf("lane %d = %q, want %q", i, ts.Team, lanes[i])
		}
	}
	if ts := teamOf(s, "red"); ts.Members != 1 {
		t.Errorf("red lane = %+v, want 1 member", ts)
	}
}

func TestTeamTimedWinnerIsTheHighestAverageNotTheHighestSum(t *testing.T) {
	r := startTeamRound(t, 5, map[string]any{"mode": "team-timed", "durationMs": 15000})

	r.advanceTo(10000)
	// red: p0 100 + p4 20 = avg 60 (sum 120); blue 70; green 65; yellow 10.
	r.report(10000, 100, 70, 65, 10, 20)

	s := r.host.NextSnapshot(r.h)
	for _, want := range []servertest.TeamScore{
		{Team: "red", Members: 2, Total: 120, Avg: 60, Rank: 3},
		{Team: "blue", Members: 1, Total: 70, Avg: 70, Rank: 1},
		{Team: "green", Members: 1, Total: 65, Avg: 65, Rank: 2},
		{Team: "yellow", Members: 1, Total: 10, Avg: 10, Rank: 4},
	} {
		if got := teamOf(s, want.Team); got != want {
			t.Errorf("live %s = %+v, want %+v", want.Team, got, want)
		}
	}
	wantMVP := []string{"p0", "p1", "p2", "p4", "p3"}
	if len(s.MVP) != 5 {
		t.Fatalf("live MVP has %d players, want 5", len(s.MVP))
	}
	for i, want := range wantMVP {
		if s.MVP[i].Name != want || s.MVP[i].Rank != i+1 {
			t.Errorf("live MVP[%d] = %s rank %d, want %s rank %d", i, s.MVP[i].Name, s.MVP[i].Rank, want, i+1)
		}
	}

	r.advanceTo(15000 + graceMs)
	s = r.host.NextSnapshot(r.h)
	if s.Phase != "results" || teamOf(s, "blue").Rank != 1 || len(s.MVP) != 5 || s.MVP[0].Name != "p0" {
		t.Errorf("results: phase %q, blue %+v, MVP %+v; want blue rank 1 and p0 MVP", s.Phase, teamOf(s, "blue"), s.MVP)
	}
	if res := r.phones[1].ExpectResult(); res.Team != "blue" || res.TeamRank != 1 || res.TeamAvg != 70 || res.Rank != 2 {
		t.Errorf("blue phone result = %+v, want team blue rank 1 avg 70, individual rank 2", res)
	}
	if res := r.phones[4].ExpectResult(); res.Team != "red" || res.TeamRank != 3 || res.TeamAvg != 60 || res.Rank != 4 {
		t.Errorf("red phone result = %+v, want team red rank 3 avg 60, individual rank 4", res)
	}
}

func TestTeamAveragesHandleFractionsAndEmptyTeams(t *testing.T) {
	// red p0+p4, blue p1+p5, green p2, yellow p3; kicking p5 and p3 mid-round
	// leaves blue with 1 and yellow with nobody.
	r := startTeamRound(t, 6, map[string]any{"mode": "team-timed", "durationMs": 15000})
	r.host.Kick(r.ids[5])
	r.host.Kick(r.ids[3])

	r.advanceTo(10000)
	r.report(10000, 30, 0, 0, 0, 1) // red (30+1)/2 = 15.5

	s := r.host.NextSnapshot(r.h)
	if red := teamOf(s, "red"); red.Avg != 15.5 || red.Members != 2 || red.Rank != 1 {
		t.Errorf("red = %+v, want avg 15.5 of 2 members, rank 1", red)
	}
	if y := teamOf(s, "yellow"); y.Members != 0 || y.Avg != 0 || y.Rank != 0 {
		t.Errorf("empty yellow = %+v, want no members, unranked", y)
	}
}

func TestTeamRaceEndsAtTheFirstTeamToTheTargetAndRanksByFinalAverage(t *testing.T) {
	// red p0+p4, blue p1, green p2, yellow p3.
	r := startTeamRound(t, 5, map[string]any{"mode": "team-race", "target": 100})

	r.advanceTo(10000)
	r.report(10000, 100, 99, 10, 10, 90) // red avg 95: nobody's there yet
	if s := r.host.NextSnapshot(r.h); s.Phase != "racing" {
		t.Fatalf("red at 95, blue at 99: phase %q, want racing", s.Phase)
	}

	r.advanceTo(11000)
	crossedAt := r.h.NowMs()
	r.phones[4].Progress(100, 11000) // red (100+100)/2 = 100
	r.phones[4].Echo()
	r.phones[2].ExpectEnd()
	s := r.host.NextSnapshot(r.h)
	if s.Phase != "grace" || s.EndAt != crossedAt {
		t.Errorf("after red reaches 100: phase %q endAt %d, want grace at %d", s.Phase, s.EndAt, crossedAt)
	}

	// Blue's late report arrives in grace: it counts, and once the grace
	// is over teams rank by final average, so blue overtakes red.
	r.phones[1].Progress(120, 11100)
	r.phones[1].Echo()
	r.h.Advance(graceMs * time.Millisecond)

	s = r.host.NextSnapshot(r.h)
	if red, blue := teamOf(s, "red"), teamOf(s, "blue"); blue.Rank != 1 || blue.Avg != 120 || red.Rank != 2 || red.Avg != 100 {
		t.Errorf("results red %+v blue %+v, want blue rank 1 with avg 120, red rank 2 with avg 100", red, blue)
	}
	if s.MVP[0].Name != "p1" || s.MVP[0].Total != 120 {
		t.Errorf("MVP[0] = %+v, want p1 with 120", s.MVP[0])
	}
	if res := r.phones[1].ExpectResult(); res.Team != "blue" || res.TeamRank != 1 || res.TeamAvg != 120 {
		t.Errorf("blue phone result = %+v, want winning team blue with avg 120", res)
	}
	if res := r.phones[0].ExpectResult(); res.Team != "red" || res.TeamRank != 2 || res.TeamAvg != 100 {
		t.Errorf("red phone result = %+v, want team red second with avg 100", res)
	}
}

func TestTeamRaceTieOnFinalAverageGoesToTheEarlierLane(t *testing.T) {
	// red p0, blue p1, green p2, yellow p3.
	r := startTeamRound(t, 4, map[string]any{"mode": "team-race", "target": 100})
	r.advanceTo(10000)
	r.phones[2].Progress(100, 10000) // green reaches the target first
	r.phones[2].Echo()
	r.phones[0].Progress(100, 10050) // red equals it during grace
	r.phones[0].Echo()
	r.h.Advance(graceMs * time.Millisecond)

	s := r.host.NextSnapshot(r.h)
	if red, green := teamOf(s, "red"), teamOf(s, "green"); s.Phase != "results" || red.Rank != 1 || green.Rank != 2 {
		t.Errorf("phase %q red %+v green %+v, want results with red 1, green 2 (equal averages go by lane order)", s.Phase, red, green)
	}
}

func TestTeamRaceHasNoTimeCap(t *testing.T) {
	r := startTeamRound(t, 2, map[string]any{"mode": "team-race", "target": 500, "timeCapMs": 60000})
	r.advanceTo(30000)
	r.report(30000, 300, 200)
	r.advanceTo(180000)
	if s := r.host.NextSnapshot(r.h); s.Phase != "racing" {
		t.Errorf("3 minutes in below the target: phase %q, want racing until the host ends it", s.Phase)
	}
	r.host.End()
	r.phones[0].ExpectEnd()
}

func TestAgainKeepsTeams(t *testing.T) {
	// Kicking leaves the teams uneven; 再來一場 must not rebalance them.
	r := startTeamRound(t, 6, map[string]any{"mode": "team-timed", "durationMs": 15000})
	r.host.Kick(r.ids[2])
	r.host.Kick(r.ids[3])
	before := r.host.NextSnapshot(r.h)
	r.advanceTo(15000 + graceMs)
	r.host.Again()
	after := r.host.NextSnapshot(r.h)
	if after.Phase != "lobby" {
		t.Fatalf("phase %q, want lobby", after.Phase)
	}
	for i, p := range after.Players {
		if p.Team != before.Players[i].Team {
			t.Errorf("%s: team %q after 再來一場, was %q", p.Name, p.Team, before.Players[i].Team)
		}
	}
}

func TestBotsPlayATeamRace(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	host.SetConfig(map[string]any{"mode": "team-race", "target": 100})
	host.Bots("add")
	s := host.WaitPlayers(h, 10)
	if n := teamSizes(s); n["red"] != 3 || n["blue"] != 3 || n["green"] != 2 || n["yellow"] != 2 {
		t.Fatalf("10 bots: team sizes = %v, want red 3 blue 3 green 2 yellow 2", n)
	}

	host.Start()

	s = host.WaitPhase(h, "results")
	if len(s.MVP) != 5 {
		t.Errorf("MVP has %d players, want 5", len(s.MVP))
	}
	winners := 0
	for _, ts := range s.Teams {
		if ts.Rank == 1 {
			winners++
			if ts.Avg < 100 {
				t.Errorf("winner %s avg %.1f, want at least the target", ts.Team, ts.Avg)
			}
		}
	}
	if winners != 1 {
		t.Errorf("teams = %+v, want exactly one winner", s.Teams)
	}
}

func TestIndividualModesCarryNoTeamAggregates(t *testing.T) {
	r := startRace(t, 15000, "甲", "乙")
	if s := r.host.NextSnapshot(r.h); len(s.Teams) != 0 || len(s.MVP) != 0 {
		t.Errorf("individual snapshot teams %v mvp %v, want none", s.Teams, s.MVP)
	}
	r.advanceTo(15000 + graceMs)
	if res := r.phones[0].ExpectResult(); res.Team != "" || res.TeamRank != 0 {
		t.Errorf("individual result = %+v, want no team result", res)
	}
}

func TestReshuffleOnlyInTheLobby(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	for i := range 8 {
		h.Phone().Join(fmt.Sprintf("p%d", i))
	}
	before := host.NextSnapshot(h)
	host.Start()
	host.Shuffle()

	after := host.NextSnapshot(h)
	for i, p := range after.Players {
		if p.Team != before.Players[i].Team {
			t.Errorf("%s moved from %q to %q during the countdown", p.Name, before.Players[i].Team, p.Team)
		}
	}
}
