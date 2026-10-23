package server

import "racing/internal/game"

// Wire protocol: JSON messages tagged by "type".

// envelope is decoded first to dispatch on the message type.
type envelope struct {
	Type string `json:"type"`
}

// Phone -> server.

type joinMsg struct {
	Name     string `json:"name"`
	PlayerID string `json:"playerId,omitempty"`
}

// Server -> phone.

type welcomeMsg struct {
	Type      string      `json:"type"` // "welcome"
	PlayerID  string      `json:"playerId"`
	Constants detectorMsg `json:"constants"`
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

type errorMsg struct {
	Type   string `json:"type"` // "error"
	Reason string `json:"reason"`
}

// Server -> host.

type snapshotMsg struct {
	Type    string           `json:"type"` // "snapshot"
	Phase   string           `json:"phase"`
	Players []snapshotPlayer `json:"players"`
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
	ID   string `json:"id"`
	Name string `json:"name"`
}
