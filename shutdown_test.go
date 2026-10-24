package main

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestWaitForStopOnBrowserExit(t *testing.T) {
	exited := make(chan struct{})
	close(exited)
	reason, err := waitForStop(context.Background(), exited, make(chan error))
	if err != nil || reason != "kiosk window closed" {
		t.Errorf("waitForStop = %q, %v", reason, err)
	}
}

func TestWaitForStopOnSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A nil exited channel (no kiosk) never fires.
	reason, err := waitForStop(ctx, nil, make(chan error))
	if err != nil || reason != "interrupted" {
		t.Errorf("waitForStop = %q, %v", reason, err)
	}
}

func TestWaitForStopOnServeError(t *testing.T) {
	errc := make(chan error, 1)
	boom := errors.New("listener died")
	errc <- boom
	if _, err := waitForStop(context.Background(), nil, errc); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

func TestShutdownClosesAllInOrderAndJoinsErrors(t *testing.T) {
	var order []string
	closer := func(name string, err error) io.Closer {
		return closerFunc(func() error { order = append(order, name); return err })
	}
	bad := errors.New("tunnel stuck")
	err := shutdown(closer("browser", nil), nil, closer("tunnel", bad), closer("http", nil))
	if !errors.Is(err, bad) {
		t.Errorf("err = %v, want it to wrap %v", err, bad)
	}
	want := []string{"browser", "tunnel", "http"}
	if len(order) != len(want) {
		t.Fatalf("closed %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("closed %v, want %v", order, want)
		}
	}
}
