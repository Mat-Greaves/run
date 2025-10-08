package run

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	ShutdownTimeout = 10 * time.Second
)

var (
	ErrExited  = errors.New("runner exited early")
	ErrTimeout = errors.New("one or more runners did not exit in time")
)

type Runner interface {
	Run(context.Context) error
}

var _ Runner = Func(func(_ context.Context) error { return nil })

// Func is a [Runner] for a Go function literal.
type Func (func(context.Context) error)

// Run implements [Runner]
func (f Func) Run(ctx context.Context) error {
	return f(ctx)
}

var _ Runner = Sequence{}

// Sequence executes a group of [Runner] sequentially.
type Sequence []Runner

// Run implements [Runner]
func (s Sequence) Run(ctx context.Context) error {
	for i, r := range s {
		err := r.Run(ctx)
		if err != nil {
			return fmt.Errorf("sequence [%d:%d]: %w", i, len(s)-1, err)
		}
	}
	return nil
}

var _ Runner = Group{}
var _ Runner = Group{}.WithoutCancel()

// Group executes a group of [Runner] in parallel returning the reason the first member exits.
//
// If a runner exits with error == nil then [ErrExited] will be returned.
//
// Runners have until [ShutdownTimeout] to exit or the group will exit in a [ErrTimeout] wrapping
// the original cause for the shutdown.
//
// Group will catch panics within members and propagate them as errors instead gracefully terminating
// other members.
type Group map[string]Runner

func (g Group) Run(ctx context.Context) error {
	return g.run(ctx, true)
}

// WithoutCancel returns a group that doesn't cancel other runners if a runner exits with a nil error.
func (g Group) WithoutCancel() Runner {
	return Func(func(ctx context.Context) error {
		return g.run(ctx, false)
	})
}

func (g Group) run(ctx context.Context, cancelOnExit bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type groupErr struct {
		runner string
		err    error
	}
	// channel has to be buffered or goroutines might leak when they cannot write
	// after we've given up on waiting for them
	errs := make(chan groupErr, len(g)+1)

	for name, r := range g {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					var err error
					if _, is := r.(error); is {
						err = fmt.Errorf("run.Group recover: %w", r.(error))
					} else {
						err = fmt.Errorf("run.Group recover: %v", r)
					}
					errs <- groupErr{runner: name, err: err}
				}
			}()
			err := r.Run(ctx)
			errs <- groupErr{
				runner: name,
				err:    err,
			}
		}()
	}

	exited := map[string]bool{}
	for k := range g {
		exited[k] = false
	}

	var cause error
	var exitTimeout <-chan time.Time
	for range g {
		select {
		case gerr := <-errs:
			exited[gerr.runner] = true

			if cause == nil && gerr.err != nil {
				cause = fmt.Errorf("run.Group[%s]: %w", gerr.runner, gerr.err)
			}
			if gerr.err == nil && cancelOnExit && cause == nil {
				cause = fmt.Errorf("run.Group[%s]: %w", gerr.runner, ErrExited)
			}
			if cause != nil {
				cancel()
				exitTimeout = time.After(ShutdownTimeout)
			}
		case <-exitTimeout:
			running := []string{}
			for name, done := range exited {
				if !done {
					running = append(running, name)
				}
			}
			return fmt.Errorf("%s: %w: shutdown cause: %w", running, ErrTimeout, cause)
		}
	}

	return cause
}

// Once returns a [Runner] that only executes r the first time [Run] is called.
//
// Successive calls will return the same result.
func Once(r Runner) Runner {
	once := sync.Once{}
	var err error
	return Func(func(ctx context.Context) error {
		once.Do(func() {
			err = r.Run(ctx)
		})
		return err
	})
}

// Execute r in a Goroutine pushing the return value into res.
//
// Recovers panics from r returning them as an error instead. If r panics with a
// error the returned error will wrap that error.
func Go(ctx context.Context, r Runner, res chan<- error) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if err, is := r.(error); is {
					res <- fmt.Errorf("run.Go recover: %w", err)
					return
				}
				res <- fmt.Errorf("run.Go recover: %v", r)
			}
		}()
		res <- r.Run(ctx)
	}()
}

// Idle is a [Runner] that does nothing waiting for ctx to be cancelled.
//
// Idle can be useful as the last member of a [Sequence] if you don't want the Sequence
// to return causing sibling runners to be cancelled, such as within a [Group].
var Idle = Func(func(ctx context.Context) error {
	<-ctx.Done()
	return nil
})
