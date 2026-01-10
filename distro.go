package cloudless

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cavaliergopher/cpio"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/sassoftware/go-rpmutils"
	"github.com/ulikunitz/xz"
	"go.uber.org/zap"

	"github.com/outofforest/archive"
	"github.com/outofforest/logger"
)

const (
	efiURL           = "https://github.com/outofforest/efi/archive/refs/tags/%s.tar.gz"
	configFile       = "config.json"
	efiFile          = "efi.tar.gz"
	distroFile       = "distro.tar"
	initramfsFile    = "initramfs"
	kernelFile       = "vmlinuz"
	moduleDir        = "modules"
	depsFile         = "deps.json"
	kernelTargetPath = "/boot/" + kernelFile
	modulePathPrefix = "/lib/modules/"
	moduleTargetDir  = "/usr/lib/modules"
)

func buildDistro(ctx context.Context, config DistroConfig) (retConfigDir string, retErr error) {
	distroDir, err := distroDir(config)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(distroDir); err == nil {
		return distroDir, nil
	}

	distroDirTmp := distroDir + ".tmp"

	logger.Get(ctx).Info("Building distro")

	if err := os.RemoveAll(distroDirTmp); err != nil {
		return "", errors.WithStack(err)
	}

	if err := os.MkdirAll(distroDirTmp, 0o700); err != nil {
		return "", errors.WithStack(err)
	}

	defer func() {
		if retErr != nil {
			_ = os.RemoveAll(distroDirTmp)
		}
	}()

	configBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", errors.WithStack(err)
	}
	configPath := filepath.Join(distroDirTmp, configFile)

	if err := os.WriteFile(configPath, configBytes, 0o600); err != nil {
		return "", errors.WithStack(err)
	}

	if err := downloadFile(ctx, fmt.Sprintf(efiURL, config.EFI.Version),
		filepath.Join(distroDirTmp, efiFile), config.EFI.Hash); err != nil {
		return "", err
	}

	distroPath := filepath.Join(distroDirTmp, distroFile)
	distroF, err := distroFromBase(ctx, config.Base, distroPath)
	if err != nil {
		return "", err
	}
	defer distroF.Close()

	distroWriter := tar.NewWriter(distroF)
	defer distroWriter.Close()

	if err := addKernelToDistro(ctx, config.KernelPackage, kernelPath(distroDirTmp), distroWriter); err != nil {
		return "", err
	}

	if err := addModulesToDistro(ctx, config, distroDirTmp, distroWriter); err != nil {
		return "", err
	}

	for _, pkg := range config.BtrfsPackages {
		if err := addURLToDistro(ctx, filepath.Join("rpm", "btrfs", filepath.Base(pkg.URL)), pkg.URL, pkg.Hash, 0o400,
			distroWriter); err != nil {
			return "", err
		}
	}

	initramfsF, err := os.OpenFile(initramfsPath(distroDirTmp), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", errors.WithStack(err)
	}
	defer initramfsF.Close()

	cW := gzip.NewWriter(initramfsF)
	defer cW.Close()

	w := cpio.NewWriter(cW)
	defer w.Close()

	if err := addFileToInitramfs(w, 0o600, distroPath); err != nil {
		return "", err
	}

	if err := os.Rename(distroDirTmp, distroDir); err != nil {
		return "", errors.WithStack(err)
	}

	return distroDir, nil
}

func distroFromBase(ctx context.Context, base Resource, path string) (retF *os.File, retErr error) {
	log := logger.Get(ctx)
	log.Info("Downloading distro base", zap.String("url", base.URL))

	reader, _, err := streamFromURL(ctx, base.URL)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer reader.Close()

	hReader, err := archive.NewHashingReader(reader, base.Hash)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Fedora .tar files are .tar.gz in reality.
	gr, err := gzip.NewReader(hReader)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer gr.Close()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, errors.WithStack(err)
	}
	f, err := os.OpenFile(path, os.O_TRUNC|os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer func() {
		if retErr != nil {
			_ = f.Close()
		}
	}()

	tr := tar.NewReader(io.TeeReader(gr, f))

	var lastFileSize, lastStreamPos int64
loop:
	for {
		hdr, err := tr.Next()
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			break loop
		default:
			return nil, errors.WithStack(err)
		}

		lastStreamPos, err = f.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		lastFileSize = hdr.Size
	}

	if err := hReader.ValidateChecksum(); err != nil {
		return nil, errors.Wrapf(err, "downloading distro base %q failed", base.URL)
	}

	if err := roundTarToBlock(f, lastStreamPos+lastFileSize); err != nil {
		return nil, errors.WithStack(err)
	}

	return f, err
}

func addFileToDistro(dstPath, srcPath string, mode os.FileMode, w *tar.Writer) error {
	reader, size, err := streamFromFile(srcPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := w.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     dstPath,
		Size:     size,
		Mode:     int64(mode),
	}); err != nil {
		return errors.WithStack(err)
	}

	_, err = io.Copy(w, reader)
	return errors.WithStack(err)
}

func addURLToDistro(ctx context.Context, dstPath, srcURL, checksum string, mode os.FileMode, w *tar.Writer) error {
	log := logger.Get(ctx)
	log.Info("Adding file", zap.String("url", srcURL))

	reader, size, err := streamFromURL(ctx, srcURL)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := w.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     dstPath,
		Size:     size,
		Mode:     int64(mode),
	}); err != nil {
		return errors.WithStack(err)
	}

	if err := copyStream(w, reader, checksum); err != nil {
		return errors.Wrapf(err, "downloading file %q failed", srcURL)
	}

	return nil
}

func addKernelToDistro(ctx context.Context, kernelPackage Resource, path string, w *tar.Writer) error {
	log := logger.Get(ctx)
	log.Info("Adding kernel module", zap.String("url", kernelPackage.URL))

	reader, _, err := streamFromURL(ctx, kernelPackage.URL)
	if err != nil {
		return err
	}
	defer reader.Close()

	hReader, err := archive.NewHashingReader(reader, kernelPackage.Hash)
	if err != nil {
		return err
	}

	kernelReader, size, err := searchRPM(hReader, kernelFile)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errors.WithStack(err)
	}

	kernelF, err := os.OpenFile(path, os.O_TRUNC|os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return errors.WithStack(err)
	}
	defer kernelF.Close()

	if err := w.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     kernelTargetPath,
		Size:     size,
		Mode:     0o500,
	}); err != nil {
		return errors.WithStack(err)
	}

	if _, err := io.Copy(kernelF, io.TeeReader(kernelReader, w)); err != nil {
		return errors.WithStack(err)
	}
	if err := hReader.ValidateChecksum(); err != nil {
		return errors.Wrapf(err, "downloading kernel module %q failed", kernelPackage.URL)
	}

	return nil
}

func addModulesToDistro(ctx context.Context, config DistroConfig, distroDir string, w *tar.Writer) error {
	moduleDir := filepath.Join(distroDir, moduleDir)
	depsPath := filepath.Join(distroDir, depsFile)
	if err := downloadModules(ctx, config.KernelModulePackages, config.KernelModules,
		moduleDir, depsPath); err != nil {
		return err
	}

	depF, err := os.Open(depsPath)
	if err != nil {
		return errors.WithStack(err)
	}
	defer depF.Close()

	depMod := map[string][]string{}
	if err := json.NewDecoder(depF).Decode(&depMod); err != nil {
		return errors.WithStack(err)
	}

	modules := map[string]struct{}{}
	for _, m := range config.KernelModules {
		modules[m] = struct{}{}
	}
	for m, deps := range depMod {
		modules[m] = struct{}{}
		for _, d := range deps {
			modules[d] = struct{}{}
		}
	}
	install := make([]string, 0, len(modules))
	for m := range modules {
		install = append(install, m)
	}
	sort.Strings(install)

	for _, mName := range install {
		fileName := mName + ".ko"
		if err := addFileToDistro(filepath.Join(moduleTargetDir, fileName),
			filepath.Join(moduleDir, fileName), 0o400, w); err != nil {
			return err
		}
	}

	return addFileToDistro(filepath.Join(moduleTargetDir, depsFile), depsPath, 0o400, w)
}

func streamFromFile(path string) (retR io.ReadSeekCloser, retSize int64, retErr error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, errors.WithStack(err)
	}
	defer func() {
		if retErr != nil {
			_ = f.Close()
		}
	}()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, 0, errors.WithStack(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, 0, errors.WithStack(err)
	}

	return f, size, nil
}

func streamFromURL(ctx context.Context, url string) (retR io.ReadCloser, retSize int64, retErr error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, errors.WithStack(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, errors.WithStack(err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, 0, errors.Errorf("unexpected status code %d, url: %q", resp.StatusCode, url)
	}

	defer func() {
		if retErr != nil {
			_ = resp.Body.Close()
		}
	}()

	return resp.Body, resp.ContentLength, nil
}

func copyStream(w io.Writer, r io.Reader, checksum string) error {
	reader, err := archive.NewHashingReader(r, checksum)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, reader)
	if err != nil {
		return errors.WithStack(err)
	}

	return reader.ValidateChecksum()
}

func downloadFile(ctx context.Context, url, path, checksum string) error {
	log := logger.Get(ctx)
	log.Info("Downloading file", zap.String("url", url))

	reader, _, err := streamFromURL(ctx, url)
	if err != nil {
		return errors.WithStack(err)
	}
	defer reader.Close()

	f, err := os.OpenFile(path, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return errors.WithStack(err)
	}
	defer f.Close()

	if err := copyStream(f, reader, checksum); err != nil {
		return errors.Wrapf(err, "downloading file %q failed", path)
	}
	return err
}

func downloadModules(ctx context.Context, packages []Resource, modules []string, moduleDir, depsFile string) error {
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		return errors.WithStack(err)
	}

	providers := map[string]string{}
	requires := map[string][]string{}

	for _, pkg := range packages {
		if err := downloadModulesFromPackage(ctx, pkg, moduleDir, providers, requires); err != nil {
			return err
		}
	}

	dependencies := map[string][]string{}
	for mName, ss := range requires {
		deps := map[string]struct{}{}
		for _, s := range ss {
			dep := providers[s]
			if dep == "" {
				continue
			}
			if _, exists := deps[dep]; exists {
				continue
			}
			dependencies[mName] = append(dependencies[mName], dep)
			deps[dep] = struct{}{}
		}
	}

	included := map[string]struct{}{}
	finalDependencies := map[string][]string{}
	stack := append([]string{}, modules...)
	for len(stack) > 0 {
		mName := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		included[mName] = struct{}{}
		deps, exists := dependencies[mName]
		if !exists {
			continue
		}

		finalDependencies[mName] = deps
		for _, d := range deps {
			if _, exists := included[d]; !exists {
				stack = append(stack, d)
			}
		}
	}

	return errors.WithStack(os.WriteFile(depsFile, lo.Must(json.MarshalIndent(finalDependencies, "", "  ")), 0o600))
}

func downloadModulesFromPackage(
	ctx context.Context,
	pkg Resource,
	moduleDir string,
	providers map[string]string,
	requires map[string][]string,
) error {
	log := logger.Get(ctx)
	log.Info("Downloading modules", zap.String("url", pkg.URL))

	reader, _, err := streamFromURL(ctx, pkg.URL)
	if err != nil {
		return err
	}
	defer reader.Close()

	hReader, err := archive.NewHashingReader(reader, pkg.Hash)
	if err != nil {
		return err
	}

	rpm, err := rpmutils.ReadRpm(hReader)
	if err != nil {
		return errors.WithStack(err)
	}
	pReader, err := rpm.PayloadReaderExtended()
	if err != nil {
		return errors.WithStack(err)
	}

loop:
	for {
		fInfo, err := pReader.Next()
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			break loop
		default:
			return errors.WithStack(err)
		}

		if !strings.HasPrefix(fInfo.Name(), modulePathPrefix) {
			continue
		}

		fileName := filepath.Base(fInfo.Name())
		if !strings.HasSuffix(fileName, ".ko.xz") || pReader.IsLink() {
			continue
		}

		moduleName, providedSymbols, importedSymbols, err := storeModuleOnDisk(fileName, pReader, moduleDir)
		if err != nil {
			return err
		}

		if len(importedSymbols) > 0 {
			requires[moduleName] = importedSymbols
		}
		for _, s := range providedSymbols {
			if _, exists := providers[s]; exists {
				providers[s] = ""
			} else {
				providers[s] = moduleName
			}
		}
	}

	if err := hReader.ValidateChecksum(); err != nil {
		return errors.Wrapf(err, "rpm checksum mismatch, url: %q", pkg.URL)
	}

	return nil
}

func storeModuleOnDisk(fileName string, r io.Reader, moduleDir string) (string, []string, []string, error) {
	xr, err := xz.NewReader(r)
	if err != nil {
		return "", nil, nil, errors.WithStack(err)
	}

	fileName = strings.ReplaceAll(strings.TrimSuffix(fileName, ".xz"), "_", "-")
	moduleName := strings.TrimSuffix(fileName, ".ko")
	modulePath := filepath.Join(moduleDir, fileName)
	modF, err := os.OpenFile(modulePath, os.O_TRUNC|os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return "", nil, nil, errors.WithStack(err)
	}
	defer modF.Close()

	_, err = io.Copy(modF, xr)
	if err != nil {
		return "", nil, nil, errors.WithStack(err)
	}

	if _, err := modF.Seek(0, io.SeekStart); err != nil {
		return "", nil, nil, errors.WithStack(err)
	}

	elfF, err := elf.NewFile(modF)
	if err != nil {
		return "", nil, nil, errors.Wrap(err, fileName)
	}

	symbols, err := elfF.Symbols()
	if errors.Is(err, elf.ErrNoSymbols) {
		return moduleName, nil, nil, nil
	}
	if err != nil {
		return "", nil, nil, errors.WithStack(err)
	}

	providedSymbols := []string{}
	importedSymbols := []string{}
	for _, s := range symbols {
		bind := elf.ST_BIND(s.Info)
		switch {
		case s.Name == "" || (bind != elf.STB_GLOBAL && bind != elf.STB_WEAK):
		case s.Section == elf.SHN_UNDEF:
			importedSymbols = append(importedSymbols, s.Name)
		default:
			providedSymbols = append(providedSymbols, s.Name)
		}
	}

	return moduleName, providedSymbols, importedSymbols, nil
}

func roundTarToBlock(f *os.File, pos int64) error {
	const blockSize = 512
	if (pos % blockSize) != 0 {
		pos += blockSize - (pos % blockSize)
	}
	if err := f.Truncate(pos); err != nil {
		return errors.WithStack(err)
	}

	_, err := f.Seek(pos, io.SeekStart)
	return errors.WithStack(err)
}

func searchRPM(r io.Reader, fileName string) (io.Reader, int64, error) {
	rpm, err := rpmutils.ReadRpm(r)
	if err != nil {
		return nil, 0, errors.WithStack(err)
	}

	rpmReader, err := rpm.PayloadReaderExtended()
	if err != nil {
		return nil, 0, errors.WithStack(err)
	}

	var f rpmutils.FileInfo
	for {
		var err error
		f, err = rpmReader.Next()
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			return nil, 0, errors.Errorf("file %q not found in rpm", fileName)
		default:
			return nil, 0, errors.WithStack(err)
		}

		if filepath.Base(f.Name()) == fileName && !rpmReader.IsLink() {
			break
		}
	}

	return rpmReader, f.Size(), nil
}

func distrosBase() string {
	return filepath.Join(lo.Must(os.UserCacheDir()), "cloudless/distros")
}

func distroDir(config DistroConfig) (string, error) {
	configMarshalled, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", errors.WithStack(err)
	}
	configHash := sha256.Sum256(configMarshalled)
	return filepath.Join(distrosBase(), hex.EncodeToString(configHash[:])), nil
}

func kernelPath(distroDir string) string {
	return filepath.Join(distroDir, kernelFile)
}

func initramfsPath(distroDir string) string {
	return filepath.Join(distroDir, initramfsFile)
}
