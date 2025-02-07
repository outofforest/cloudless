package vnet

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/samber/lo"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
	"github.com/outofforest/cloudless/pkg/kernel"
	"github.com/outofforest/cloudless/pkg/parse"
)

var (
	//go:embed nat.tmpl.xml
	natDef string

	natDefTmpl = lo.Must(template.New("nat").Parse(natDef))
)

// Config represents network configuration.
type Config struct {
	IP4 net.IPNet
	IP6 net.IPNet
}

// Configurator defines function setting the network configuration.
type Configurator func(n *Config)

// NAT creates NATed network.
func NAT(name, mac string, configurators ...Configurator) host.Configurator {
	var n Config

	hostMAC := parse.MAC(mac)
	ifName := "v" + strings.ToLower(name)
	if len(ifName) > 15 {
		ifName = ifName[:15]
	}

	for _, configurator := range configurators {
		configurator(&n)
	}

	return cloudless.Join(
		cloudless.Firewall(
			firewall.Forward(ifName),
			firewall.Masquerade(ifName),
		),
		cloudless.EnableIPForwarding(),
		cloudless.StartVirtServices(),
		cloudless.KernelModules(kernel.Module{
			Name: "tun",
		}),
		cloudless.Prepare(func(_ context.Context) error {
			netUUID, err := uuid.NewUUID()
			if err != nil {
				return errors.WithStack(err)
			}

			filePath := fmt.Sprintf("/etc/libvirt/qemu/networks/%s.xml", name)
			f, err := os.OpenFile(filePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				return errors.WithStack(err)
			}
			defer f.Close()

			data := struct {
				UUID      uuid.UUID
				Name      string
				IfaceName string
				MAC       net.HardwareAddr
				IP4       net.IP
				IP4Mask   string
				IP6       net.IP
				IP6Prefix int
			}{
				UUID:      netUUID,
				Name:      name,
				IfaceName: ifName,
				MAC:       hostMAC,
				IP4:       n.IP4.IP,
				IP6:       n.IP6.IP,
			}

			if n.IP4.Mask != nil {
				data.IP4Mask = fmt.Sprintf("%d.%d.%d.%d", n.IP4.Mask[0], n.IP4.Mask[1], n.IP4.Mask[2],
					n.IP4.Mask[3])
			}
			if n.IP6.Mask != nil {
				data.IP6Prefix, _ = n.IP6.Mask.Size()
			}

			if err := natDefTmpl.Execute(f, data); err != nil {
				return errors.WithStack(err)
			}

			return errors.WithStack(os.Link(filePath,
				filepath.Join(filepath.Dir(filePath), "autostart", filepath.Base(filePath))))
		}),
	)
}

// IP4 configures network's IPv4 address on the host.
func IP4(ip string) Configurator {
	return func(n *Config) {
		n.IP4 = parse.IPNet4(ip)
	}
}

// IP6 configures network's IPv6 address on the host.
func IP6(ip string) Configurator {
	return func(n *Config) {
		n.IP6 = parse.IPNet6(ip)
	}
}
