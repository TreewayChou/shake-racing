package server_test

import (
	"encoding/json"
	"io"
	"regexp"
	"testing"

	"racing/internal/servertest"
)

// The starting detector tuning from the spec: |acceleration| >= 15 m/s²,
// fallback |Δ accelerationIncludingGravity| >= 12, re-arm below 60 %,
// 80 ms cooldown.
var specDetector = servertest.Detector{Accel: 15, Delta: 12, Rearm: 0.6, CooldownMs: 80}

func TestPlayPageCarriesDetectorConstants(t *testing.T) {
	h := servertest.Start(t)
	body, _ := io.ReadAll(h.Get("/play", nil).Body)

	m := regexp.MustCompile(`var K=(\{[^}]*\})`).FindSubmatch(body)
	if m == nil {
		t.Fatal("/play has no injected `var K={...}` detector constants")
	}
	var got servertest.Detector
	if err := json.Unmarshal(m[1], &got); err != nil {
		t.Fatalf("constants %s: %v", m[1], err)
	}
	if got != specDetector {
		t.Errorf("page constants = %+v, want %+v", got, specDetector)
	}
}

func TestWelcomeCarriesDetectorConstants(t *testing.T) {
	h := servertest.Start(t)
	w := h.Phone().Join("小明")
	if w.Constants != specDetector {
		t.Errorf("welcome constants = %+v, want %+v", w.Constants, specDetector)
	}
}
