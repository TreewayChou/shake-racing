package game

import (
	"errors"
	"slices"
	"strconv"
	"time"
)

// MaxPlayers is the room's capacity, bots included.
const MaxPlayers = 100

// OfflineAfter is how long a player may go unseen before being shown as
// offline: two missed 15 s heartbeats plus slack.
const OfflineAfter = 35 * time.Second

// ErrFull is returned when a new player joins a room at MaxPlayers.
var ErrFull = errors.New("game: room is full")

// uniqueName returns name, or name with the first free "#n" suffix (n ≥ 2)
// if another player already has it.
func (e *Engine) uniqueName(name string) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(e.players, func(p *entry) bool { return p.Name == n })
	}
	if !taken(name) {
		return name
	}
	for i := 2; ; i++ {
		if n := name + "#" + strconv.Itoa(i); !taken(n) {
			return n
		}
	}
}

// view is p as seen from outside at now.
func (e *Engine) view(p *entry, now time.Time) Player {
	v := p.Player
	v.Waiting = !p.inRound && e.phase != PhaseLobby
	v.Offline = now.Sub(p.lastSeen) >= OfflineAfter
	return v
}

// Player returns the roster entry with the given id.
func (e *Engine) Player(id string) (Player, bool) {
	now := e.sync()
	p := e.find(id)
	if p == nil {
		return Player{}, false
	}
	return e.view(p, now), true
}

// Seen records that player id's phone is alive (any message counts).
func (e *Engine) Seen(id string) {
	now := e.sync()
	if p := e.find(id); p != nil {
		p.lastSeen = now
	}
}

// Kick removes player id from the roster, mid-round included, and reports
// whether it was there.
func (e *Engine) Kick(id string) bool {
	e.sync()
	i := slices.IndexFunc(e.players, func(p *entry) bool { return p.ID == id })
	if i < 0 {
		return false
	}
	e.players = slices.Delete(e.players, i, i+1)
	e.standings = slices.DeleteFunc(e.standings, func(s Standing) bool { return s.ID == id })
	return true
}

// Result returns player id's final standing and the number of players
// ranked, once the round is in results.
func (e *Engine) Result(id string) (Standing, int, bool) {
	e.sync()
	if e.phase != PhaseResults {
		return Standing{}, 0, false
	}
	for _, s := range e.standings {
		if s.ID == id {
			return s, len(e.standings), true
		}
	}
	return Standing{}, 0, false
}

// Reset abandons any round and returns to the lobby with the same roster.
// It is allowed in every phase.
func (e *Engine) Reset() {
	e.sync()
	e.toLobby()
	e.events = nil // nothing pending from the abandoned round is delivered
}
