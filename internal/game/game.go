// Package game is the pure game engine: roster, phases and rules. It does
// no I/O and reads time only from an injected clock. It is not safe for
// concurrent use; the transport layer serialises calls.
package game

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
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
	ID    string
	Name  string
	Total int // shakes accepted this round
}

// entry is the engine's own roster record.
type entry struct {
	Player
	seq     int           // join order, the last tie-break
	inRound bool          // takes part in the current round
	reached time.Duration // phone-elapsed time when Total was reached
}

// Snapshot is a point-in-time view of the room. Taking one advances the
// phase state machine, so drain Events afterwards.
type Snapshot struct {
	At      time.Time // the moment the snapshot describes
	Phase   Phase
	Config  Config
	Round   Round // zero in the lobby
	Players []Player
	Top     []Standing // live leaderboard, final in results; at most TopN
}

// Engine holds all game state for the single room.
type Engine struct {
	clock     clock.Clock
	phase     Phase
	config    Config
	round     Round // valid outside the lobby
	rules     rules // the round's mode rules; valid outside the lobby
	players   []*entry
	joins     int
	standings []Standing // final ranking, in results
	events    []Event
}

// New returns an engine in the lobby phase.
func New(c clock.Clock) *Engine {
	return &Engine{clock: c, phase: PhaseLobby, config: DefaultConfig}
}

func (e *Engine) find(id string) *entry {
	for _, p := range e.players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// Join adds a player with the given nickname and returns the new player.
func (e *Engine) Join(name string) (Player, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > MaxNameLen {
		return Player{}, ErrInvalidName
	}
	e.sync()
	e.joins++
	p := &entry{Player: Player{ID: newID(), Name: name}, seq: e.joins}
	e.players = append(e.players, p)
	return p.Player, nil
}

// Warmup reports whether a lobby warm-up shake from player id should be
// shown on the host: the player must be in the roster and the room in the
// lobby.
func (e *Engine) Warmup(id string) bool {
	e.sync()
	return e.phase == PhaseLobby && e.find(id) != nil
}

// Snapshot returns the current room state.
func (e *Engine) Snapshot() Snapshot {
	now := e.sync()
	s := Snapshot{At: now, Phase: e.phase, Config: e.config, Players: make([]Player, len(e.players)), Top: e.top()}
	if e.phase != PhaseLobby {
		s.Round = e.round
	}
	for i, p := range e.players {
		s.Players[i] = p.Player
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
