package server

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
	Type     string `json:"type"` // "welcome"
	PlayerID string `json:"playerId"`
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

type snapshotPlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
