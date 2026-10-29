//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package net

import (
	"errors"
	"net"
)

func listenTCP(address string, reusePort bool) (net.Listener, error) {
	if reusePort {
		return nil, errors.New("reuseport is not supported on this platform")
	}
	return net.Listen("tcp", address)
}
