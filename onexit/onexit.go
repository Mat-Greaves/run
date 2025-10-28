// onexit is a package for robustly running deferred logic even if the go program exits
// forcefully without waiting for deferred logic to run.
//
// Useful for software creators whose users love spamming ctrl+c to exit their tests or CLI's faster.
package onexit

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"al.essio.dev/pkg/shellescape"
)

func init() {
	var err error
	DefaultKiller, err = NewKiller(io.Discard, "/dev/null")
	if err != nil {
		panic(err)
	}
}

// DefaultKiller is the Killer instance used by the global [Kill], [OnExit], and [OnExitF] methods.
//
// By default this is a Killer that writes logs to io.Discard. Replace this with a killer from [NewKiller]
// for more customisable options.
var DefaultKiller *Killer

//go:embed onexit.sh
var onexit string

type operation string

const (
	opExit   operation = "exit"
	opCancel operation = "cancel"
)

// Killer makes sure that commands that are supposed to run to clean up resources
// are executed regardless of how cleanly the calling program ends.
//
// Killer is a insurance policy, the program should still aim to gracefully clean up
// resources cancelling the command registered with Killer when done so.
//
// Killer is particularly useful for external processes that don't close automatically when
// the parent process closes and needing to handle abrupt exits to our program such as SIGKILL
// which interfere with graceful cleanup.
type Killer struct {
	w   *os.File
	seq atomic.Int64
	mu  sync.Mutex
	// filepath to external log file script logs are tee'd to. Defaults to /dev/null
	// but can be useful for debugging.
	extLogs string
}

// Kill kills the process identified by pid with human readable description desc.
//
// If sig is not empty the first sig is sent to pid otherswise syscall.SIGINT is sent.
//
// A cancel function is returned that will cancel this killing if the process has otherwise been gracefully
// terminated.
func (k *Killer) Kill(desc string, pid int, sig ...syscall.Signal) (cancel func() error, _ error) {
	desc = shellescape.Quote(desc)
	signal := syscall.SIGINT
	if len(sig) > 0 {
		signal = sig[0]
	}
	return OnExitF("echo killing %s; kill -%d %d", desc, signal, pid)
}

// Close closes k.
//
// In most circumstances it is not necessary to manually call close and can be depended on
// to close automatically when the program exits.
func (k *Killer) Close() error {
	return k.w.Close()
}

// OnExitF queues the shell command specified by format and args to be run when this process exits.
//
// cancel can be used to cancel the command from running.
func (k *Killer) OnExitF(format string, args ...any) (cancel func() error, _ error) {
	return k.OnExit(fmt.Sprintf(format, args...))
}

// OnExit queues the shell command command to be run when this process exits.
func (k *Killer) OnExit(command string) (cancel func() error, _ error) {
	return k.queue(command)
}

func (k *Killer) queue(command string) (func() error, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	seq := k.seq.Add(1)

	if err := k.send(opExit, command); err != nil {
		return nil, fmt.Errorf("onexit: failed to queue: %w", err)
	}
	return func() error { return k.dequeue(seq) }, nil
}

func (k *Killer) dequeue(seq int64) error {
	return k.send(opCancel, seq)
}

func (k *Killer) send(op operation, data any) error {
	_, err := fmt.Fprintf(k.w, "%s:%v\n", op, data)
	if err != nil {
		return err
	}
	return nil
}

func NewKiller(logWriter io.Writer, logFile string) (*Killer, error) {
	script := onexit
	if logFile != "" {
		script = strings.TrimSuffix(script, "\n")
		script += "2>&1 | tee " + logFile + "\n"
	}

	p := exec.Command("/bin/bash", "-c", script)

	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("onexit: failed to open pipe: %w", err)
	}

	p.Stdin = r
	p.Stdout = logWriter
	p.Stderr = logWriter
	p.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	k := &Killer{w: w}
	k.seq.Store(-1)
	k.mu.Lock()
	go func() {
		_ = p.Start()
		k.mu.Unlock()
	}()
	return k, nil
}

// Kill runs [DefaultKiller.Kill].
func Kill(desc string, pid int, sig ...syscall.Signal) (cancel func() error, _ error) {
	return DefaultKiller.Kill(desc, pid, sig...)
}

// OnExitF runs [DefaultKiller.OnExitF].
func OnExitF(format string, args ...any) (cancel func() error, _ error) {
	return DefaultKiller.OnExit(fmt.Sprintf(format, args...))
}

// OnExit runs [DefaultKiller.OnExit].
func OnExit(command string) (cancel func() error, _ error) {
	return DefaultKiller.OnExit(command)
}
