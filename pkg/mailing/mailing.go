package mailing

import (
	"context"
	"net"
	netmail "net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/wneessen/go-mail"
	"github.com/wneessen/go-mail-middleware/dkim"

	dnsdkim "github.com/outofforest/cloudless/pkg/dns/dkim"
)

// NewMessage creates new email message builder.
func NewMessage() *mail.Msg {
	return mail.NewMsg(mail.WithNoDefaultUserAgent())
}

// SendMessage sends message.
func SendMessage(ctx context.Context, config Config, dkimConfig dnsdkim.Config, msg *mail.Msg) error {
	recipients, err := msg.GetRecipients()
	if err != nil {
		return errors.WithStack(err)
	}

	senderStr, err := msg.GetSender(false)
	if err != nil {
		return errors.WithStack(err)
	}

	senderParsed, err := netmail.ParseAddress(senderStr)
	if err != nil {
		return errors.WithStack(err)
	}
	sender := senderParsed.Address

	senderDomain, err := domainFromEmail(sender)
	if err != nil {
		return err
	}

	msg.SetMessageIDWithValue(uuid.New().String() + "@" + senderDomain)

	dkimMidConfig, err := dkim.NewConfig(senderDomain, dkimConfig.Provider)
	if err != nil {
		return errors.WithStack(err)
	}
	dkimMiddleware, err := dkim.NewFromRSAKey(dkimConfig.PrivateKeyPEM, dkimMidConfig)
	if err != nil {
		return errors.WithStack(err)
	}
	msg = dkimMiddleware.Handle(msg)
	for _, recipientStr := range recipients {
		recipientParsed, err := netmail.ParseAddress(recipientStr)
		if err != nil {
			return errors.WithStack(err)
		}

		if err := send(ctx, config, msg, recipientParsed.Address); err != nil {
			return err
		}
	}

	return nil
}

func send(
	ctx context.Context,
	config Config,
	msg *mail.Msg,
	recipient string,
) error {
	domain, err := domainFromEmail(recipient)
	if err != nil {
		return err
	}

	mxs, err := config.Resolver.LookupMX(ctx, domain)
	if err != nil {
		return errors.WithStack(err)
	}

	client, err := mail.NewClient(mxs[0].Host, mail.WithPort(25), mail.WithTLSPolicy(mail.TLSOpportunistic),
		mail.WithHELO(config.Hostname), mail.WithDialContextFunc(dialFunc(config.Resolver)), mail.WithoutNoop())
	if err != nil {
		return errors.WithStack(err)
	}
	defer client.Close()

	return errors.WithStack(client.DialAndSendWithContext(ctx, msg))
}

func dialFunc(resolver *net.Resolver) mail.DialContextFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		parts := strings.SplitN(address, ".:", 2)
		if len(parts) != 2 {
			return nil, errors.Errorf("invalid address: %s", address)
		}

		address = parts[0]
		port := parts[1]

		ips, err := resolver.LookupIP(ctx, "ip", address)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		if len(ips) == 0 {
			return nil, errors.Errorf("no IP addresses found for %s", address)
		}

		d := &net.Dialer{}
		conn, err := d.DialContext(ctx, network, ips[0].String()+":"+port)
		return conn, errors.WithStack(err)
	}
}

func domainFromEmail(email string) (string, error) {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return "", errors.New("invalid email")
	}
	return parts[1], nil
}
