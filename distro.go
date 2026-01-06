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
	repoURL          = "http://mirror.slu.cz/fedora/linux/updates/43/Everything/x86_64/Packages/k/"
	configFile       = "config.json"
	efiFile          = "efi.tar.gz"
	baseDistroFile   = "distro.base.tar"
	distroFile       = "distro.tar"
	initramfsFile    = "initramfs"
	kernelFile       = "vmlinuz"
	moduleDir        = "modules"
	depsFile         = "deps.json"
	kernelTargetPath = "/boot/" + kernelFile
	modulePathPrefix = "/lib/modules/"
	moduleTargetDir  = "/usr/lib/modules"
)

//nolint:gocyclo
func buildDistro(ctx context.Context, config DistroConfig) (retConfigDir string, retErr error) {
	configMarshalled, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", errors.WithStack(err)
	}
	configHash := sha256.Sum256(configMarshalled)
	configDir := filepath.Join(lo.Must(os.UserCacheDir()), "cloudless/distros", hex.EncodeToString(configHash[:]))

	if _, err := os.Stat(configDir); err == nil {
		return configDir, nil
	}

	configDirTmp := configDir + ".tmp"
	initramfsPath := filepath.Join(configDirTmp, initramfsFile)

	logger.Get(ctx).Info("Building distro")

	if err := os.RemoveAll(configDirTmp); err != nil && !os.IsNotExist(err) {
		return "", errors.WithStack(err)
	}

	if err := os.MkdirAll(configDirTmp, 0o700); err != nil {
		return "", errors.WithStack(err)
	}

	defer func() {
		if retErr != nil {
			_ = os.RemoveAll(configDirTmp)
		}
	}()

	configBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", errors.WithStack(err)
	}
	configPath := filepath.Join(configDirTmp, configFile)

	if err := os.WriteFile(configPath, configBytes, 0o600); err != nil {
		return "", errors.WithStack(err)
	}

	efiPath := filepath.Join(configDirTmp, efiFile)
	if err := downloadEFI(ctx, config.EFI, efiPath); err != nil {
		return "", err
	}

	baseDistroPath := filepath.Join(configDirTmp, baseDistroFile)
	if err := downloadBase(ctx, config.Base, baseDistroPath); err != nil {
		return "", err
	}

	kernelPath := filepath.Join(configDirTmp, kernelFile)
	if err := downloadKernel(ctx, config.KernelPackage, kernelPath); err != nil {
		return "", err
	}

	moduleDir := filepath.Join(configDirTmp, moduleDir)
	depsPath := filepath.Join(configDirTmp, depsFile)
	if err := downloadModules(ctx, config.KernelModulePackages, config.KernelModules,
		moduleDir, depsPath); err != nil {
		return "", err
	}

	baseDistroF, err := os.Open(baseDistroPath)
	if err != nil {
		return "", errors.WithStack(err)
	}
	defer baseDistroF.Close()

	distroPath := filepath.Join(configDirTmp, distroFile)
	finalDistroF, err := os.OpenFile(distroPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", errors.WithStack(err)
	}
	defer finalDistroF.Close()

	if _, err := io.Copy(finalDistroF, baseDistroF); err != nil {
		return "", errors.WithStack(err)
	}

	if _, err := finalDistroF.Seek(0, io.SeekStart); err != nil {
		return "", errors.WithStack(err)
	}

	tr := tar.NewReader(finalDistroF)

	var lastFileSize, lastStreamPos int64
loop:
	for {
		hdr, err := tr.Next()
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			break loop
		default:
			return "", errors.WithStack(err)
		}
		lastStreamPos, err = finalDistroF.Seek(0, io.SeekCurrent)
		if err != nil {
			return "", errors.WithStack(err)
		}
		lastFileSize = hdr.Size
	}

	const blockSize = 512
	newOffset := lastStreamPos + lastFileSize
	// shift to next-nearest block boundary (unless we are already on it)
	if (newOffset % blockSize) != 0 {
		newOffset += blockSize - (newOffset % blockSize)
	}
	if _, err := finalDistroF.Seek(newOffset, io.SeekStart); err != nil {
		return "", errors.WithStack(err)
	}

	tw := tar.NewWriter(finalDistroF)
	defer tw.Close()

	kernelF, err := os.Open(kernelPath)
	if err != nil {
		return "", errors.WithStack(err)
	}
	defer kernelF.Close()

	size, err := kernelF.Seek(0, io.SeekEnd)
	if err != nil {
		return "", errors.WithStack(err)
	}
	if _, err := kernelF.Seek(0, io.SeekStart); err != nil {
		return "", errors.WithStack(err)
	}

	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     kernelTargetPath,
		Size:     size,
		Mode:     0o500,
	}); err != nil {
		return "", errors.WithStack(err)
	}

	if _, err := io.Copy(tw, kernelF); err != nil {
		return "", errors.WithStack(err)
	}

	depF, err := os.Open(depsPath)
	if err != nil {
		return "", errors.WithStack(err)
	}
	defer depF.Close()

	depMod := map[string][]string{}
	if err := json.NewDecoder(depF).Decode(&depMod); err != nil {
		return "", errors.WithStack(err)
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
		if err := writeModule(mName, tw, moduleDir); err != nil {
			return "", err
		}
	}

	size, err = depF.Seek(0, io.SeekEnd)
	if err != nil {
		return "", errors.WithStack(err)
	}
	if _, err := depF.Seek(0, io.SeekStart); err != nil {
		return "", errors.WithStack(err)
	}

	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     filepath.Join(moduleTargetDir, depsFile),
		Size:     size,
		Mode:     0o400,
	}); err != nil {
		return "", errors.WithStack(err)
	}
	if _, err := io.Copy(tw, depF); err != nil {
		return "", errors.WithStack(err)
	}

	initramfsF, err := os.OpenFile(initramfsPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", errors.WithStack(err)
	}
	defer initramfsF.Close()

	cW := gzip.NewWriter(initramfsF)
	defer cW.Close()

	w := cpio.NewWriter(cW)
	defer w.Close()

	if err := addFile(w, 0o600, distroPath); err != nil {
		return "", err
	}

	if err := os.Rename(configDirTmp, configDir); err != nil {
		return "", errors.WithStack(err)
	}

	return configDir, nil
}

func downloadEFI(ctx context.Context, efi EFI, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errors.WithStack(err)
	}

	efiURL := fmt.Sprintf(efiURL, efi.Version)

	log := logger.Get(ctx)
	log.Info("Downloading EFI", zap.String("url", efiURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, efiURL, nil)
	if err != nil {
		return errors.WithStack(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	efiF, err := os.OpenFile(path, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return errors.WithStack(err)
	}
	defer efiF.Close()

	reader, err := archive.NewHashingReader(resp.Body, efi.Hash)
	if err != nil {
		return errors.Wrapf(err, "creating hasher failed for efi %q", efiURL)
	}

	if _, err := io.Copy(efiF, reader); err != nil {
		return errors.WithStack(err)
	}

	if err := reader.ValidateChecksum(); err != nil {
		return errors.Wrap(err, "efi checksum mismatch")
	}

	return nil
}

func downloadBase(ctx context.Context, base Base, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errors.WithStack(err)
	}

	log := logger.Get(ctx)
	log.Info("Downloading distro base", zap.String("url", base.URL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.URL, nil)
	if err != nil {
		return errors.WithStack(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	initramfsF, err := os.OpenFile(path, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return errors.WithStack(err)
	}
	defer initramfsF.Close()

	reader, err := archive.NewHashingReader(resp.Body, base.Hash)
	if err != nil {
		return errors.Wrapf(err, "creating hasher failed for distro base %q", base.URL)
	}

	// Fedora .tar files are .tar.gz in reality.
	gr, err := gzip.NewReader(reader)
	if err != nil {
		return errors.WithStack(err)
	}
	defer gr.Close()

	if _, err := io.Copy(initramfsF, gr); err != nil {
		return errors.WithStack(err)
	}

	if err := reader.ValidateChecksum(); err != nil {
		return errors.Wrap(err, "distro base checksum mismatch")
	}

	return nil
}

func downloadKernel(ctx context.Context, kernelPackage Package, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errors.WithStack(err)
	}

	kernelPackageURL := packageURL(kernelPackage)

	log := logger.Get(ctx)
	log.Info("Downloading kernel module", zap.String("url", kernelPackageURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kernelPackageURL, nil)
	if err != nil {
		return errors.WithStack(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	reader, err := archive.NewHashingReader(resp.Body, kernelPackage.Hash)
	if err != nil {
		return errors.Wrapf(err, "creating hasher failed for package %q", kernelPackage.Name)
	}

	rpm, err := rpmutils.ReadRpm(reader)
	if err != nil {
		return errors.Wrapf(err, "failed reading RPM package %s", kernelPackageURL)
	}

	pReader, err := rpm.PayloadReaderExtended()
	if err != nil {
		return errors.WithStack(err)
	}

	for {
		fInfo, err := pReader.Next()
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			return errors.New("kernel not found in rpm")
		default:
			return errors.WithStack(err)
		}

		if filepath.Base(fInfo.Name()) == kernelFile && !pReader.IsLink() {
			break
		}
	}

	vmlinuzF, err := os.OpenFile(path, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0o700)
	if err != nil {
		return errors.WithStack(err)
	}
	defer vmlinuzF.Close()

	if _, err := io.Copy(vmlinuzF, pReader); err != nil {
		return errors.WithStack(err)
	}

	if err := reader.ValidateChecksum(); err != nil {
		return errors.Wrapf(err, "rpm checksum mismatch, url: %q", kernelPackageURL)
	}

	return nil
}

func downloadModules(ctx context.Context, packages []Package, modules []string, moduleDir, depsFile string) error {
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
	pkg Package,
	moduleDir string,
	providers map[string]string,
	requires map[string][]string,
) error {
	mURL := packageURL(pkg)

	log := logger.Get(ctx)
	log.Info("Downloading module", zap.String("url", mURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mURL, nil)
	if err != nil {
		return errors.WithStack(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	reader, err := archive.NewHashingReader(resp.Body, pkg.Hash)
	if err != nil {
		return errors.Wrapf(err, "creating hasher failed for package %q", pkg.Name)
	}

	rpm, err := rpmutils.ReadRpm(reader)
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

		moduleName, providedSymbols, importedSymbols, err := storeModule(fileName, pReader, moduleDir)
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

	if err := reader.ValidateChecksum(); err != nil {
		return errors.Wrapf(err, "rpm checksum mismatch, url: %q", mURL)
	}

	return nil
}

func storeModule(fileName string, r io.Reader, moduleDir string) (string, []string, []string, error) {
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

func writeModule(name string, tw *tar.Writer, moduleDir string) error {
	fileName := name + ".ko"
	dstPath := filepath.Join(moduleTargetDir, fileName)

	mf, err := os.Open(filepath.Join(moduleDir, fileName))
	if err != nil {
		return errors.WithStack(err)
	}
	defer mf.Close()

	size, err := mf.Seek(0, io.SeekEnd)
	if err != nil {
		return errors.WithStack(err)
	}
	if _, err := mf.Seek(0, io.SeekStart); err != nil {
		return errors.WithStack(err)
	}

	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     dstPath,
		Size:     size,
		Mode:     0o400,
	}); err != nil {
		return errors.WithStack(err)
	}

	_, err = io.Copy(tw, mf)
	return errors.WithStack(err)
}

func packageURL(p Package) string {
	return repoURL + p.Name + "-" + p.Version + ".rpm"
}
