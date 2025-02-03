package grafana

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"text/template"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/grafana/types"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
)

const (
	// Port is the port grafana listens on.
	Port = 80

	image = "grafana/grafana@sha256:58aeabeae706b990b3b1fc5ae8c97fd131921b2d6eb26a137ebaa91689d6ebfe"
)

var (
	//go:embed datasources.tmpl.yaml
	datasourceTmpl     string
	datasourceTemplate = template.Must(template.New("").Parse(datasourceTmpl))

	//go:embed dashboard.yaml
	dashboard []byte
)

// Config is the configuration of grafana.
type Config struct {
	DataSources []DataSourceConfig
	Dashboards  []types.Dashboard
}

// DataSourceConfig is the configuration of data source.
type DataSourceConfig struct {
	Name string
	Type DataSourceType
	URL  string
}

// DataSourceType defines data source type.
type DataSourceType string

// Supported data sources.
const (
	DataSourceLoki       DataSourceType = "loki"
	DataSourcePrometheus DataSourceType = "prometheus"
)

// Configurator defines the function configuring grafana.
type Configurator func(config *Config)

// Container runs grafana container.
func Container(appDir string, configurators ...Configurator) host.Configurator {
	var config Config

	for _, configurator := range configurators {
		configurator(&config)
	}

	return cloudless.Join(
		cloudless.Firewall(firewall.OpenV4TCPPort(Port)),
		container.AppMount(appDir),
		cloudless.Prepare(func(_ context.Context) error {
			dataSourcesDir := filepath.Join(container.AppDir, "provisioning", "datasources")
			if err := os.MkdirAll(dataSourcesDir, 0o700); err != nil {
				return errors.WithStack(err)
			}

			f, err := os.OpenFile(filepath.Join(dataSourcesDir, "datasources.yaml"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return errors.WithStack(err)
			}
			defer f.Close()

			if err := datasourceTemplate.Execute(f, config.DataSources); err != nil {
				return errors.WithStack(err)
			}

			dashboardsDir := filepath.Join(container.AppDir, "provisioning", "dashboards")
			if err := os.MkdirAll(dashboardsDir, 0o700); err != nil {
				return errors.WithStack(err)
			}
			if err := os.WriteFile(filepath.Join(dashboardsDir, "dashboard.yaml"), dashboard, 0o400); err != nil {
				return errors.WithStack(err)
			}

			for i, d := range config.Dashboards {
				if err := os.WriteFile(filepath.Join(dashboardsDir, fmt.Sprintf("%d.json", i)), d, 0o400); err != nil {
					return errors.WithStack(err)
				}
			}

			return errors.WithStack(os.MkdirAll(filepath.Join(container.AppDir, "data"), 0o700))
		}),
		container.RunImage(image,
			container.EnvVar("GF_USERS_ALLOW_SIGN_UP", "false"),
			container.EnvVar("GF_PATHS_PROVISIONING", filepath.Join(container.AppDir, "provisioning")),
			container.EnvVar("GF_PATHS_DATA", filepath.Join(container.AppDir, "data")),
			container.EnvVar("GF_SERVER_HTTP_PORT", strconv.Itoa(Port)),
			container.EnvVar("GF_LOG_MODE", "console"),
			container.EnvVar("GF_LOG_CONSOLE_LEVEL", "info"),
			container.EnvVar("GF_LOG_CONSOLE_FORMAT", "json"),
		))
}

// DataSource adds data source to grafana.
func DataSource(name string, sourceType DataSourceType, url string) Configurator {
	return func(config *Config) {
		config.DataSources = append(config.DataSources, DataSourceConfig{
			Name: name,
			Type: sourceType,
			URL:  url,
		})
	}
}

// Dashboard adds dashboard to grafana.
func Dashboard(dashboard types.Dashboard) Configurator {
	return func(config *Config) {
		config.Dashboards = append(config.Dashboards, dashboard)
	}
}
