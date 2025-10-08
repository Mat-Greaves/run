package ports

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"sync/atomic"
	"time"
)

const (
	portBase  = 0x2000
	portCount = 0x8000 - portBase
)

var (
	lastPortOffset atomic.Uint64

	// a random number guaranteed to be 1 or prime
	portStep = func() uint64 {
	outer:
		for {
			n := rand.Uint64()%portCount | 1
			for i := uint64(2); i <= n/2; i++ {
				if n%i == 0 {
					continue outer
				}
			}
			return n
		}
	}()
)

func localhost(port int) string { return fmt.Sprintf("127.0.0.1:%d", port) }

// Random returns a random port number minimising the chance of overlap between parallel uses.
func Random(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	for range portCount {
		port := portBase + int(lastPortOffset.Add(portStep)%portCount)
		d := net.Dialer{
			Timeout: 10 * time.Millisecond,
		}
		conn, err := d.DialContext(ctx, "tcp", localhost(port))
		if err != nil {
			if err == context.Canceled {
				return "", err
			}
			return localhost(port), nil
		}
		conn.Close()
	}
	return "", errors.New("no ports available")
}
