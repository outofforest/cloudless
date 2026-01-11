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

// StoragePoolName is the name of storage pool.
const StoragePoolName = "cloudless"

// DecideFunc defines function which decides if resource should be stopped.
type DecideFunc func(xml string) bool

// StopDev is the decision function stopping resources created by cloudless.
func StopDev(xml string) bool {
	return strings.Contains(xml, "<cloudless:cloudless")
}

// DestroyVMs destroys vms.
func DestroyVMs(ctx context.Context, l *libvirt.Libvirt, decideFun DecideFunc) error {
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
					if ctx.Err() != nil {
						return errors.WithStack(ctx.Err())
					}

					active, err := l.DomainIsActive(d)
					if err != nil {
						if IsError(err, libvirt.ErrNoDomain) {
							return nil
						}
						return errors.WithStack(err)
					}

					if active == 0 {
						err := l.DomainUndefineFlags(d, libvirt.DomainUndefineManagedSave|
							libvirt.DomainUndefineSnapshotsMetadata|libvirt.DomainUndefineNvram|
							libvirt.DomainUndefineCheckpointsMetadata)
						if err == nil || IsError(err, libvirt.ErrNoDomain) {
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
					case IsError(err, libvirt.ErrNoDomain):
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

// DestroyStoragePool destroys storage pool.
func DestroyStoragePool(l *libvirt.Libvirt) error {
	pool, err := l.StoragePoolLookupByName(StoragePoolName)
	switch {
	case err == nil:
	case IsError(err, libvirt.ErrNoStoragePool):
		return nil
	default:
		return errors.WithStack(err)
	}

	vols, _, err := l.StoragePoolListAllVolumes(pool, 1, 0)
	if err != nil {
		if IsError(err, libvirt.ErrNoStoragePool) {
			return nil
		}
		return errors.WithStack(err)
	}
	for _, v := range vols {
		if err := l.StorageVolDelete(v, libvirt.StorageVolDeleteNormal); err != nil &&
			!IsError(err, libvirt.ErrNoStorageVol) {
			return errors.WithStack(err)
		}
	}

	err = l.StoragePoolDestroy(pool)
	switch {
	case err == nil:
	case IsError(err, libvirt.ErrNoStoragePool):
		return nil
	default:
		return errors.WithStack(err)
	}

	err = l.StoragePoolDelete(pool, libvirt.StoragePoolDeleteNormal)
	if err != nil {
		if IsError(err, libvirt.ErrNoStoragePool) {
			return nil
		}
		return errors.WithStack(err)
	}

	err = l.StoragePoolUndefine(pool)
	if err != nil && !IsError(err, libvirt.ErrNoStoragePool) {
		return errors.WithStack(err)
	}

	return nil
}

// DestroyNetworks destroys networks.
func DestroyNetworks(ctx context.Context, l *libvirt.Libvirt, decideFun DecideFunc) error {
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
					if ctx.Err() != nil {
						return errors.WithStack(ctx.Err())
					}

					active, err := l.NetworkIsActive(n)
					if err != nil {
						if IsError(err, libvirt.ErrNoNetwork) {
							return nil
						}
						return errors.WithStack(err)
					}

					if active == 0 {
						err := l.NetworkUndefine(n)
						if err == nil || IsError(err, libvirt.ErrNoNetwork) {
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
					case IsError(err, libvirt.ErrNoNetwork):
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

// IsError checks if error is a particular libvirt error.
func IsError(err error, expectedError libvirt.ErrorNumber) bool {
	var virtErr libvirt.Error
	if errors.As(err, &virtErr) {
		return virtErr.Code == uint32(expectedError)
	}
	return false
}
