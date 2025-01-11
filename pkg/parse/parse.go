package parse

import (
	"net"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/samber/lo"
)

// MAC parses MAC address.
func MAC(mac string) net.HardwareAddr {
	return lo.Must(net.ParseMAC(mac))
}

// IP4 parses IPv4 address.
func IP4(ip string) net.IP {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		panic(errors.New("invalid IP address"))
	}
	parsedIP = parsedIP.To4()
	if parsedIP == nil {
		panic(errors.New("not an IPNet4 address"))
	}

	return parsedIP
}

// IP6 parses IPv6 address.
func IP6(ip string) net.IP {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		panic(errors.New("invalid IP address"))
	}

	return parsedIP
}

// IPNet4 parses IPv4 address and prefix.
func IPNet4(ip string) net.IPNet {
	parts := strings.Split(ip, "/")
	if len(parts) != 2 {
		panic(errors.New("invalid IP address"))
	}

	maskBits, err := strconv.Atoi(parts[1])
	if err != nil {
		panic(err)
	}
	if maskBits < 0 || maskBits > 32 {
		panic(errors.New("invalid IP address"))
	}

	return net.IPNet{
		IP:   IP4(parts[0]),
		Mask: net.CIDRMask(maskBits, 32),
	}
}

// IPNet6 parses IPv6 address and prefix.
func IPNet6(ip string) net.IPNet {
	parts := strings.Split(ip, "/")
	if len(parts) != 2 {
		panic(errors.New("invalid IP address"))
	}

	maskBits, err := strconv.Atoi(parts[1])
	if err != nil {
		panic(err)
	}
	if maskBits < 0 || maskBits > 128 {
		panic(errors.New("invalid IP address"))
	}

	return net.IPNet{
		IP:   IP6(parts[0]),
		Mask: net.CIDRMask(maskBits, 128),
	}
}