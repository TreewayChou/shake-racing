package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"

	"racing/internal/bots"
	"racing/internal/game"
)

// BotsPerAdd is how many bots +10 機器人 adds.
const BotsPerAdd = 10

// botsMsg is the host's bot control: add 10, fill to capacity, or clear.
type botsMsg struct {
	Action string `json:"action"` // "add", "fill" or "clear"
}

// SetBotURL enables the host's bot controls. Bots connect to the phone
// endpoint at base, this server's own loopback http:// base URL, so they
// take the same path as phones.
func (s *Server) SetBotURL(base string) {
	url := "ws" + strings.TrimPrefix(strings.TrimRight(base, "/"), "http") + "/ws/play?bot=" + s.botToken
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bots != nil {
		s.bots.Close()
	}
	s.bots = bots.New(url, s.clock, s.botCfg)
}

// Bots returns the bot manager, or nil until SetBotURL.
func (s *Server) Bots() *bots.Manager {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bots
}

func (s *Server) botsLocked(data []byte) {
	var m botsMsg
	if s.bots == nil || json.Unmarshal(data, &m) != nil {
		return
	}
	free := game.MaxPlayers - len(s.engine.Snapshot().Players) - s.bots.Pending()
	switch m.Action {
	case "add":
		s.bots.Add(max(0, min(BotsPerAdd, free)))
	case "fill":
		s.bots.Add(max(0, free))
	case "clear":
		s.clearBotsLocked()
	}
}

// clearBotsLocked removes every bot from the roster and disconnects it.
// Real players are untouched.
func (s *Server) clearBotsLocked() {
	for ph := range s.botPhones {
		if ph.id != "" {
			s.kickLocked(ph.id)
		}
		ph.c.cancel() // a join already read is refused in servePhone
	}
	s.bots.Clear()
}

func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
