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
	"github.com/outofforest/tools/pkg/tools/zig"
)

const (
	initBinPath   = "bin/init"
	initramfsPath = "bin/embed/initramfs"
)

func buildLoader(ctx context.Context, deps types.DepsFunc) error {
	deps(prepareEmbeds, zig.EnsureZig)

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

func prepareEmbeds(ctx context.Context, deps types.DepsFunc) error {
	deps(buildInit, buildDistro)

	initramfsF, err := os.OpenFile(initramfsPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.WithStack(err)
	}
	defer initramfsF.Close()

	cW := gzip.NewWriter(initramfsF)
	defer cW.Close()

	w := cpio.NewWriter(cW)
	defer w.Close()

	if err := addFile(w, 0o600, finalDistroPath); err != nil {
		return err
	}
	return addFile(w, 0o700, initBinPath)
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
