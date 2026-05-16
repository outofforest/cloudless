package acpi

import (
	"context"
	"strings"

	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/parallel"
)

const (
	// See https://github.com/torvalds/linux/blob/master/drivers/acpi/event.c
	acpiGenlFamilyName     = "acpi_event"
	acpiGenlMcastGroupName = "acpi_mc_group"
)

// PowerService creates new ACPI service for powering off and rebooting the host.
func PowerService() host.Configurator {
	return cloudless.Service("acpi-power", func(ctx context.Context) error {
		conn, err := genetlink.Dial(nil)
		if err != nil {
			return errors.WithStack(err)
		}
		defer conn.Close()

		if err := subscribe(conn); err != nil {
			return err
		}

		return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
			spawn("watchdog", parallel.Fail, func(ctx context.Context) error {
				<-ctx.Done()
				_ = conn.Close()
				return errors.WithStack(ctx.Err())
			})
			spawn("client", parallel.Fail, func(ctx context.Context) error {
				for {
					msgs, _, err := conn.Receive()
					if err != nil {
						return errors.WithStack(err)
					}

					if err := react(msgs); err != nil {
						return errors.WithStack(err)
					}
				}
			})

			return nil
		})
	})
}

func react(msgs []genetlink.Message) error {
	for _, msg := range msgs {
		ad, err := netlink.NewAttributeDecoder(msg.Data)
		if err != nil {
			return errors.WithStack(err)
		}

		for ad.Next() {
			if strings.HasPrefix(ad.String(), "button/power") {
				switch ad.Bytes()[40] {
				case 0x1:
					return host.ErrPowerOff
				case 0x2:
					return host.ErrReboot
				}
			}
		}
	}
	return nil
}

func subscribe(conn *genetlink.Conn) error {
	f, err := conn.GetFamily(acpiGenlFamilyName)
	if err != nil {
		return errors.WithStack(err)
	}

	for _, group := range f.Groups {
		if group.Name == acpiGenlMcastGroupName {
			return errors.WithStack(conn.JoinGroup(group.ID))
		}
	}
	return errors.New("acpi gen mcast group not found")
}
