package webmail

import (
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/host"
)

const (
	// Port is the port grafana listens on.
	Port = 80

	image = "roundcube/roundcubemail@sha256:6e7420a1b228b639e9e1701afcbfe7820d08572ed9fe34a3560a421ee01c4da6"
)

// Container runs grafana container.
func Container(smtpAddr, imapAddr string) host.Configurator {
	return container.RunImage(image,
		container.EnvVar("ROUNDCUBEMAIL_DEFAULT_HOST", imapAddr),
		container.EnvVar("ROUNDCUBEMAIL_SMTP_SERVER", smtpAddr),
	)
}
