package kernel

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltInModule(t *testing.T) {
	requireT := require.New(t)

	isBuiltIn, err := isBuiltInModule("ext4", filepath.Join("test", fileBuiltIn))
	requireT.NoError(err)
	requireT.True(isBuiltIn)
}

func TestFindModulesToLoad(t *testing.T) {
	requireT := require.New(t)

	modulesToLoad, err := findModulesToLoad("virtio_net",
		filepath.Join("test", fileDeps))
	requireT.NoError(err)
	requireT.Equal([]string{
		"kernel/drivers/net/virtio_net.ko.xz",
		"kernel/drivers/net/net_failover.ko.xz",
		"kernel/net/core/failover.ko.xz",
	}, modulesToLoad)

	modulesToLoad, err = findModulesToLoad("overlay",
		filepath.Join("test", fileDeps))
	requireT.NoError(err)
	requireT.Equal([]string{
		"kernel/fs/overlayfs/overlay.ko.xz",
	}, modulesToLoad)
}

func TestIsLoaded(t *testing.T) {
	requireT := require.New(t)

	loaded, err := isLoaded("kernel/fs/overlayfs/overlay.ko.xz", "test/proc/modules")
	requireT.NoError(err)
	requireT.True(loaded)

	loaded, err = isLoaded("rfkill.ko.xz", "test/proc/modules")
	requireT.NoError(err)
	requireT.True(loaded)

	loaded, err = isLoaded("nonexistingmodule.ko.xz", "test/proc/modules")
	requireT.NoError(err)
	requireT.False(loaded)
}
