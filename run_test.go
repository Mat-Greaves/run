package run_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Mat-Greaves/run"
	"github.com/matryer/is"
)

var innerErr = errors.New("inner")

func TestFunc(t *testing.T) {
	t.Parallel()
	var touched bool
	ctx := context.WithValue(context.Background(), "touched", &touched)
	f := run.Func(func(ctx context.Context) error {
		to := ctx.Value("touched").(*bool)
		*to = true
		return innerErr
	})
	err := f(ctx)
	if touched != true {
		t.Fatal("touched not updated")
	}
	if !errors.Is(err, innerErr) {
		t.Fatalf("err tree does not contain innerErr")
	}
}

func ExampleFunc() {
	f := run.Func(func(ctx context.Context) error {
		fmt.Println("Hello, World!")
		return nil
	})
	_ = f.Run(context.Background())
	// Output: Hello, World!
}

func TestSequence(t *testing.T) {
	t.Parallel()
	t.Run("all succeed", func(t *testing.T) {
		t.Parallel()
		var count int
		ctx := context.WithValue(context.Background(), "count", &count)
		addOne := run.Func(func(ctx context.Context) error {
			c := ctx.Value("count").(*int)
			*c++
			return nil
		})
		s := run.Sequence{
			addOne,
			addOne,
		}
		err := s.Run(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Fatalf("got count %d want 2", count)
		}
	})

	t.Run("stop on failure", func(t *testing.T) {
		t.Parallel()
		var count int
		ctx := context.WithValue(context.Background(), "count", &count)
		addOne := run.Func(func(ctx context.Context) error {
			c := ctx.Value("count").(*int)
			*c++
			return nil
		})
		s := run.Sequence{
			addOne,
			run.Func(func(_ context.Context) error { return innerErr }),
			addOne,
		}
		err := s.Run(ctx)
		if !errors.Is(err, innerErr) {
			t.Fatalf("innerErr not part of errs tree: %s", err)
		}
		if count != 1 {
			t.Fatalf("got count %d want 1", count)
		}
	})
}

func ExampleSequence() {
	r := run.Sequence{
		run.Func(func(ctx context.Context) error {
			fmt.Print("one")
			return nil
		}),
		run.Func(func(ctx context.Context) error {
			fmt.Print(" two")
			return nil
		}),
	}
	_ = r.Run(context.Background())
	// Output: one two
}

func TestGroup(t *testing.T) {
	t.Parallel()
	t.Run("run until cancelled", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			g := run.Group{
				"foo": run.Func(func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				}),
			}
			err := g.Run(ctx)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected err to be context.DeadlineExceeded got: %v", err)
			}
		})
	})

	t.Run("cancel on failure", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			g := run.Group{
				"foo": run.Func(func(ctx context.Context) error {
					<-time.After(1 * time.Second)
					return innerErr
				}),
				"bar": run.Func(func(ctx context.Context) error {
					<-ctx.Done()
					if ctx.Err() != context.Canceled {
						t.Error("bar expected context canceled got", ctx.Err())
					}
					return nil
				}),
			}
			err := g.Run(t.Context())
			if !errors.Is(err, innerErr) {
				t.Error("expected innerErr got", err)
			}
		})
	})

	t.Run("cancel on early return", func(t *testing.T) {
		g := run.Group{
			"foo": run.Func(func(ctx context.Context) error {
				<-time.After(1 * time.Second)
				return nil
			}),
			"bar": run.Func(func(ctx context.Context) error {
				<-ctx.Done()
				if ctx.Err() != context.Canceled {
					t.Error("bar expected context canceled got", ctx.Err())
				}
				return nil
			}),
		}
		err := g.Run(t.Context())
		if !errors.Is(err, run.ErrExited) {
			t.Error("expected ErrExited got", err)
		}
	})

	t.Run("timeout waiting for runner cancel", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			wait := make(chan struct{})
			g := run.Group{
				"foo": run.Func(func(ctx context.Context) error {
					<-time.After(1 * time.Second)
					return innerErr
				}),
				"bar": run.Func(func(ctx context.Context) error {
					<-wait
					return nil
				}),
			}
			err := g.Run(t.Context())
			if !errors.Is(err, innerErr) && !errors.Is(err, run.ErrTimeout) {
				t.Error("expected innerErr and run.ErrTimeout got", err)
			}
			if !strings.Contains(err.Error(), "[bar]: one or more runners did not exit in time") {
				t.Errorf("error did not contain expected substring: %s", err.Error())
			}
			wait <- struct{}{}
		})
	})

	t.Run("catch panic", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			g := run.Group{
				"foo": run.Func(func(ctx context.Context) error {
					panic(innerErr)
				}),
			}
			err := g.Run(t.Context())
			if !errors.Is(err, innerErr) {
				t.Error("expected innerErr got", err)
			}
		})
	})

	t.Run("don't cancel on early return", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			g := run.Group{
				"foo": run.Func(func(ctx context.Context) error {
					return nil
				}),
				"bar": run.Func(func(ctx context.Context) error {
					<-time.After(2 * time.Second)
					return innerErr
				}),
			}.WithoutCancel()
			err := g.Run(t.Context())
			if !errors.Is(err, innerErr) {
				t.Error("expected innerErr got", err)
			}
		})
	})
}

func TestOnce(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		var count int
		r := run.Once(run.Func(func(_ context.Context) error {
			count++
			return nil
		}))
		r.Run(t.Context())
		r.Run(t.Context())
		is.New(t).Equal(count, 1)
	})

	t.Run("fail", func(t *testing.T) {
		t.Parallel()
		var count int
		// fails on first call both calls should return the same error
		r := run.Once(run.Func(func(_ context.Context) error {
			count++
			if count == 1 {
				return innerErr
			}
			return nil
		}))
		is.New(t).True(errors.Is(r.Run(t.Context()), innerErr))
		is.New(t).True(errors.Is(r.Run(t.Context()), innerErr))
	})
}

func ExampleOnce() {
	r := run.Once(run.Func(func(_ context.Context) error {
		fmt.Println("Hello, World!")
		return nil
	}))
	r.Run(context.Background())
	r.Run(context.Background())
	// Output: Hello, World!
}

func TestGo(t *testing.T) {
	t.Parallel()

	t.Run("runs", func(t *testing.T) {
		t.Parallel()
		var count int
		res := make(chan error)
		run.Go(t.Context(), run.Func(func(ctx context.Context) error {
			count++
			return nil
		}), res)
		err := <-res
		if err != nil {
			t.Fatal("expected nil got", err)
		}
		if count != 1 {
			t.Error("expected 1 got", count)
		}
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		res := make(chan error)
		run.Go(t.Context(), run.Func(func(ctx context.Context) error {
			return innerErr
		}), res)
		err := <-res
		if !errors.Is(err, innerErr) {
			t.Error("expected innerErr got", err)
		}
	})

	t.Run("panics error", func(t *testing.T) {
		t.Parallel()
		res := make(chan error)
		run.Go(t.Context(), run.Func(func(ctx context.Context) error {
			panic(innerErr)
		}), res)
		err := <-res
		if !errors.Is(err, innerErr) {
			t.Error("expected innerErr got", err)
		}
	})

	t.Run("panics value", func(t *testing.T) {
		t.Parallel()
		res := make(chan error)
		run.Go(t.Context(), run.Func(func(ctx context.Context) error {
			panic("eek")
		}), res)
		err := <-res
		if !strings.Contains(err.Error(), "eek") {
			t.Error("expected err to contain 'eek' got", err.Error())
		}
	})
}

func TestIdle(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()
		err := run.Idle.Run(ctx)
		if err != nil {
			t.Error("expected nil, got", err)
		}
	})
}
