package build

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

	"github.com/outofforest/build/v2/pkg/types"
	"github.com/outofforest/logger"
	"github.com/outofforest/tools/pkg/tools/zig"
)

const (
	embedDir = "bin/embed"
)

// Loader builds UEFI loader for application.
func Loader(ctx context.Context, deps types.DepsFunc, config Config) error {
	deps(zig.EnsureZig)

	distroDir, err := buildDistro(ctx, config.Distro)
	if err != nil {
		return err
	}

	logger.Get(ctx).Info("Building loader")

	if err := os.RemoveAll(embedDir); err != nil && !os.IsNotExist(err) {
		return errors.WithStack(err)
	}
	if err := os.MkdirAll(embedDir, 0o700); err != nil {
		return errors.WithStack(err)
	}

	if err := buildInitramfs(config, filepath.Join(distroDir, initramfsFile),
		filepath.Join(embedDir, initramfsFile)); err != nil {
		return err
	}

	if err := os.Symlink(filepath.Join(distroDir, kernelFile), filepath.Join(embedDir, kernelFile)); err != nil {
		return errors.WithStack(err)
	}

	if err := zig.Build(ctx, deps, zig.BuildConfig{
		PackagePath: "loader",
		OutputPath:  "bin",
	}); err != nil {
		return err
	}

	inF, err := os.Open("bin/bootx64.efi")
	if err != nil {
		return errors.WithStack(err)
	}
	defer inF.Close()

	if err := os.Remove("bin/efi.img"); err != nil && !os.IsNotExist(err) {
		return errors.WithStack(err)
	}

	const size = 1024 * 1024 * 1024
	b, err := file.CreateFromPath("bin/efi.img", size)
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

func buildInitramfs(config Config, baseInitramfsPath, finalInitramfsPath string) error {
	baseInitramfsF, err := os.Open(baseInitramfsPath)
	if err != nil {
		return errors.WithStack(err)
	}
	defer baseInitramfsF.Close()

	finalInitramfsPathTmp := finalInitramfsPath + ".tmp"
	finalInitramfsF, err := os.OpenFile(finalInitramfsPathTmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
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

	if err := addFile(w, 0o700, config.InitBinPath); err != nil {
		return err
	}

	return errors.WithStack(os.Rename(finalInitramfsPathTmp, finalInitramfsPath))
}

func addFile(w *cpio.Writer, mode cpio.FileMode, file string) error {
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
