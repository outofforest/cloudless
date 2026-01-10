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
)

const (
	// Port is the port grafana listens on.
	Port = 80

	image = "grafana/grafana@sha256:d3c4a16b994e381144063ca9b0ed4900c6c25cc7697613af6d380469a095ae3e"
)

var (
	//go:embed datasources.tmpl.yaml
	datasourceTmpl     string
	datasourceTemplate = template.Must(template.New("").Parse(datasourceTmpl))

	//go:embed dashboard.tmpl.yaml
	dashboardTmpl     string
	dashboardTemplate = template.Must(template.New("").Parse(dashboardTmpl))
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
func Container(appName string, configurators ...Configurator) host.Configurator {
	var config Config
	appDir := cloudless.AppDir(appName)

	for _, configurator := range configurators {
		configurator(&config)
	}

	return cloudless.Join(
		container.AppMount(appName),
		cloudless.Prepare(func(_ context.Context) error {
			dataSourcesDir := filepath.Join(appDir, "provisioning", "datasources")
			if err := os.RemoveAll(dataSourcesDir); err != nil {
				return errors.WithStack(err)
			}
			if err := os.MkdirAll(dataSourcesDir, 0o700); err != nil {
				return errors.WithStack(err)
			}

			fDataSources, err := os.OpenFile(filepath.Join(dataSourcesDir, "datasources.yaml"),
				os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return errors.WithStack(err)
			}
			defer fDataSources.Close()

			if err := datasourceTemplate.Execute(fDataSources, config.DataSources); err != nil {
				return errors.WithStack(err)
			}

			dashboardsDir := filepath.Join(appDir, "provisioning", "dashboards")
			if err := os.RemoveAll(dashboardsDir); err != nil {
				return errors.WithStack(err)
			}
			if err := os.MkdirAll(dashboardsDir, 0o700); err != nil {
				return errors.WithStack(err)
			}

			fDashboard, err := os.OpenFile(filepath.Join(dashboardsDir, "dashboard.yaml"),
				os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return errors.WithStack(err)
			}
			defer fDashboard.Close()

			if err := dashboardTemplate.Execute(fDashboard, struct {
				AppDir string
			}{
				AppDir: appDir,
			}); err != nil {
				return errors.WithStack(err)
			}

			for i, d := range config.Dashboards {
				if err := os.WriteFile(filepath.Join(dashboardsDir, fmt.Sprintf("%d.json", i)), d, 0o400); err != nil {
					return errors.WithStack(err)
				}
			}

			return errors.WithStack(os.MkdirAll(filepath.Join(appDir, "data"), 0o700))
		}),
		container.RunImage(image,
			container.EnvVar("GF_USERS_ALLOW_SIGN_UP", "false"),
			container.EnvVar("GF_PATHS_PROVISIONING", filepath.Join(appDir, "provisioning")),
			container.EnvVar("GF_PATHS_DATA", filepath.Join(appDir, "data")),
			container.EnvVar("GF_SERVER_HTTP_PORT", strconv.Itoa(Port)),
			container.EnvVar("GF_LOG_MODE", "console"),
			container.EnvVar("GF_LOG_CONSOLE_LEVEL", "info"),
			container.EnvVar("GF_LOG_CONSOLE_FORMAT", "json"),
		),
	)
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

// Dashboards adds dashboards to grafana.
func Dashboards(dashboards ...types.Dashboard) Configurator {
	return func(config *Config) {
		config.Dashboards = append(config.Dashboards, dashboards...)
	}
}
