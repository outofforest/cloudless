package astro

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"strconv"

	"github.com/pkg/errors"

	"github.com/outofforest/archive"
	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/host"
)

const (
	// Port is the port astro app listens on.
	Port = 80

	image = "node@sha256:a1bd592d65946bb1d011211351ba7be6a778eaf9617bca02ae3249581ac11dbc"
)

// Container runs astro frontend container.
func Container(appName string, tgz []byte, configurators ...Configurator) host.Configurator {
	astroDir := filepath.Join(cloudless.AppDir(appName), "astro")

	config := Config{
		EnvVars: map[string]string{},
	}

	for _, c := range configurators {
		c(&config)
	}

	return cloudless.Join(
		container.AppMount(appName),
		cloudless.Prepare(func(_ context.Context) error {
			if err := os.RemoveAll(astroDir); err != nil && !os.IsNotExist(err) {
				return errors.WithStack(err)
			}
			return archive.InflateTarGz(bytes.NewReader(tgz), astroDir)
		}),
		container.RunImage(image,
			container.Entrypoint("/usr/local/bin/docker-entrypoint.sh"),
			container.Cmd("node", filepath.Join(astroDir, "dist", "server", "entry.mjs")),
			container.EnvVar("HOST", "0.0.0.0"),
			container.EnvVar("PORT", strconv.Itoa(Port)),
			func(cc *container.RunImageConfig) {
				maps.Copy(cc.EnvVars, config.EnvVars)
			},
		),
	)
}
