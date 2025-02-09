package build

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

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/sassoftware/go-rpmutils"
	"github.com/ulikunitz/xz"
)

const (
	baseDistroPath  = "bin/distro/distro.base.tar"
	finalDistroPath = "bin/distro/distro.tar"

	kernelFile       = "vmlinuz"
	kernelPath       = "bin/embed/vmlinuz"
	kernelTargetPath = "/boot/vmlinuz"

	modulePathPrefix = "/lib/modules/"
	moduleDir        = "bin/distro/modules"
	moduleTargetDir  = "/usr/lib/modules"
	depsFile         = "bin/distro/deps.json"
)

//nolint:gocyclo
func buildDistro(ctx context.Context, config Config) (string, error) {
	if err := downloadDistro(ctx, config.Base); err != nil {
		return "", err
	}
	if err := downloadKernel(ctx, config.KernelPackage); err != nil {
		return "", err
	}
	if err := downloadModules(ctx, config.KernelModulePackages, config.KernelModules); err != nil {
		return "", err
	}

	baseDistroF, err := os.Open(baseDistroPath)
	if err != nil {
		return "", errors.WithStack(err)
	}
	defer baseDistroF.Close()

	finalDistroF, err := os.OpenFile(finalDistroPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
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

	depF, err := os.Open(depsFile)
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
		if err := writeModule(mName, tw); err != nil {
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
		Name:     filepath.Join(moduleTargetDir, filepath.Base(depsFile)),
		Size:     size,
		Mode:     0o400,
	}); err != nil {
		return "", errors.WithStack(err)
	}
	if _, err := io.Copy(tw, depF); err != nil {
		return "", errors.WithStack(err)
	}

	return finalDistroPath, nil
}

func downloadDistro(ctx context.Context, distro Base) error {
	if err := os.MkdirAll(filepath.Dir(baseDistroPath), 0o700); err != nil {
		return errors.WithStack(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, distro.URL, nil)
	if err != nil {
		return errors.WithStack(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	initramfsF, err := os.OpenFile(baseDistroPath, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return errors.WithStack(err)
	}
	defer initramfsF.Close()

	hasher := sha256.New()

	// Fedora .tar files are .tar.gz in reality.
	gr, err := gzip.NewReader(io.TeeReader(resp.Body, hasher))
	if err != nil {
		return errors.WithStack(err)
	}
	defer gr.Close()

	if _, err := io.Copy(initramfsF, gr); err != nil {
		return errors.WithStack(err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	if checksum != distro.SHA256 {
		return errors.Errorf("distro base checksum mismatch, expected: %q, got: %q", distro.SHA256, checksum)
	}

	return nil
}

func downloadKernel(ctx context.Context, kernelPackage Package) error {
	if err := os.MkdirAll(filepath.Dir(kernelPath), 0o700); err != nil {
		return errors.WithStack(err)
	}

	kernelPackageURL := packageURL(kernelPackage)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kernelPackageURL, nil)
	if err != nil {
		return errors.WithStack(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	hasher := sha256.New()

	rpm, err := rpmutils.ReadRpm(io.TeeReader(resp.Body, hasher))
	if err != nil {
		return errors.WithStack(err)
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

	vmlinuzF, err := os.OpenFile(kernelPath, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0o700)
	if err != nil {
		return errors.WithStack(err)
	}
	defer vmlinuzF.Close()

	if _, err := io.Copy(vmlinuzF, pReader); err != nil {
		return errors.WithStack(err)
	}

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return errors.WithStack(err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	if checksum != kernelPackage.SHA256 {
		return errors.Errorf("rpm %q checksum mismatch, expected: %q, got: %q", kernelPackageURL,
			kernelPackage.SHA256, checksum)
	}

	return nil
}

func downloadModules(ctx context.Context, packages []Package, modules []string) error {
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		return errors.WithStack(err)
	}

	providers := map[string]string{}
	requires := map[string][]string{}

	for _, p := range packages {
		mURL := packageURL(p)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, mURL, nil)
		if err != nil {
			return errors.WithStack(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return errors.WithStack(err)
		}
		defer resp.Body.Close()

		hasher := sha256.New()

		rpm, err := rpmutils.ReadRpm(io.TeeReader(resp.Body, hasher))
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

			moduleName, providedSymbols, importedSymbols, err := storeModule(fileName, pReader)
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

		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return errors.WithStack(err)
		}

		checksum := hex.EncodeToString(hasher.Sum(nil))
		if checksum != p.SHA256 {
			return errors.Errorf("rpm %q checksum mismatch, expected: %q, got: %q", mURL, p.SHA256, checksum)
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

	return errors.WithStack(os.WriteFile(depsFile, lo.Must(json.Marshal(finalDependencies)), 0o600))
}

func storeModule(fileName string, r io.Reader) (string, []string, []string, error) {
	xr, err := xz.NewReader(r)
	if err != nil {
		return "", nil, nil, errors.WithStack(err)
	}

	fileName = strings.ReplaceAll(strings.TrimSuffix(fileName, ".xz"), "_", "-")
	moduleName := strings.TrimSuffix(fileName, ".ko")
	modF, err := os.OpenFile(filepath.Join(moduleDir, fileName), os.O_TRUNC|os.O_RDWR|os.O_CREATE, 0o600)
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

func writeModule(name string, tw *tar.Writer) error {
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
	category := p.Name
	if pos := strings.Index(category, "-"); pos >= 0 {
		category = category[:pos]
	}
	versionParts := strings.Split(p.Version, "-")
	version := versionParts[0]
	props := strings.Split(versionParts[1], ".")

	return fmt.Sprintf(
		"https://kojipkgs.fedoraproject.org/packages/%[1]s/%[3]s/%[4]s.%[5]s/%[6]s/%[2]s-%[3]s-%[4]s.%[5]s.%[6]s.rpm",
		category, p.Name, version, props[0], props[1], props[2],
	)
}
