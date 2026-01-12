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
)

const (
	// Port is the port pebble listens on.
	Port = 14000

	image      = "ghcr.io/letsencrypt/pebble@sha256:cb3ba60a6f27fbc8e1725bb286f6e82b03c57ea5d5223b5cddd6246222e26b86"
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
func Container(appName, dnsServer string) host.Configurator {
	appDir := cloudless.AppDir(appName)

	return cloudless.Join(
		container.AppMount(appName),
		cloudless.Prepare(prepareConfig(appDir), prepareCA(appDir)),
		container.RunImage(image,
			container.EnvVar("HOME", appDir),
			container.EnvVar("PEBBLE_WFE_NONCEREJECT", "0"),
			container.WorkingDir(appDir),
			container.Cmd(
				"-config", filepath.Join(appDir, configFile),
				"-dnsserver", dnsServer,
			),
		))
}

func prepareConfig(appDir string) host.PrepareFn {
	return func(_ context.Context) error {
		args := struct {
			ListenPort uint16
			CACertPath string
			CAKeyPath  string
		}{
			ListenPort: Port,
			CACertPath: filepath.Join(appDir, caCerFile),
			CAKeyPath:  filepath.Join(appDir, caKeyFile),
		}

		f, err := os.OpenFile(filepath.Join(appDir, configFile), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o400)
		if err != nil {
			return errors.WithStack(err)
		}
		defer f.Close()

		return errors.WithStack(configTemplate.Execute(f, args))
	}
}

func prepareCA(appDir string) host.PrepareFn {
	return func(_ context.Context) error {
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

		caPrivateKeyFile, err := os.OpenFile(filepath.Join(appDir, caKeyFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
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

		caCertificateFile, err := os.OpenFile(filepath.Join(appDir, caCerFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
			0o400)
		if err != nil {
			return errors.WithStack(err)
		}
		defer caCertificateFile.Close()

		return errors.WithStack(pem.Encode(caCertificateFile, caCertificateBlock))
	}
}
