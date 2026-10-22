//go:build windows

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
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := exec.Command("taskkill", "/T", "/F", "/PID", itoa(cmd.Process.Pid)).Run()
		if err != nil && cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

func processSignalName(*exec.ExitError) any { return nil }

func signalExitCode(string) int { return 0 }

func signalValue(name string) (os.Signal, bool, error) {
	switch name {
	case "SIGINT":
		return os.Interrupt, true, nil
	case "SIGTERM":
		return syscall.SIGTERM, true, nil
	case "SIGBREAK", "SIGHUP", "SIGQUIT", "SIGUSR1", "SIGUSR2", "SIGWINCH", "SIGPIPE", "SIGALRM":
		return nil, false, nil
	default:
		return nil, false, errors.New("process.onSignal: unsupported signal name")
	}
}

func defaultShell() string {
	if shell := os.Getenv("COMSPEC"); shell != "" {
		return shell
	}
	return "cmd.exe"
}

func timeoutExitCode() int { return 1 }

func timeoutSignalName() string { return "SIGKILL" }

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
