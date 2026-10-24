package server

import (
	"encoding/json"

	"racing/internal/game"
)

// Wire protocol: JSON messages tagged by "type". Timestamps are Unix
// milliseconds; durations are milliseconds.

// envelope is decoded first to dispatch on the message type.
type envelope struct {
	Type string          `json:"type"`
	ID   json.RawMessage `json:"id,omitempty"` // echo only
}

// echoMsg answers echo{id} on any endpoint once all earlier messages on
// that connection have been handled. Tests and bots use it as a barrier.
type echoMsg struct {
	Type string          `json:"type"` // "echo"
	ID   json.RawMessage `json:"id,omitempty"`
}

// Phone -> server.

type joinMsg struct {
	Name     string `json:"name"`
	PlayerID string `json:"playerId,omitempty"`
}

// pongMsg answers ping{id} with the phone's clock reading (Date.now()).
type pongMsg struct {
	ID       int64 `json:"id"`
	ClientTs int64 `json:"clientTs"`
}

// progressMsg is a phone's cumulative count since the synced start,
// sent every 150 ms while racing.
type progressMsg struct {
	TotalShakes int   `json:"totalShakes"`
	ElapsedMs   int64 `json:"elapsedMs"`
}

// Server -> phone.

// endMsg: time is up; results follow after the grace period.
type endMsg struct {
	Type string `json:"type"` // "end"
}

// resultMsg is a participant's final standing.
type resultMsg struct {
	Type        string `json:"type"` // "result"
	Rank        int    `json:"rank"`
	TotalShakes int    `json:"totalShakes"`
	Players     int    `json:"players"`             // participants ranked
	Finished    bool   `json:"finished,omitempty"`  // race: reached the target
	ElapsedMs   int64  `json:"elapsedMs,omitempty"` // race: phone-measured finish time

	// Team modes: the player's team and its final place and average.
	Team     string  `json:"team,omitempty"`
	TeamRank int     `json:"teamRank,omitempty"`
	TeamAvg  float64 `json:"teamAvg,omitempty"`
}

// pingMsg is a heartbeat and clock-sync probe.
type pingMsg struct {
	Type     string `json:"type"` // "ping"
	ID       int64  `json:"id"`
	ServerTs int64  `json:"serverTs"`
}

type welcomeMsg struct {
	Type      string      `json:"type"` // "welcome"
	PlayerID  string      `json:"playerId"`
	Name      string      `json:"name"` // deduplicated; the kept name on a reconnect
	Team      string      `json:"team"` // red, blue, green or yellow
	Constants detectorMsg `json:"constants"`
}

// teamMsg tells a phone its new team after the host reshuffles.
type teamMsg struct {
	Type string `json:"type"` // "team"
	Team string `json:"team"`
}

// detectorMsg is the shake-detector tuning, sent in welcome and injected
// into the phone page as `var K={...}`.
type detectorMsg struct {
	Accel      float64 `json:"accel"`
	Delta      float64 `json:"delta"`
	Rearm      float64 `json:"rearm"`
	CooldownMs int     `json:"cooldownMs"`
}

func newDetectorMsg(d game.Detector) detectorMsg {
	return detectorMsg{Accel: d.Accel, Delta: d.Delta, Rearm: d.Rearm, CooldownMs: d.CooldownMs}
}

// configMsg is sent after welcome and whenever the host changes the
// configuration.
type configMsg struct {
	Type string `json:"type"` // "config"
	Mode string `json:"mode"`
	modeOptions
}

// modeOptions are the options that apply to a mode: durationMs for timed
// modes, target and timeCapMs for race.
type modeOptions struct {
	DurationMs int64 `json:"durationMs,omitempty"`
	Target     int   `json:"target,omitempty"`
	TimeCapMs  int64 `json:"timeCapMs,omitempty"`
}

func newModeOptions(c game.Config) modeOptions {
	switch c.Mode {
	case game.IndividualRace:
		return modeOptions{Target: c.Target, TimeCapMs: c.TimeCap.Milliseconds()}
	case game.TeamRace:
		return modeOptions{Target: c.Target}
	}
	return modeOptions{DurationMs: c.Duration.Milliseconds()}
}

func newConfigMsg(c game.Config) configMsg {
	return configMsg{Type: "config", Mode: string(c.Mode), modeOptions: newModeOptions(c)}
}

// countdownMsg announces the synced start moment, converted to the
// receiving phone's own clock, and the round's mode options.
type countdownMsg struct {
	Type    string `json:"type"` // "countdown"
	StartAt int64  `json:"startAt"`
	modeOptions
	Total int `json:"totalShakes,omitempty"` // score kept, when resuming after a reconnect
	// Race finish kept, when resuming after a reconnect.
	FinishedMs int64 `json:"finishedMs,omitempty"`
}

// signalMsg is a message with no payload: wait (joined mid-round, races
// in the next one), full (room at capacity) or kicked (removed by the host).
type signalMsg struct {
	Type string `json:"type"`
}

type errorMsg struct {
	Type   string `json:"type"` // "error"
	Reason string `json:"reason"`
}

// Host -> server.

// setConfigMsg changes the round configuration; omitted fields keep their
// current value.
type setConfigMsg struct {
	Mode       *string `json:"mode"`
	DurationMs *int64  `json:"durationMs"`
	Target     *int    `json:"target"`
	TimeCapMs  *int64  `json:"timeCapMs"`
}

// kickMsg removes a player from the roster.
type kickMsg struct {
	ID string `json:"id"`
}

// Server -> host.

type snapshotMsg struct {
	Type       string           `json:"type"` // "snapshot"
	Phase      string           `json:"phase"`
	Mode       string           `json:"mode"`
	DurationMs int64            `json:"durationMs"` // every option, whatever the mode,
	Target     int              `json:"target"`     // so the settings panel shows
	TimeCapMs  int64            `json:"timeCapMs"`  // the host's picks
	ServerNow  int64            `json:"serverNow"`
	StartAt    int64            `json:"startAt,omitempty"` // server clock
	EndAt      int64            `json:"endAt,omitempty"`   // racing end, server clock
	GraceMs    int64            `json:"graceMs"`
	Players    []snapshotPlayer `json:"players"`
	Top        []snapshotPlayer `json:"top"` // ranked, at most game.TopN

	// Team modes only.
	Teams []teamScore      `json:"teams,omitempty"` // lanes in order red, blue, green, yellow
	MVP   []snapshotPlayer `json:"mvp,omitempty"`   // individual Top 5, live or final
}

// teamScore is one team lane; its score is the average per member.
type teamScore struct {
	Team    string  `json:"team"`
	Members int     `json:"members"`
	Total   int     `json:"total"`
	Avg     float64 `json:"avg"`
	Rank    int     `json:"rank"` // 0 for a team with nobody
}

// warmupMsg is sent to hosts as soon as a lobby player warm-up shakes.
type warmupMsg struct {
	Type string `json:"type"` // "warmup"
	ID   string `json:"id"`
}

// urlsMsg tells hosts where players join: full /play URLs.
type urlsMsg struct {
	Type       string `json:"type"` // "urls"
	Public     string `json:"public"`
	LAN        string `json:"lan"`
	TunnelMode string `json:"tunnelMode"`
}

type snapshotPlayer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Total int    `json:"total"`
	Rank  int    `json:"rank,omitempty"`
	Team  string `json:"team"`

	Offline bool `json:"offline,omitempty"` // missed 2 heartbeats
	Waiting bool `json:"waiting,omitempty"` // joined mid-round

	Finished  bool  `json:"finished,omitempty"`  // race: reached the target
	ElapsedMs int64 `json:"elapsedMs,omitempty"` // race: phone-measured finish time
}

// withFinish adds p's race finish, if any, to sp.
func (sp snapshotPlayer) withFinish(p game.Player) snapshotPlayer {
	sp.Finished, sp.ElapsedMs = p.Finished, p.Finish.Milliseconds()
	return sp
}

// newResultMsg is a participant's final standing among n ranked, with
// their team's final score when teams (team modes) is non-nil.
func newResultMsg(st game.Standing, n int, teams []game.TeamScore) resultMsg {
	m := resultMsg{
		Type: "result", Rank: st.Rank, TotalShakes: st.Total, Players: n,
		Finished: st.Finished, ElapsedMs: st.Finish.Milliseconds(),
	}
	for _, ts := range teams {
		if ts.Team == st.Team {
			m.Team, m.TeamRank, m.TeamAvg = string(ts.Team), ts.Rank, ts.Avg
		}
	}
	return m
}

// newTeamScores is the snapshot's team lanes; nil outside team modes.
func newTeamScores(teams []game.TeamScore) []teamScore {
	if teams == nil {
		return nil
	}
	out := make([]teamScore, len(teams))
	for i, ts := range teams {
		out[i] = teamScore{Team: string(ts.Team), Members: ts.Members, Total: ts.Total, Avg: ts.Avg, Rank: ts.Rank}
	}
	return out
}
