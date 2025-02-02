package prometheus

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
)

const (
	// Port is the port prometheus listens on.
	Port = 80

	image = "prom/prometheus@sha256:c4c1af714765bd7e06e7ae8301610c9244686a4c02d5329ae275878e10eb481b"
)

//go:embed config.yaml
var config []byte

// Container runs prometheus container.
func Container(appDir string) host.Configurator {
	return cloudless.Join(
		cloudless.Firewall(firewall.OpenV4TCPPort(Port)),
		container.AppMount(appDir),
		cloudless.Prepare(func(_ context.Context) error {
			return errors.WithStack(os.WriteFile(filepath.Join(container.AppDir, "config.yaml"), config, 0o600))
		}),
		container.RunImage(image,
			container.Cmd(
				"--config.file", filepath.Join(container.AppDir, "config.yaml"),
				"--web.listen-address", fmt.Sprintf("0.0.0.0:%d", Port),
				"--web.enable-remote-write-receiver",
				"--storage.tsdb.path", filepath.Join(container.AppDir, "data"),
				"--storage.tsdb.retention.time=1m",
				"--log.format=json",
				"--log.level=info",
			),
			container.WorkingDir(container.AppDir),
		))
}
