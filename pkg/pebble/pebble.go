package pebble

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
)

const (
	// Port is the port pebble listens on.
	Port = 14000

	image      = "ghcr.io/letsencrypt/pebble@sha256:6d78e2b981c77b16e07a2344fb1e0a0beb420af0246816df6810503a2fe74b1b"
	caCerFile  = "ca.cer"
	caKeyFile  = "ca.key"
	configFile = "config.json"
)

var (
	//go:embed config.tmpl.json
	tmpl           string
	configTemplate = template.Must(template.New("").Parse(tmpl))
)

// Container runs grafana container.
func Container(appDir, dnsServer string) host.Configurator {
	return cloudless.Join(
		cloudless.Firewall(firewall.OpenV4TCPPort(Port)),
		container.AppMount(appDir),
		cloudless.Prepare(prepareConfig, prepareCA),
		container.RunImage(image,
			container.EnvVar("HOME", container.AppDir),
			container.EnvVar("PEBBLE_WFE_NONCEREJECT", "0"),
			container.WorkingDir(container.AppDir),
			container.Cmd(
				"-config", filepath.Join(container.AppDir, configFile),
				"-dnsserver", dnsServer,
			),
		))
}

func prepareConfig(_ context.Context) error {
	args := struct {
		ListenPort uint16
		CACertPath string
		CAKeyPath  string
	}{
		ListenPort: Port,
		CACertPath: filepath.Join(container.AppDir, caCerFile),
		CAKeyPath:  filepath.Join(container.AppDir, caKeyFile),
	}

	f, err := os.OpenFile(filepath.Join(container.AppDir, configFile), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o400)
	if err != nil {
		return errors.WithStack(err)
	}
	defer f.Close()

	return errors.WithStack(configTemplate.Execute(f, args))
}

func prepareCA(_ context.Context) error {
	caPrivateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return errors.WithStack(err)
	}

	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:         "Cloudless Certificate Authority",
			OrganizationalUnit: []string{"cloudless"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature |
			x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertificateBytes, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caPrivateKey.PublicKey,
		caPrivateKey)
	if err != nil {
		return errors.WithStack(err)
	}

	caPrivateKeyBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(caPrivateKey),
	}

	caPrivateKeyFile, err := os.OpenFile(filepath.Join(container.AppDir, caKeyFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0o400)
	if err != nil {
		return errors.WithStack(err)
	}
	defer caPrivateKeyFile.Close()

	if err := pem.Encode(caPrivateKeyFile, caPrivateKeyBlock); err != nil {
		return errors.WithStack(err)
	}

	caCertificateBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertificateBytes,
	}

	caCertificateFile, err := os.OpenFile(filepath.Join(container.AppDir, caCerFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0o400)
	if err != nil {
		return errors.WithStack(err)
	}
	defer caCertificateFile.Close()

	return errors.WithStack(pem.Encode(caCertificateFile, caCertificateBlock))
}
