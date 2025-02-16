package main

import (
	"github.com/outofforest/cloudless/pkg/dns/dkim/wire"
	"github.com/outofforest/proton"
)

//go:generate go run .

func main() {
	proton.Generate("../types.proton.go",
		wire.MsgRequest{},
		wire.MsgAck{},
	)
}
