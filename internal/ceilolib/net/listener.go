//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd

package net

import (
	"context"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func listenTCP(address string, reusePort bool) (net.Listener, error) {
	if !reusePort {
		return net.Listen("tcp", address)
	}

	config := net.ListenConfig{
		Control: func(_, _ string, raw syscall.RawConn) error {
			var socketErr error
			if err := raw.Control(func(fd uintptr) {
				socketErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			}); err != nil {
				return err
			}
			return socketErr
		},
	}
	listener, err := config.Listen(context.Background(), "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("enable reuseport: %w", err)
	}
	return listener, nil
}
