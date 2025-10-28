package run_test

import (
	"bytes"
	"testing"

	"github.com/matgreaves/run"
	"github.com/matryer/is"
)

func TestCommand(t *testing.T) {
	is := is.New(t)
	p := run.Command("echo", "Hello, World!")
	buf := &bytes.Buffer{}
	p.Stdout = buf
	err := p.Run(t.Context())
	is.NoErr(err)
	is.Equal(buf.String(), "Hello, World!\n")
}
