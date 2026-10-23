package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// Tunnel is a running cloudflared quick tunnel. Release it with Stop.
type Tunnel struct {
	url  string
	cmd  *exec.Cmd
	done chan struct{}
	once sync.Once
}

// Start runs bin as a quick tunnel to localURL and waits, until ctx is
// done, for cloudflared to report its public URL. On failure cloudflared
// is killed before Start returns.
func Start(ctx context.Context, bin, localURL string) (*Tunnel, error) {
	cmd := exec.Command(bin, "tunnel", "--no-autoupdate", "--url", localURL)
	setProcAttr(cmd)
	out, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("tunnel: start cloudflared: %w", err)
	}
	t := &Tunnel{cmd: cmd, done: make(chan struct{})}

	found := make(chan string, 1)
	var (
		mu   sync.Mutex
		tail []string // last lines, for the error message
	)
	go func() {
		defer close(t.done)
		sc := bufio.NewScanner(out)
		reported := false
		for sc.Scan() {
			line := sc.Text()
			if !reported {
				if u, ok := matchURL(line); ok {
					reported = true
					found <- u
					continue
				}
				mu.Lock()
				if tail = append(tail, line); len(tail) > 3 {
					tail = tail[1:]
				}
				mu.Unlock()
			}
		}
		// Keep draining until EOF so cloudflared never blocks on a full
		// pipe; EOF means it has exited.
		cmd.Wait()
	}()

	select {
	case t.url = <-found:
		return t, nil
	case <-t.done:
		mu.Lock()
		defer mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrNoURL, strings.Join(tail, " | "))
	case <-ctx.Done():
		t.Stop()
		return nil, fmt.Errorf("tunnel: waiting for cloudflared URL: %w", ctx.Err())
	}
}

// URL is the public https://*.trycloudflare.com base URL.
func (t *Tunnel) URL() string { return t.url }

// Done is closed once cloudflared has exited.
func (t *Tunnel) Done() <-chan struct{} { return t.done }

// Stop kills cloudflared and waits for it to exit. It is safe to call more
// than once and from multiple goroutines.
func (t *Tunnel) Stop() {
	t.once.Do(func() {
		if t.cmd.Process != nil {
			t.cmd.Process.Kill()
		}
	})
	<-t.done
}

// Close is Stop, as an io.Closer.
func (t *Tunnel) Close() error {
	t.Stop()
	return nil
}
