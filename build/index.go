package build

import (
	"github.com/outofforest/build/v2/pkg/types"
)

// Commands is a definition of commands available in build system.
var Commands = map[string]types.Command{
	"build": {Fn: buildBootloader, Description: "Builds EFI bootloader"},
}
