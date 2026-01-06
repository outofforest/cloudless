package vnet

import (
	"bytes"
	_ "embed"
	"fmt"
	"math"
	"net"
	"text/template"

	"github.com/digitalocean/go-libvirt"
	"github.com/google/nftables/binaryutil"
	"github.com/pkg/errors"
	"github.com/samber/lo"

	"github.com/outofforest/cloudless/pkg/dev"
	"github.com/outofforest/cloudless/pkg/parse"
)

var (
	//go:embed vnet.tmpl.xml
	vnetDef string

	vnetDefTmpl = lo.Must(template.New("vnet").Parse(vnetDef))
)

// Config represents vnet configuration.
type Config struct {
	ForwardMode string
	Name        string
	MAC         net.HardwareAddr
	IP4         net.IP
	IP4Mask     string
	IP6         net.IP
	IP6Prefix   uint8
}

// Configurator defines function setting the vnet configuration.
type Configurator func(vnet *Config)

// NAT defines dev spec of NAT network.
func NAT(name, mac string, configurators ...Configurator) dev.SpecSource {
	return spec("nat", name, mac, configurators)
}

// Isolated defines dev spec of isolated network.
func Isolated(name, mac string, configurators ...Configurator) dev.SpecSource {
	return spec("", name, mac, configurators)
}

func spec(forwardMode, name, mac string, configurators []Configurator) dev.SpecSource {
	vnet := Config{
		ForwardMode: forwardMode,
		Name:        name,
		MAC:         parse.MAC(mac),
	}

	for _, configurator := range configurators {
		configurator(&vnet)
	}

	return func(l *libvirt.Libvirt) error {
		buf := &bytes.Buffer{}
		if err := vnetDefTmpl.Execute(buf, vnet); err != nil {
			return errors.WithStack(err)
		}

		vnet, err := l.NetworkDefineXML(buf.String())
		if err != nil {
			return errors.WithStack(err)
		}
		return errors.WithStack(l.NetworkCreate(vnet))
	}
}

// IPs configures the IP addresses for host interface.
func IPs(ips ...string) Configurator {
	return func(vnet *Config) {
		for _, ip := range ips {
			parsedIP := parse.IPNet(ip)
			ones, bits := parsedIP.Mask.Size()
			if len(parsedIP.IP) == net.IPv4len {
				maskBytes := binaryutil.BigEndian.PutUint32(math.MaxUint32 << (bits - ones))
				vnet.IP4 = parsedIP.IP
				vnet.IP4Mask = fmt.Sprintf("%d.%d.%d.%d", maskBytes[0], maskBytes[1], maskBytes[2], maskBytes[3])
			} else {
				vnet.IP6 = parsedIP.IP
				vnet.IP6Prefix = uint8(ones)
			}
		}
	}
}
