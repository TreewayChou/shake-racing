package kiosk

import (
	"os"
	"os/exec"
	"sync"
)

// Browser is a running kiosk browser process.
type Browser struct {
	cmd    *exec.Cmd
	exited chan struct{}
	once   sync.Once
}

// Launch starts the browser at path showing url as a full-screen app window,
// using profileDir as its dedicated profile (created if missing).
func Launch(path, url, profileDir string) (*Browser, error) {
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, err
	}
	cmd := exec.Command(path, Args(url, profileDir)...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	b := &Browser{cmd: cmd, exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(b.exited)
	}()
	return b, nil
}

// Exited is closed when the browser process ends, whether the user closed
// the window or Close killed it.
func (b *Browser) Exited() <-chan struct{} { return b.exited }

// Close kills the browser if it is still running and waits for it to exit.
// It is safe to call more than once.
func (b *Browser) Close() error {
	b.once.Do(func() {
		select {
		case <-b.exited:
		default:
			_ = b.cmd.Process.Kill()
		}
	})
	<-b.exited
	return nil
}
