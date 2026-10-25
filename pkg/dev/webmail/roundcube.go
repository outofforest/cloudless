package webmail

import (
	"github.com/outofforest/cloudless/pkg"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/host"
)

// Port is the port grafana listens on.
const Port = 80

// Container runs grafana container.
func Container(smtpAddr, imapAddr string) host.Configurator {
	return container.RunImage(pkg.RoundcubeDockerImage,
		container.EnvVar("ROUNDCUBEMAIL_DEFAULT_HOST", imapAddr),
		container.EnvVar("ROUNDCUBEMAIL_SMTP_SERVER", smtpAddr),
	)
}
