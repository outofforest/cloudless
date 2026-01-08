package cloudless

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/cavaliergopher/cpio"
	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/fat32"
	"github.com/pkg/errors"

	"github.com/outofforest/archive"
	"github.com/outofforest/build/v2/pkg/types"
	"github.com/outofforest/logger"
	"github.com/outofforest/tools/pkg/tools/zig"
)

// BuildEFI builds EFI loader.
func BuildEFI(ctx context.Context, deps types.DepsFunc, config Config) error {
	deps(zig.EnsureZig)

	distroDir, err := buildDistro(ctx, config.Distro)
	if err != nil {
		return err
	}

	logger.Get(ctx).Info("Building loader")

	efiSrcFile := filepath.Join(distroDir, efiFile)
	efiF, err := os.Open(efiSrcFile)
	if err != nil {
		return errors.WithStack(err)
	}
	defer efiF.Close()

	efiDir, err := os.MkdirTemp("", "efi")
	if err != nil {
		return errors.WithStack(err)
	}
	defer os.RemoveAll(efiDir) //nolint:errcheck

	efiUnpackDir := filepath.Join(efiDir, "build")
	if err := archive.Inflate(efiSrcFile, efiF, efiUnpackDir); err != nil {
		return err
	}

	dirs, err := os.ReadDir(efiUnpackDir)
	if err != nil {
		return errors.WithStack(err)
	}
	if len(dirs) != 1 || !dirs[0].IsDir() {
		return errors.New("expected exactly one subdirectory in EFI release")
	}

	efiPkgDir := filepath.Join(efiUnpackDir, dirs[0].Name())

	embedDir := filepath.Join(efiPkgDir, "src", "embed")
	if err := os.MkdirAll(embedDir, 0o700); err != nil {
		return errors.WithStack(err)
	}

	if err := buildInitramfs(ctx, config, filepath.Join(embedDir, initramfsFile)); err != nil {
		return err
	}

	if err := os.Symlink(kernelPath(distroDir), filepath.Join(embedDir, kernelFile)); err != nil {
		return errors.WithStack(err)
	}

	efiOutDir := filepath.Join(efiDir, "out")
	if err := zig.Build(ctx, deps, zig.BuildConfig{
		PackagePath: efiPkgDir,
		OutputPath:  efiOutDir,
	}); err != nil {
		return err
	}

	inF, err := os.Open(filepath.Join(efiOutDir, "bootx64.efi"))
	if err != nil {
		return errors.WithStack(err)
	}
	defer inF.Close()

	if err := os.MkdirAll(filepath.Base(config.Output.EFI), 0o700); err != nil {
		return errors.WithStack(err)
	}
	if err := os.Remove(config.Output.EFI); err != nil && !os.IsNotExist(err) {
		return errors.WithStack(err)
	}

	const size = 200 * 1024 * 1024
	b, err := file.CreateFromPath(config.Output.EFI, size)
	if err != nil {
		return errors.WithStack(err)
	}
	defer b.Close()

	fs, err := fat32.Create(b, size, 0, 0, "efi")
	if err != nil {
		return errors.WithStack(err)
	}

	if err := fs.Mkdir("/EFI/BOOT"); err != nil {
		return errors.WithStack(err)
	}

	outF, err := fs.OpenFile("/EFI/BOOT/bootx64.efi", os.O_RDWR|os.O_TRUNC|os.O_CREATE)
	if err != nil {
		return errors.WithStack(err)
	}
	defer outF.Close()

	_, err = io.Copy(outF, inF)
	return errors.WithStack(err)
}

// BuildKernel builds kernel.
func BuildKernel(ctx context.Context, config Config) error {
	distroDir, err := buildDistro(ctx, config.Distro)
	if err != nil {
		return err
	}

	if err := os.Remove(config.Output.Kernel); err != nil && !os.IsNotExist(err) {
		return errors.WithStack(err)
	}
	return errors.WithStack(os.Symlink(kernelPath(distroDir), config.Output.Kernel))
}

// BuildInitramfs builds initramfs.
func BuildInitramfs(ctx context.Context, config Config) error {
	return buildInitramfs(ctx, config, config.Output.Initramfs)
}

func buildInitramfs(ctx context.Context, config Config, finalInitramfsPath string) error {
	distroDir, err := buildDistro(ctx, config.Distro)
	if err != nil {
		return err
	}

	baseInitramfsF, err := os.Open(initramfsPath(distroDir))
	if err != nil {
		return errors.WithStack(err)
	}
	defer baseInitramfsF.Close()

	finalInitramfsF, err := os.OpenFile(finalInitramfsPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.WithStack(err)
	}
	defer finalInitramfsF.Close()

	if _, err := io.Copy(finalInitramfsF, baseInitramfsF); err != nil {
		return errors.WithStack(err)
	}

	cW := gzip.NewWriter(finalInitramfsF)
	defer cW.Close()

	w := cpio.NewWriter(cW)
	defer w.Close()

	return addFileToInitramfs(w, 0o700, config.Input.InitBin)
}

func addFileToInitramfs(w *cpio.Writer, mode cpio.FileMode, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return errors.WithStack(err)
	}
	defer f.Close()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return errors.WithStack(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return errors.WithStack(err)
	}

	if err := w.WriteHeader(&cpio.Header{
		Name: filepath.Base(file),
		Size: size,
		Mode: mode,
	}); err != nil {
		return errors.WithStack(err)
	}

	_, err = io.Copy(w, f)
	return errors.WithStack(err)
}
