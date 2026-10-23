// Package game is the pure game engine: roster, phases and rules. It does
// no I/O and reads time only from an injected clock. It is not safe for
// concurrent use; the transport layer serialises calls.
package game

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"

	"racing/internal/clock"
)

// MaxNameLen is the maximum nickname length in characters (runes).
const MaxNameLen = 8

// Detector is the phone shake-detector tuning. It is defined here, not in
// the page, so it can be tuned after real-phone tests; the server injects
// it into the phone page.
type Detector struct {
	Accel      float64 // primary: |acceleration| threshold in m/s²
	Delta      float64 // fallback: |Δ|accelerationIncludingGravity|| threshold
	Rearm      float64 // re-arm once the signal drops below Rearm × threshold
	CooldownMs int     // minimum gap between counted shakes
}

// DefaultDetector is the starting tuning: 80 ms cooldown caps every phone
// at 12.5 shakes/s.
var DefaultDetector = Detector{Accel: 15, Delta: 12, Rearm: 0.6, CooldownMs: 80}

// ErrInvalidName is returned when a trimmed nickname is empty or too long.
var ErrInvalidName = errors.New("game: nickname must be 1-8 characters")

// Phase is the room's state-machine phase.
type Phase string

const (
	PhaseLobby Phase = "lobby"
)

// Player is a roster entry as seen from outside the engine.
type Player struct {
	ID   string
	Name string
}

// Snapshot is a point-in-time view of the room.
type Snapshot struct {
	Phase   Phase
	Players []Player
}

// Engine holds all game state for the single room.
type Engine struct {
	clock   clock.Clock
	phase   Phase
	players []*Player
}

// New returns an engine in the lobby phase.
func New(c clock.Clock) *Engine {
	return &Engine{clock: c, phase: PhaseLobby}
}

// Join adds a player with the given nickname and returns the new player.
func (e *Engine) Join(name string) (Player, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > MaxNameLen {
		return Player{}, ErrInvalidName
	}
	p := &Player{ID: newID(), Name: name}
	e.players = append(e.players, p)
	return *p, nil
}

// Warmup reports whether a lobby warm-up shake from player id should be
// shown on the host: the player must be in the roster and the room in the
// lobby.
func (e *Engine) Warmup(id string) bool {
	if e.phase != PhaseLobby {
		return false
	}
	for _, p := range e.players {
		if p.ID == id {
			return true
		}
	}
	return false
}

// Snapshot returns the current room state.
func (e *Engine) Snapshot() Snapshot {
	s := Snapshot{Phase: e.phase, Players: make([]Player, len(e.players))}
	for i, p := range e.players {
		s.Players[i] = *p
	}
	return s
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
