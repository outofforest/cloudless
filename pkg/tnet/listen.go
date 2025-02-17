package tnet

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/pkg/errors"
)

var lc = net.ListenConfig{
	KeepAlive: 3 * time.Minute,
}

// Network represents network.
type Network string

const (
	// NetworkTCP represents tcp network.
	NetworkTCP Network = "tcp"

	// NetworkUnix represents unix network.
	NetworkUnix Network = "unix"
)

// Listen installs a listener on the specified address.
//
// If the address string starts with "tcp:", the rest is interpreted as
// [address]:port on which to open a TCP listening socket. TCP keep-alive is
// enabled in this case.
//
// If the address string starts with "unix:", the rest is interpreted the path
// to a UNIX domain socket to listen on.
//
// If neither prefix is present, "tcp:" is assumed.
func Listen(ctx context.Context, address string) (net.Listener, error) {
	var proto string
	switch {
	case strings.HasPrefix(address, "unix:"):
		proto = "unix"
	default:
		proto = "tcp"
	}

	l, err := lc.Listen(ctx, proto, strings.TrimPrefix(address, proto+":"))
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return l, nil
}

// ListenOnRandomPort selects a random local port using specified network type and installs a listener on.
func ListenOnRandomPort(ctx context.Context, network Network) (net.Listener, error) {
	address := "localhost:"
	if network != "" {
		address = string(network) + ":" + address
	}
	return Listen(ctx, address)
}
