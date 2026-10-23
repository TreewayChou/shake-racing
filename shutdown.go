package main

import (
	"context"
	"errors"
	"io"
)

// closerFunc adapts a function to io.Closer.
type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// waitForStop blocks until the process should shut down: a signal cancels
// ctx, the kiosk browser exits (a nil channel means no kiosk), or the HTTP
// server fails. It returns a human-readable reason, or the server error.
func waitForStop(ctx context.Context, browserExited <-chan struct{}, serveErr <-chan error) (string, error) {
	select {
	case err := <-serveErr:
		return "", err
	case <-browserExited:
		return "kiosk window closed", nil
	case <-ctx.Done():
		return "interrupted", nil
	}
}

// shutdown closes every non-nil closer in order, even if some fail, and
// returns their errors joined.
func shutdown(closers ...io.Closer) error {
	var errs []error
	for _, c := range closers {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
