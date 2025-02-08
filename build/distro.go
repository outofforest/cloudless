package build

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
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

	"github.com/outofforest/build/v2/pkg/types"
)

// https://packages.fedoraproject.org
const (
	//nolint:lll
	fedoraURL       = "https://github.com/fedora-cloud/docker-brew-fedora/raw/54e85723288471bb9dc81bc5cfed807635f93818/x86_64/fedora-20250119.tar"
	fedoraSHA256    = "3b8a25c27f4773557aee851f75beba17d910c968671c2771a105ce7c7a40e3ec"
	baseDistroPath  = "bin/distro/distro.base.tar"
	finalDistroPath = "bin/distro/distro.tar"

	//nolint:lll
	kernelCoreURL    = "https://kojipkgs.fedoraproject.org/packages/kernel/6.12.7/200.fc41/x86_64/kernel-core-6.12.7-200.fc41.x86_64.rpm"
	kernelSHA256     = "5cd46b0ba12275d811470c84a8d0fbfcda364d278d40be8a9d0ade2d9f396752"
	kernelFile       = "vmlinuz"
	kernelPath       = "bin/embed/vmlinuz"
	kernelTargetPath = "/boot/vmlinuz"

	modulePathPrefix = "/lib/modules/"
	moduleDir        = "bin/distro/modules"
	moduleTargetDir  = "/usr/lib/modules"
	depsFile         = "bin/distro/deps.json"
)

type modulePackage struct {
	URL    string
	SHA256 string
}

var modulePackages = []modulePackage{
	{
		URL:    "https://kojipkgs.fedoraproject.org/packages/kernel/6.12.7/200.fc41/x86_64/kernel-modules-core-6.12.7-200.fc41.x86_64.rpm", //nolint:lll
		SHA256: "791f222e27395c571319c93eb17cbf391bfbd8955557478e5301152834b3b662",
	},
	{
		URL:    "https://kojipkgs.fedoraproject.org/packages/kernel/6.12.7/200.fc41/x86_64/kernel-modules-6.12.7-200.fc41.x86_64.rpm", //nolint:lll
		SHA256: "d5d9603ec1bf97b01c30f98992dda9993b8a7cd4f885eb5b732064ec2f6b4936",
	},
}

var requiredModules = []string{
	"tun",
	"kvm-intel",
	"virtio-net",
	"vhost-net",
	"virtio-scsi",
	"bridge",
	"veth",
	"nft-masq",
	"nft-nat",
	"nft-fib-ipv4",
	"nft-ct",
	"nft-chain-nat",
}

func buildDistro(_ context.Context, deps types.DepsFunc) error {
	deps(downloadDistro, downloadKernel, downloadModules)

	baseInitramfsF, err := os.Open(baseDistroPath)
	if err != nil {
		return errors.WithStack(err)
	}
	defer baseInitramfsF.Close()

	finalInitramfsF, err := os.OpenFile(finalDistroPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.WithStack(err)
	}
	defer finalInitramfsF.Close()

	if _, err := io.Copy(finalInitramfsF, baseInitramfsF); err != nil {
		return errors.WithStack(err)
	}

	if _, err := finalInitramfsF.Seek(0, io.SeekStart); err != nil {
		return errors.WithStack(err)
	}

	tr := tar.NewReader(finalInitramfsF)

	var lastFileSize, lastStreamPos int64
loop:
	for {
		hdr, err := tr.Next()
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			break loop
		default:
			return errors.WithStack(err)
		}
		lastStreamPos, err = finalInitramfsF.Seek(0, io.SeekCurrent)
		if err != nil {
			return errors.WithStack(err)
		}
		lastFileSize = hdr.Size
	}

	const blockSize = 512
	newOffset := lastStreamPos + lastFileSize
	// shift to next-nearest block boundary (unless we are already on it)
	if (newOffset % blockSize) != 0 {
		newOffset += blockSize - (newOffset % blockSize)
	}
	if _, err := finalInitramfsF.Seek(newOffset, io.SeekStart); err != nil {
		return errors.WithStack(err)
	}

	tw := tar.NewWriter(finalInitramfsF)
	defer tw.Close()

	kernelF, err := os.Open(kernelPath)
	if err != nil {
		return errors.WithStack(err)
	}
	defer kernelF.Close()

	size, err := kernelF.Seek(0, io.SeekEnd)
	if err != nil {
		return errors.WithStack(err)
	}
	if _, err := kernelF.Seek(0, io.SeekStart); err != nil {
		return errors.WithStack(err)
	}

	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     kernelTargetPath,
		Size:     size,
		Mode:     0o500,
	}); err != nil {
		return errors.WithStack(err)
	}

	if _, err := io.Copy(tw, kernelF); err != nil {
		return errors.WithStack(err)
	}

	depF, err := os.Open(depsFile)
	if err != nil {
		return errors.WithStack(err)
	}
	defer depF.Close()

	depMod := map[string][]string{}
	if err := json.NewDecoder(depF).Decode(&depMod); err != nil {
		return errors.WithStack(err)
	}

	modules := map[string]struct{}{}
	for _, m := range requiredModules {
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
			return err
		}
	}

	size, err = depF.Seek(0, io.SeekEnd)
	if err != nil {
		return errors.WithStack(err)
	}
	if _, err := depF.Seek(0, io.SeekStart); err != nil {
		return errors.WithStack(err)
	}

	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     filepath.Join(moduleTargetDir, filepath.Base(depsFile)),
		Size:     size,
		Mode:     0o400,
	}); err != nil {
		return errors.WithStack(err)
	}
	_, err = io.Copy(tw, depF)
	return errors.WithStack(err)
}

func downloadDistro(ctx context.Context, _ types.DepsFunc) error {
	if err := os.MkdirAll(filepath.Dir(baseDistroPath), 0o700); err != nil {
		return errors.WithStack(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fedoraURL, nil)
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
	if checksum != fedoraSHA256 {
		return errors.Errorf("initramfs checksum mismatch, expected: %q, got: %q", fedoraSHA256, checksum)
	}

	return nil
}

func downloadKernel(ctx context.Context, _ types.DepsFunc) error {
	if err := os.MkdirAll(filepath.Dir(kernelPath), 0o700); err != nil {
		return errors.WithStack(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kernelCoreURL, nil)
	if err != nil {
		return errors.WithStack(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	rpm, err := rpmutils.ReadRpm(resp.Body)
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

		if filepath.Base(fInfo.Name()) != kernelFile || pReader.IsLink() {
			continue
		}

		vmlinuzF, err := os.OpenFile(kernelPath, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0o700)
		if err != nil {
			return errors.WithStack(err)
		}
		defer vmlinuzF.Close()

		hasher := sha256.New()

		if _, err := io.Copy(vmlinuzF, io.TeeReader(pReader, hasher)); err != nil {
			return errors.WithStack(err)
		}

		checksum := hex.EncodeToString(hasher.Sum(nil))
		if checksum != kernelSHA256 {
			return errors.Errorf("kernel checksum mismatch, expected: %q, got: %q", kernelSHA256, checksum)
		}

		return nil
	}
}

func downloadModules(ctx context.Context, _ types.DepsFunc) error {
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		return errors.WithStack(err)
	}

	providers := map[string]string{}
	requires := map[string][]string{}

	for _, m := range modulePackages {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
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
		if checksum != m.SHA256 {
			return errors.Errorf("rpm %q checksum mismatch, expected: %q, got: %q", m.URL, m.SHA256, checksum)
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
	stack := append([]string{}, requiredModules...)
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
