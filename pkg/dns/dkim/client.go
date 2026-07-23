package dkim

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"time"

	"github.com/pkg/errors"
	"github.com/samber/lo"

	"github.com/outofforest/cloudless/pkg/dns/dkim/wire"
	"github.com/outofforest/wave"
)

// Config stores DKIM config.
type Config struct {
	Provider string
	Signer   crypto.Signer
}

// NewConfig creates new DKIM config.
func NewConfig(appName string) Config {
	var timeBytes [8]byte
	binary.BigEndian.PutUint64(timeBytes[:], uint64(time.Now().Unix()))

	config := Config{
		Provider: appName + "-" + hex.EncodeToString(timeBytes[:]),

		// TODO (wojciech): Change to ED25519 once smtp servers support it finally.
		Signer: lo.Must(rsa.GenerateKey(rand.Reader, 2048)),
	}

	return config
}

// RunClient runs wave client sending DKIM config to DNS servers.
func RunClient(
	ctx context.Context,
	waveClient *wave.Client,
	config Config,
) error {
	pubKeyMarshalled, err := x509.MarshalPKIXPublicKey(config.Signer.Public())
	if err != nil {
		return errors.WithStack(err)
	}

	m := wire.NewMarshaller()
	for {
		if err := waveClient.Send(&wire.MsgRequest{
			Provider:  config.Provider,
			PublicKey: pubKeyMarshalled,
		}, m); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return errors.WithStack(ctx.Err())
		case <-time.After(RefreshInterval):
		}
	}
}
