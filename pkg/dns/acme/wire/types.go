package wire

// MsgRequest is used to request dns record creation for ACME challenges.
type MsgRequest struct {
	Provider   string
	AccountURI string
	Challenges []Challenge
}

// Challenge is the ACME challenge.
type Challenge struct {
	Domain string
	Value  string
}
