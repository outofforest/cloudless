package wire

// MsgRequest is used to request dns record creation for ACME challenges.
type MsgRequest struct {
	Challenges []Challenge
}

// MsgAck acknowledges record creation.
type MsgAck struct{}

// Challenge is the ACME challenge.
type Challenge struct {
	Domain string
	Value  string
}
