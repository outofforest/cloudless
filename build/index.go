package build

import "github.com/outofforest/build/v2/pkg/types"

// Commands is a definition of commands available in build system.
var Commands = map[string]types.Command{
	"build":     {Fn: buildEFI, Description: "Builds EFI loader"},
	"start":     {Fn: startKernel, Description: "Starts dev environment with direct kernel bool"},
	"start/efi": {Fn: startEFI, Description: "Starts dev environment with EFI boot"},
	"destroy":   {Fn: destroy, Description: "Destroys dev environment"},
}
