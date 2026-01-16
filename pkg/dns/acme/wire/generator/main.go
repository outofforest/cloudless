package main

import (
	"github.com/outofforest/cloudless/pkg/dns/acme/wire"
	"github.com/outofforest/proton"
)

//go:generate go run .

func main() {
	proton.Generate("../types.proton.go",
		proton.Message[wire.MsgRequest](),
	)
}
