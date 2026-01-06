package virt

import (
	"context"
	"strings"
	"time"

	"github.com/digitalocean/go-libvirt"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

// DecideFunc defines function which decides if resource should be stopped.
type DecideFunc func(xml string) bool

// StopDev is the decision function stopping resources created by cloudless.
func StopDev(xml string) bool {
	return strings.Contains(xml, "<cloudless:cloudless")
}

// StopVMs stops vms.
func StopVMs(ctx context.Context, l *libvirt.Libvirt, decideFun DecideFunc) error {
	domains, _, err := l.ConnectListAllDomains(1,
		libvirt.ConnectListDomainsActive|libvirt.ConnectListDomainsInactive)
	if err != nil {
		return errors.WithStack(err)
	}

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for _, d := range domains {
			if decideFun != nil {
				xml, err := l.DomainGetXMLDesc(d, 0)
				if err != nil {
					return errors.WithStack(err)
				}
				if !decideFun(xml) {
					continue
				}
			}

			spawn("stopVM", parallel.Continue, func(ctx context.Context) error {
				log := logger.Get(ctx)

				for trial := 0; ; trial++ {
					active, err := l.DomainIsActive(d)
					if err != nil {
						if libvirt.IsNotFound(err) {
							return nil
						}
						return errors.WithStack(err)
					}

					if active == 0 {
						err := l.DomainUndefineFlags(d, libvirt.DomainUndefineManagedSave|
							libvirt.DomainUndefineSnapshotsMetadata|libvirt.DomainUndefineNvram|
							libvirt.DomainUndefineCheckpointsMetadata)
						if err == nil || libvirt.IsNotFound(err) {
							return nil
						}
						return errors.WithStack(err)
					}

					err = l.DomainShutdown(d)
					switch {
					case err == nil:
						if trial%10 == 0 {
							log.Info("VM is still running", zap.String("vm", d.Name))
						}
						<-time.After(time.Second)
					case libvirt.IsNotFound(err):
						return nil
					default:
						return errors.WithStack(err)
					}
				}
			})
		}

		return nil
	})
}

// StopNetworks stops networks.
func StopNetworks(ctx context.Context, l *libvirt.Libvirt, decideFun DecideFunc) error {
	networks, _, err := l.ConnectListAllNetworks(1,
		libvirt.ConnectListNetworksActive|libvirt.ConnectListNetworksInactive)
	if err != nil {
		return errors.WithStack(err)
	}

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for _, n := range networks {
			if decideFun != nil {
				xml, err := l.NetworkGetXMLDesc(n, 0)
				if err != nil {
					return errors.WithStack(err)
				}
				if !decideFun(xml) {
					continue
				}
			}

			spawn("stopNetwork", parallel.Continue, func(ctx context.Context) error {
				log := logger.Get(ctx)

				for trial := 0; ; trial++ {
					active, err := l.NetworkIsActive(n)
					if err != nil {
						if libvirt.IsNotFound(err) {
							return nil
						}
						return errors.WithStack(err)
					}

					if active == 0 {
						err := l.NetworkUndefine(n)
						if err == nil || libvirt.IsNotFound(err) {
							return nil
						}
						return errors.WithStack(err)
					}

					err = l.NetworkDestroy(n)
					switch {
					case err == nil:
						if trial%10 == 0 {
							log.Info("Network is still running", zap.String("network", n.Name))
						}
						<-time.After(time.Second)
					case libvirt.IsNotFound(err):
						return nil
					default:
						return errors.WithStack(err)
					}
				}
			})
		}

		return nil
	})
}
