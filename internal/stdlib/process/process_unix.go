//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func newCommand(ctx context.Context, program string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

func processSignalName(err *exec.ExitError) any {
	status, ok := err.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return nil
	}
	return signalName(status.Signal())
}

func signalName(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGHUP:
		return "SIGHUP"
	case syscall.SIGQUIT:
		return "SIGQUIT"
	case syscall.SIGUSR1:
		return "SIGUSR1"
	case syscall.SIGUSR2:
		return "SIGUSR2"
	case syscall.SIGWINCH:
		return "SIGWINCH"
	case syscall.SIGPIPE:
		return "SIGPIPE"
	case syscall.SIGALRM:
		return "SIGALRM"
	case syscall.SIGKILL:
		return "SIGKILL"
	default:
		return "unknown"
	}
}

func signalExitCode(name string) int {
	switch name {
	case "SIGINT":
		return int(syscall.SIGINT)
	case "SIGTERM":
		return int(syscall.SIGTERM)
	case "SIGHUP":
		return int(syscall.SIGHUP)
	case "SIGQUIT":
		return int(syscall.SIGQUIT)
	case "SIGUSR1":
		return int(syscall.SIGUSR1)
	case "SIGUSR2":
		return int(syscall.SIGUSR2)
	case "SIGWINCH":
		return int(syscall.SIGWINCH)
	case "SIGPIPE":
		return int(syscall.SIGPIPE)
	case "SIGALRM":
		return int(syscall.SIGALRM)
	case "SIGKILL":
		return int(syscall.SIGKILL)
	default:
		return 0
	}
}

func signalValue(name string) (os.Signal, bool, error) {
	switch name {
	case "SIGINT":
		return syscall.SIGINT, true, nil
	case "SIGTERM":
		return syscall.SIGTERM, true, nil
	case "SIGHUP":
		return syscall.SIGHUP, true, nil
	case "SIGQUIT":
		return syscall.SIGQUIT, true, nil
	case "SIGUSR1":
		return syscall.SIGUSR1, true, nil
	case "SIGUSR2":
		return syscall.SIGUSR2, true, nil
	case "SIGWINCH":
		return syscall.SIGWINCH, true, nil
	case "SIGPIPE":
		return syscall.SIGPIPE, true, nil
	case "SIGALRM":
		return syscall.SIGALRM, true, nil
	case "SIGBREAK":
		return nil, false, nil
	default:
		return nil, false, errors.New("process.onSignal: unsupported signal name")
	}
}

func defaultShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "/bin/sh"
}

func timeoutExitCode() int { return 128 + int(syscall.SIGKILL) }

func timeoutSignalName() string { return "SIGKILL" }
