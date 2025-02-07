package tnet

import (
	"net"
	"strconv"
)

// Join joins host and port.
func Join(host string, port uint16) string {
	return net.JoinHostPort(host, strconv.Itoa(int(port)))
}

// JoinScheme joins scheme, hsot and port.
func JoinScheme(scheme, host string, port uint16) string {
	return scheme + "://" + Join(host, port)
}
