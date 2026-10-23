package server_test

import (
	"testing"

	"racing/internal/servertest"
)

func TestPhoneJoinAppearsInHostLobby(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phone := h.Phone()

	welcome := phone.Join("小明")
	if welcome.PlayerID == "" {
		t.Fatal("welcome carried no playerId")
	}

	h.Tick()
	snap := host.WaitSnapshot(func(s servertest.Snapshot) bool { return len(s.Players) == 1 })
	if snap.Phase != "lobby" {
		t.Errorf("phase = %q, want lobby", snap.Phase)
	}
	got := snap.Players[0]
	if got.ID != welcome.PlayerID || got.Name != "小明" {
		t.Errorf("lobby player = %+v, want id %q name 小明", got, welcome.PlayerID)
	}
}
