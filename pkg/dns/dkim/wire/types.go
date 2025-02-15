package wire

// MsgRequest is used to request dns record creation for DKIM.
type MsgRequest struct {
	Provider  string
	PublicKey []byte
}

// MsgAck acknowledges record creation.
type MsgAck struct{}
