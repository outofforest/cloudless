package astro

import (
	"context"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strconv"

	"github.com/pkg/errors"

	"github.com/outofforest/archive"
	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/host"
)

// Port is the port astro app listens on.
const Port = 80

// Container runs astro frontend container.
func Container(appName string, fs fs.FS, configurators ...Configurator) host.Configurator {
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
			f, err := fs.Open("frontend.tgz")
			if err != nil {
				return errors.WithStack(err)
			}
			defer f.Close()

			if err := os.RemoveAll(astroDir); err != nil && !os.IsNotExist(err) {
				return errors.WithStack(err)
			}
			return archive.InflateTarGz(f, astroDir)
		}),
		container.RunImage(pkg.NodeDockerImage,
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
