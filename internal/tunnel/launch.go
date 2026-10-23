package tunnel

import (
	"context"
	"sync"
	"time"
)

// Background brings a tunnel up without blocking the caller. Release it
// with Close, which also cancels a startup still in progress.
type Background struct {
	cancel  context.CancelFunc
	closing chan struct{} // closed by Close
	once    sync.Once
	wg      sync.WaitGroup

	mu  sync.Mutex
	tun *Tunnel
}

// Launch locates cloudflared and starts a quick tunnel to localURL in the
// background, allowing up to timeout (including any download). report is
// called exactly once, from another goroutine, with the public URL or the
// reason the tunnel could not start; it is not called if Close comes first.
// After a successful report, lost is called, from the same goroutine, if
// cloudflared then exits on its own; never after Close.
func Launch(loc Locator, localURL string, timeout time.Duration, report func(url string, err error), lost func()) *Background {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	b := &Background{cancel: cancel, closing: make(chan struct{})}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		tun, err := launch(ctx, loc, localURL)
		closed := ctx.Err() == context.Canceled
		cancel()
		if closed {
			if tun != nil {
				tun.Stop()
			}
			return
		}
		if err != nil {
			report("", err)
			return
		}
		b.mu.Lock()
		b.tun = tun
		b.mu.Unlock()
		report(tun.URL(), nil)
		select {
		case <-b.closing:
		case <-tun.Done():
			select {
			case <-b.closing: // exited as Close began
			default:
				lost()
			}
		}
	}()
	return b
}

func launch(ctx context.Context, loc Locator, localURL string) (*Tunnel, error) {
	bin, err := loc.Locate(ctx)
	if err != nil {
		return nil, err
	}
	return Start(ctx, bin, localURL)
}

// Tunnel returns the running tunnel, or nil if it is not (yet) up.
func (b *Background) Tunnel() *Tunnel {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tun
}

// Close cancels startup if needed, stops cloudflared and waits for it to
// exit. It is safe to call more than once.
func (b *Background) Close() error {
	b.once.Do(func() { close(b.closing) })
	b.cancel()
	b.wg.Wait()
	if t := b.Tunnel(); t != nil {
		t.Stop()
	}
	return nil
}
