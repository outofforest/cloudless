package prometheus

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/host"
)

const (
	// Port is the port prometheus listens on.
	Port = 80

	image = "prom/prometheus@sha256:c4c1af714765bd7e06e7ae8301610c9244686a4c02d5329ae275878e10eb481b"
)

var (
	//go:embed config.tmpl.yaml
	configTmpl     string
	configTemplate = template.Must(template.New("").Parse(configTmpl))
)

// Container runs prometheus container.
func Container(appName string, configurators ...Configurator) host.Configurator {
	var config Config

	for _, c := range configurators {
		c(&config)
	}

	appDir := cloudless.AppDir(appName)
	configPath := filepath.Join(appDir, "config.yaml")

	return cloudless.Join(
		container.AppMount(appName),
		cloudless.Prepare(func(_ context.Context) error {
			f, err := os.OpenFile(configPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return errors.WithStack(err)
			}
			defer f.Close()

			return errors.WithStack(configTemplate.Execute(f, config))
		}),
		container.RunImage(image,
			container.Cmd(
				"--config.file", configPath,
				"--web.listen-address", fmt.Sprintf("0.0.0.0:%d", Port),
				"--storage.tsdb.path", filepath.Join(appDir, "data"),
				"--storage.tsdb.retention.time=1m",
				"--log.format=json",
				"--log.level=info",
			),
			container.WorkingDir(appDir),
		))
}
