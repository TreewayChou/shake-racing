package server_test

import (
	"testing"

	"racing/internal/servertest"
)

func TestNicknameIsTrimmed(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	h.Phone().Join("  阿華\t ")

	h.Tick()
	snap := host.WaitSnapshot(func(s servertest.Snapshot) bool { return len(s.Players) == 1 })
	if got := snap.Players[0].Name; got != "阿華" {
		t.Errorf("name = %q, want 阿華", got)
	}
}

func TestNicknameLengthLimits(t *testing.T) {
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"a", true},
		{"12345678", true},
		{"一二三四五六七八", true}, // 8 characters, 24 bytes
		{"  12345678  ", true},
		{"", false},
		{"   ", false},
		{"123456789", false},
		{"一二三四五六七八九", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := servertest.Start(t)
			phone := h.Phone()
			phone.SendJoin(tc.name)
			m := phone.Recv()
			switch {
			case tc.ok && m.Type != "welcome":
				t.Fatalf("got %s, want welcome: %s", m.Type, m.Raw)
			case !tc.ok && m.Type != "error":
				t.Fatalf("got %s, want error: %s", m.Type, m.Raw)
			}
			if !tc.ok {
				var e servertest.Error
				m.Decode(t, &e)
				if e.Reason != "name" {
					t.Errorf("reason = %q, want name", e.Reason)
				}
			}
		})
	}
}

func TestRejectedNicknameCanRetry(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phone := h.Phone()

	phone.SendJoin("太長的暱稱超過八個字")
	if m := phone.Recv(); m.Type != "error" {
		t.Fatalf("got %s, want error", m.Type)
	}
	w := phone.Join("短名")

	h.Tick()
	snap := host.WaitSnapshot(func(s servertest.Snapshot) bool { return len(s.Players) > 0 })
	if len(snap.Players) != 1 || snap.Players[0].ID != w.PlayerID {
		t.Errorf("lobby = %+v, want only %s", snap.Players, w.PlayerID)
	}
}
