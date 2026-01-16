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
func Container(appName string) host.Configurator {
	appDir := cloudless.AppDir(appName)

	return cloudless.Join(
		container.AppMount(appName),
		cloudless.Prepare(func(_ context.Context) error {
			data := struct {
				AppDir   string
				HTTPPort uint16
			}{
				AppDir:   appDir,
				HTTPPort: Port,
			}

			f, err := os.OpenFile(filepath.Join(appDir, "config.tmpl.yaml"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return errors.WithStack(err)
			}
			defer f.Close()

			return errors.WithStack(configTemplate.Execute(f, data))
		}),
		container.RunImage(image,
			container.Cmd(
				"-config.file", filepath.Join(appDir, "config.tmpl.yaml"),
			),
			container.WorkingDir(appDir),
		))
}
