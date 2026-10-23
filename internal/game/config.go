package game

import (
	"errors"
	"slices"
	"time"
)

// Mode is one cell of the 個人/分隊 × 限時/競速 matrix.
type Mode string

const (
	// IndividualTimed: every phone counts for exactly Duration from the
	// synced start; ranked by total shakes.
	IndividualTimed Mode = "individual-timed"

	// IndividualRace: first to Target shakes; the round ends when the top
	// TopN have finished, at TimeCap, or when the host ends it.
	IndividualRace Mode = "individual-race"

	// TeamTimed: as IndividualTimed, but teams win by average shakes per
	// member.
	TeamTimed Mode = "team-timed"

	// TeamRace: racing ends the moment the first team's average reaches
	// Target, or when the host ends it. There is no time cap.
	TeamRace Mode = "team-race"
)

// Team reports whether m is a 分隊 mode, scored by team averages.
func (m Mode) Team() bool { return m == TeamTimed || m == TeamRace }

// Race reports whether m is a 競速 mode: racing to a target rather than
// counting for a fixed duration.
func (m Mode) Race() bool { return m == IndividualRace || m == TeamRace }

// Durations are the timed-mode round lengths the host can pick.
var Durations = []time.Duration{15 * time.Second, 30 * time.Second, 45 * time.Second, 60 * time.Second}

// Targets are the race-mode shake targets the host can pick.
var Targets = []int{100, 200, 300, 500}

// TimeCaps are the individual-race time caps the host can pick.
var TimeCaps = []time.Duration{60 * time.Second, 90 * time.Second, 120 * time.Second, 180 * time.Second}

// Config is the host's round configuration. Every option is kept whatever
// the mode, so switching modes back and forth keeps the host's picks.
type Config struct {
	Mode     Mode
	Duration time.Duration // timed modes
	Target   int           // race modes: shakes to finish
	TimeCap  time.Duration // individual race: longest the round may run
}

// DefaultConfig is the configuration a new room starts with.
var DefaultConfig = Config{Mode: IndividualTimed, Duration: 30 * time.Second, Target: 200, TimeCap: 90 * time.Second}

// ErrInvalidConfig is returned for a mode or option the host cannot pick.
var ErrInvalidConfig = errors.New("game: invalid round configuration")

// ErrWrongPhase is returned for a command not allowed in the current phase.
var ErrWrongPhase = errors.New("game: not allowed in this phase")

func (c Config) valid() bool {
	switch c.Mode {
	case IndividualTimed, IndividualRace, TeamTimed, TeamRace:
	default:
		return false
	}
	return slices.Contains(Durations, c.Duration) && slices.Contains(Targets, c.Target) && slices.Contains(TimeCaps, c.TimeCap)
}

// Config returns the current round configuration.
func (e *Engine) Config() Config { return e.config }

// SetConfig replaces the round configuration. It is allowed only in the
// lobby, so a running round never changes under the players.
func (e *Engine) SetConfig(c Config) error {
	e.sync()
	if e.phase != PhaseLobby {
		return ErrWrongPhase
	}
	if !c.valid() {
		return ErrInvalidConfig
	}
	e.config = c
	return nil
}
