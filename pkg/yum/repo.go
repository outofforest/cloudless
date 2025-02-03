package yum

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
	"github.com/outofforest/cloudless/pkg/thttp"
	"github.com/outofforest/libexec"
	"github.com/outofforest/parallel"
)

const port = 80

// Service returns new yum repo service.
func Service(repoRoot string) host.Configurator {
	var c *host.Configuration
	return cloudless.Join(
		cloudless.Configuration(&c),
		cloudless.Firewall(firewall.OpenV4TCPPort(port)),
		cloudless.Service("yum", parallel.Continue, func(ctx context.Context) error {
			packages := c.Packages()
			if len(packages) == 0 {
				return nil
			}

			if err := os.MkdirAll(repoRoot, 0o700); err != nil {
				return errors.WithStack(err)
			}
			cmdInstall := exec.Command("dnf", "install", "-y", "--refresh", "createrepo_c")
			cmdDownload := exec.Command("dnf",
				append([]string{"download", "--resolve", "--alldeps"}, packages...)...)
			cmdDownload.Dir = repoRoot
			cmdRepo := exec.Command("/usr/bin/createrepo", ".")
			cmdRepo.Dir = repoRoot

			if err := libexec.Exec(ctx, cmdInstall, cmdDownload, cmdRepo); err != nil {
				return errors.WithStack(err)
			}

			l, err := net.ListenTCP("tcp", &net.TCPAddr{Port: port})
			if err != nil {
				return errors.WithStack(err)
			}
			defer l.Close()

			server := thttp.NewServer(l, thttp.Config{
				Handler: http.FileServer(http.Dir(repoRoot)),
			})
			return server.Run(ctx)
		}),
	)
}
