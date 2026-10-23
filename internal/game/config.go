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
)

// Durations are the timed-mode round lengths the host can pick.
var Durations = []time.Duration{15 * time.Second, 30 * time.Second, 45 * time.Second, 60 * time.Second}

// Config is the host's round configuration.
type Config struct {
	Mode     Mode
	Duration time.Duration // timed modes
}

// DefaultConfig is the configuration a new room starts with.
var DefaultConfig = Config{Mode: IndividualTimed, Duration: 30 * time.Second}

// ErrInvalidConfig is returned for a mode or option the host cannot pick.
var ErrInvalidConfig = errors.New("game: invalid round configuration")

// ErrWrongPhase is returned for a command not allowed in the current phase.
var ErrWrongPhase = errors.New("game: not allowed in this phase")

func (c Config) valid() bool {
	switch c.Mode {
	case IndividualTimed:
		return slices.Contains(Durations, c.Duration)
	}
	return false
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
