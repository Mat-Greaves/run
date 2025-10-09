package exp_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/matgreaves/run/exp"
	"github.com/matgreaves/run/exp/ports"
	"github.com/matryer/is"
)

func TestHTTPServer(t *testing.T) {
	t.Parallel()
	is := is.New(t)
	addr, err := ports.Random(t.Context())
	is.NoErr(err)

	err, stop := exp.StartHTTPServer(t.Context(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, r.Body)
	}), addr)
	is.NoErr(err)
	defer noErr(t, stop)

	res, err := http.DefaultClient.Post("http://"+addr, "text/plain", strings.NewReader("Hello, World!"))
	is.NoErr(err)
	defer res.Body.Close()
	text, err := io.ReadAll(res.Body)
	is.NoErr(err)
	is.Equal(string(text), "Hello, World!")
}

func noErr(t *testing.T, f func() error) {
	if err := f(); err != nil {
		t.Error(err)
	}
}
