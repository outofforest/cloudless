package yum

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/thttp"
	"github.com/outofforest/libexec"
	"github.com/outofforest/parallel"
)

// Port is the port yum listens on.
const Port = 80

// Service returns new yum repo service.
func Service(repoRoot string, release uint64) host.Configurator {
	var c host.SealedConfiguration
	return cloudless.Join(
		cloudless.Configuration(&c),
		cloudless.Service("yum", parallel.Continue, func(ctx context.Context) error {
			packages := c.Packages()
			if len(packages) == 0 {
				return nil
			}

			repoDir := filepath.Join(repoRoot, strconv.FormatUint(release, 10))
			if err := createRepo(ctx, repoDir, packages); err != nil {
				return err
			}

			l, err := net.ListenTCP("tcp", &net.TCPAddr{Port: Port})
			if err != nil {
				return errors.WithStack(err)
			}
			defer l.Close()

			server := thttp.NewServer(l, thttp.Config{
				Handler: http.FileServer(http.Dir(repoDir)),
			})
			return server.Run(ctx)
		}),
	)
}

func createRepo(ctx context.Context, repoDir string, packages []string) error {
	repoInfo, err := os.Stat(repoDir)
	if err == nil && repoInfo.IsDir() {
		return nil
	}

	if err := os.RemoveAll(repoDir); err != nil {
		return errors.WithStack(err)
	}

	repoDirTmp := repoDir + ".tmp"
	if err := os.RemoveAll(repoDirTmp); err != nil {
		return errors.WithStack(err)
	}
	if err := os.MkdirAll(repoDirTmp, 0o700); err != nil {
		return errors.WithStack(err)
	}

	cmdInstall := exec.Command("dnf", "install", "-y",
		"--refresh",
		"--setopt=keepcache=False",
		"createrepo_c",
	)
	cmdDownload := exec.Command("dnf", append([]string{
		"download", "--resolve", "--alldeps",
		"--setopt=keepcache=False",
		"--setopt=max_parallel_downloads=20",
	}, packages...)...)
	cmdDownload.Dir = repoDirTmp
	cmdRepo := exec.Command("/usr/bin/createrepo", ".")
	cmdRepo.Dir = repoDirTmp

	if err := libexec.Exec(ctx, cmdInstall, cmdDownload, cmdRepo); err != nil {
		return errors.WithStack(err)
	}

	return errors.WithStack(os.Rename(repoDirTmp, repoDir))
}
