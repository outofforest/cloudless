package cloudless

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/digitalocean/go-libvirt"
	"github.com/digitalocean/go-libvirt/socket"
	"github.com/digitalocean/go-libvirt/socket/dialers"
	"github.com/pkg/errors"

	"github.com/outofforest/cloudless/pkg/dev"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/virt"
	"github.com/outofforest/parallel"
)

// Start starts dev environment.
func Start(libvirtAddr string, sources ...dev.SpecSource) error {
	l, err := libvirtConn(libvirtAddr)
	if err != nil {
		return errors.WithStack(err)
	}

	for _, s := range sources {
		if err := s(l); err != nil {
			return err
		}
	}

	return nil
}

// Stop stops dev environment.
func Stop(ctx context.Context, libvirtAddr string) error {
	l, err := libvirtConn(libvirtAddr)
	if err != nil {
		return errors.WithStack(err)
	}

	if err := virt.DestroyVMs(ctx, l, virt.StopDev); err != nil {
		return err
	}
	return virt.DestroyNetworks(ctx, l, virt.StopDev)
}

// Destroy destroys dev environment.
func Destroy(ctx context.Context, libvirtAddr string) error {
	l, err := libvirtConn(libvirtAddr)
	if err != nil {
		return errors.WithStack(err)
	}

	if err := virt.DestroyVMs(ctx, l, virt.StopDev); err != nil {
		return err
	}
	if err := virt.DestroyStoragePool(l); err != nil {
		return err
	}
	return virt.DestroyNetworks(ctx, l, virt.StopDev)
}

// DummyService is used to keep box running without any service.
func DummyService() host.Configurator {
	return Service("dummy", parallel.Exit, func(ctx context.Context) error {
		<-ctx.Done()
		return errors.WithStack(ctx.Err())
	})
}

func libvirtConn(addr string) (*libvirt.Libvirt, error) {
	var dialer socket.Dialer = dialers.NewLocal()
	if addr != "" {
		addrParts := strings.SplitN(addr, "://", 2)
		if len(addrParts) != 2 {
			return nil, errors.Errorf("address %s has invalid format", addr)
		}

		conn, err := net.DialTimeout(addrParts[0], addrParts[1], 2*time.Second)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		dialer = dialers.NewAlreadyConnected(conn)
	}
	l := libvirt.NewWithDialer(dialer)
	if err := l.Connect(); err != nil {
		return nil, errors.WithStack(err)
	}
	return l, nil
}
