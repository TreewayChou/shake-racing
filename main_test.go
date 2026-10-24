package main

import (
	"io"
	"testing"
)

func TestFlagDefaults(t *testing.T) {
	cfg, err := parseFlags(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := config{Port: 3000}
	if cfg != want {
		t.Errorf("defaults = %+v, want %+v", cfg, want)
	}
}

func TestFlagsParsed(t *testing.T) {
	cfg, err := parseFlags([]string{"-port", "8080", "-no-tunnel", "-no-kiosk", "-public-url", "https://game.example.com"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := config{Port: 8080, NoTunnel: true, NoKiosk: true, PublicURL: "https://game.example.com"}
	if cfg != want {
		t.Errorf("parsed = %+v, want %+v", cfg, want)
	}
}

func TestBadFlagRejected(t *testing.T) {
	if _, err := parseFlags([]string{"-port", "nope"}, io.Discard); err == nil {
		t.Error("want error for non-numeric port")
	}
	if _, err := parseFlags([]string{"-port", "70000"}, io.Discard); err == nil {
		t.Error("want error for out-of-range port")
	}
}
