package server_test

import (
	"testing"

	"racing/internal/servertest"
)

func TestWarmupShakePulsesHostCard(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phone := h.Phone()
	w := phone.Join("小明")

	phone.Warmup()

	if id := host.ExpectWarmup(); id != w.PlayerID {
		t.Errorf("warmup id = %q, want %q", id, w.PlayerID)
	}
}

func TestWarmupBeforeJoinIsIgnored(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phone := h.Phone()

	phone.Warmup() // not joined yet: must not reach the host
	w := phone.Join("小明")
	phone.Warmup()

	if id := host.ExpectWarmup(); id != w.PlayerID {
		t.Errorf("first warmup id = %q, want %q", id, w.PlayerID)
	}
}
