package exp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Mat-Greaves/run"
)

// StartHTTPServer starts a [HTTPServer] returning once the server is ready to accept traffic.
//
// err will be not nil if the server was never ready to accept traffic, either not starting or not passing
// the health checks.
//
// stop must be called to wait for the server to gracefully terminate.
func StartHTTPServer(ctx context.Context, h http.Handler, addr string) (err error, stop func() error) {
	return run.Start(ctx, HTTPServer(h, addr), Poller(addr, PollHTTP))
}

// HTTPServer returns a [Runner] that starts a basic HTTP server serving h at addr.
//
// HTTPServer will shut down gracefully when the ctx passed to Run is cancelled.
func HTTPServer(h http.Handler, addr string) run.Runner {
	return run.Func(func(ctx context.Context) error {
		s := http.Server{
			Addr:    addr,
			Handler: h,
		}

		var serr = make(chan error)
		go func() {
			serr <- s.ListenAndServe()
		}()

		select {
		case err := <-serr:
			return fmt.Errorf("run.HTTPServer server exited with error: %w", err)
		case <-ctx.Done():
		}

		// Give 5 seconds for connections to wrap up. [Group] gives a max 10secs before the process is deemed
		// misbehaving and not exited cleanly.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), run.ShutdownTimeout/2)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		if ctx.Err() != context.Canceled {
			return ctx.Err()
		}
		return nil
	})
}

type PollMode int

const (
	PollHTTP PollMode = iota
)

const (
	pollInitial = 10 * time.Millisecond
	pollMax     = 1 * time.Second
	pokeTimeout = 200 * time.Millisecond
)

// Poller returns a [Runner] that polls addr until it looks ready to accept connections.
//
// Ready to accept connections is defined by mode as follows:
//
//	mode=PollHTTP: Listening on addr and sends a response `OPTIONS *` request other than 502 Bad Gateway or 504 Gateway Timeout.
//
// `GET /` request.
func Poller(addr string, mode PollMode) run.Runner {
	return run.Func(func(ctx context.Context) error {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr += ":80"
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("run.Poller invalid addr: %w", err)
		}

		b := exponentialBackoff{max: pollMax, next: pollInitial}
		var pollErr error
		for {
			// if NOT an error we're good to go
			if pollErr = pokeHTTP(ctx, addr, host); pollErr == nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("run.Poller cancelled waiting for poll target to be ready: last err: %w", pollErr)
			case <-time.After(b.Backoff()):
			}
		}
	})
}

var (
	badGatewayPrefix     = []byte("502 Bad Gateway ")
	gatewayTimeoutPrefix = []byte("504 Gateway Timeout ")
)

func pokeHTTP(_ context.Context, addr, host string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(pokeTimeout))
	if _, err := fmt.Fprintf(conn, "OPTIONS * HTTP/1.1\r\nHost: %s\r\n\r\n", host); err != nil {
		return fmt.Errorf("failed to write OPTIONS request: %w", err)
	}

	var buf [32]byte
	if _, err := io.ReadAtLeast(conn, buf[:], max(len(badGatewayPrefix), len(gatewayTimeoutPrefix))); err != nil {
		return fmt.Errorf("failed to read OPTIONS response: %w", err)
	}
	if bytes.HasPrefix(buf[:], badGatewayPrefix) {
		return fmt.Errorf("target unavailable: %s", badGatewayPrefix[:len(badGatewayPrefix)-1])
	}
	if bytes.HasPrefix(buf[:], gatewayTimeoutPrefix) {
		return fmt.Errorf("target unavailable: %s", gatewayTimeoutPrefix[:len(gatewayTimeoutPrefix)-1])
	}
	return nil
}

type exponentialBackoff struct {
	max  time.Duration
	next time.Duration
}

func (b *exponentialBackoff) Backoff() time.Duration {
	curr := b.next
	b.next = min(2*b.next, b.max)
	return curr
}
