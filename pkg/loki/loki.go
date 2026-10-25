package loki

import (
	"context"
	_ "embed"
	"os"
	"path/filepath"
	"text/template"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/host"
)

// Port is the http port loki listens on.
const Port = 80

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
		container.RunImage(pkg.LokiDockerImage,
			container.Cmd(
				"-config.file", filepath.Join(appDir, "config.tmpl.yaml"),
			),
			container.WorkingDir(appDir),
		))
}
