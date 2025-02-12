package tws

import (
	"net"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

func setTCPOption(conn net.Conn, option, value int) error {
	raw, err := conn.(*net.TCPConn).SyscallConn()
	if err != nil {
		return errors.Wrapf(err, "failed to set TCP socket option %d", option)
	}

	err = raw.Control(func(fd uintptr) {
		err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, option, value)
	})
	if err != nil {
		return errors.Wrapf(err, "failed to set TCP socket option %d", option)
	}

	return nil
}

// TuneTCP configures ACk timeouts on TCP level.
func TuneTCP(conn net.Conn, config Config) error {
	if config.TCPTimeout != 0 {
		if err := setTCPOption(conn, unix.TCP_USER_TIMEOUT, int(config.TCPTimeout/time.Millisecond)); err != nil {
			return errors.Wrap(err, "failed to tune TCP socket")
		}
	}

	return nil
}
