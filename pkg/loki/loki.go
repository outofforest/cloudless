package loki

import (
	"context"
	_ "embed"
	"os"
	"path/filepath"
	"text/template"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
)

const (
	// Port is the http port loki listens on.
	Port = 80

	// Version v3.3.2.
	image = "grafana/loki@sha256:e9d0a18363c9c3022aef6793a7e135000d11127fb0e18b89de08b1e21e629d60"
)

var (
	//go:embed config.tmpl.yaml
	configTmpl     string
	configTemplate = template.Must(template.New("").Parse(configTmpl))
)

// Container runs loki container.
func Container(appDir string) host.Configurator {
	return cloudless.Join(
		cloudless.Firewall(firewall.OpenV4TCPPort(Port)),
		container.AppMount(appDir),
		cloudless.Prepare(func(_ context.Context) error {
			data := struct {
				HTTPPort uint16
			}{
				HTTPPort: Port,
			}

			f, err := os.OpenFile(filepath.Join(container.AppDir, "config.yaml"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return errors.WithStack(err)
			}
			defer f.Close()

			return errors.WithStack(configTemplate.Execute(f, data))
		}),
		container.RunImage(image,
			container.Cmd(
				"-config.file", filepath.Join(container.AppDir, "config.yaml"),
			),
			container.WorkingDir(container.AppDir),
		))
}
