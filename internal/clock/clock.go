// Package clock abstracts time so the game engine and server can run
// against a fake clock in tests.
package clock

import (
	"sync"
	"time"
)

// Clock is the source of time for the engine and server.
type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
}

// Ticker delivers ticks on C, like time.Ticker.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// Real returns the wall clock.
func Real() Clock { return realClock{} }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTicker(d time.Duration) Ticker { return realTicker{time.NewTicker(d)} }

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }

// Fake is a manually advanced clock. Tickers fire only when Advance moves
// time past their next deadline; like time.Ticker, a slow reader drops ticks.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

// NewFake returns a Fake clock set to start.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic("clock: non-positive ticker interval")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTicker{clock: f, period: d, next: f.now.Add(d), c: make(chan time.Time, 1)}
	f.tickers = append(f.tickers, t)
	return t
}

// Advance moves time forward by d, firing every ticker whose deadline passes.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	for _, t := range f.tickers {
		for !t.next.After(f.now) {
			select {
			case t.c <- t.next:
			default:
			}
			t.next = t.next.Add(t.period)
		}
	}
}

type fakeTicker struct {
	clock  *Fake
	period time.Duration
	next   time.Time
	c      chan time.Time
}

func (t *fakeTicker) C() <-chan time.Time { return t.c }

func (t *fakeTicker) Stop() {
	f := t.clock
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, other := range f.tickers {
		if other == t {
			f.tickers = append(f.tickers[:i], f.tickers[i+1:]...)
			return
		}
	}
}
