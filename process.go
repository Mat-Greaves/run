package run

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"syscall"
)

var _ Runner = Process{}

// Process runs an extermal
type Process struct {
	Name string
	// path to executable
	Path string
	// working directory
	Dir string
	// command-line args
	Args []string
	// environment variables
	Env map[string]string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// inherit environment variables from os.Env.
	// Env takes priority over os variables.
	InheritOSEnv bool
	// A list of environment variables to exclude when InheritOSEnv is true.
	DoNotInherit []string
}

// Run implements [Runner] starting the external process.
//
// The process can be shut down by cancelling ctx. In this case the process and all child processes
//
//	will receive a SIGINT.
//
// If this program or p do not terminate gracefully then a SIGKILL will be sent to the process group.
func (p Process) Run(ctx context.Context) error {
	var err error
	p.Path, err = exec.LookPath(p.Path)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, p.Path, p.Args...)
	cmd.Dir = p.Dir
	cmd.Stdin = p.Stdin
	cmd.Stdout = p.Stdout
	cmd.Stderr = p.Stderr

	// Give the external process its own group to more easily clean up it and all of its children.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
		return nil
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// TODO: Handle making sure things are really dead even if this program doesn't exit gracefully

	return cmd.Wait()
}

func Command(cmd string, args ...string) Process {
	return Process{
		Name: filepath.Base(cmd),
		Path: cmd,
		Args: args,
	}
}
