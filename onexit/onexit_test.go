package onexit

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matryer/is"
)

func TestOnExit(t *testing.T) {
	t.Parallel()

	t.Run("with cancel", func(t *testing.T) {
		t.Parallel()

		is := is.New(t)

		k, err := NewKiller(io.Discard, "")
		is.NoErr(err)
		fn := filepath.Join(t.TempDir(), "johncena.log")

		cancel, err := k.OnExit(fmt.Sprintf("echo -n 'Hello, World!' > %s", fn))
		is.NoErr(err)
		// cancel the exit command
		cancel()
		is.NoErr(k.Close())

		// short sleep to let the exit script run in the background
		time.Sleep(10 * time.Millisecond)
		_, err = os.Stat(fn)
		is.True(errors.Is(err, os.ErrNotExist))
	})

	t.Run("no cancel", func(t *testing.T) {

		is := is.New(t)

		k, err := NewKiller(io.Discard, "")
		is.NoErr(err)
		f, err := os.CreateTemp(t.TempDir(), "test.log")
		fn := f.Name()
		is.NoErr(f.Close())

		_, err = k.OnExit(fmt.Sprintf("echo -n 'Hello, World!' > %s", fn))
		is.NoErr(err)
		is.NoErr(k.Close())

		// short sleep to let the exit script run in the background
		time.Sleep(10 * time.Millisecond)
		contents, err := os.ReadFile(fn)
		is.NoErr(err)

		is.Equal(contents, []byte("Hello, World!"))
	})
}
