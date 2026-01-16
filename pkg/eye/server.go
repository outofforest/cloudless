package eye

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/thttp"
	"github.com/outofforest/cloudless/pkg/tnet"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

// MetricPort is the port metric server listens on.
const MetricPort = 9000

// Config is the configuration of metric server.
type Config struct {
	Addresses []net.IP
}

// Configurator is the function configuring metric server.
type Configurator func(c *Config)

// Addresses adds IPs to query for metrics.
func Addresses(ips ...string) Configurator {
	return func(c *Config) {
		for _, ip := range ips {
			c.Addresses = append(c.Addresses, net.ParseIP(ip))
		}
	}
}

// MetricsServer exposes http server presenting metrics in prometheus format.
func MetricsServer(configurators ...Configurator) host.Configurator {
	var config Config

	for _, c := range configurators {
		c(&config)
	}

	var c host.SealedConfiguration
	return cloudless.Join(
		cloudless.Configuration(&c),
		cloudless.Service("eye-metrics", parallel.Fail, func(ctx context.Context) error {
			ls, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", MetricPort))
			if err != nil {
				return errors.WithStack(err)
			}

			sets := c.MetricSets()

			return thttp.NewServer(ls, thttp.Config{
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					log := logger.Get(ctx)

					for _, s := range sets {
						s.WritePrometheus(w)
					}
					for _, addr := range config.Addresses {
						url := tnet.JoinScheme("http", addr.String(), MetricPort)
						err := func() error {
							ctx, cancel := context.WithTimeout(ctx, time.Second)
							defer cancel()

							req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
							if err != nil {
								return errors.WithStack(err)
							}
							resp, err := http.DefaultClient.Do(req)
							if err != nil {
								return errors.WithStack(err)
							}
							defer resp.Body.Close()

							_, err = io.Copy(w, resp.Body)
							return errors.WithStack(err)
						}()
						if err != nil {
							log.Error("Fetching metrics failed.",
								zap.String("url", url),
								zap.Error(err),
							)
						}
					}
				}),
			}).Run(ctx)
		}),
	)
}
