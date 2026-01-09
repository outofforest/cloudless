package wire

// MsgCertificate is used to deliver TLS certificates to HTTP services.
type MsgCertificate struct {
	Certificate []byte
}
