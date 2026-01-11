package container

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/container/cache"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/kernel"
	"github.com/outofforest/cloudless/pkg/parse"
	"github.com/outofforest/cloudless/pkg/retry"
	"github.com/outofforest/cloudless/pkg/wait"
	"github.com/outofforest/libexec"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

const containersDir = cloudless.BaseDir + "/containers"

var protectedFiles = map[string]struct{}{
	"/etc/resolv.conf":  {},
	"/etc/hosts":        {},
	"/etc/hostname":     {},
	"/etc/ssl/cert.pem": {},
}

// Config represents container configuration.
type Config struct {
	Name     string
	Networks []NetworkConfig
}

// NetworkConfig represents container's network configuration.
type NetworkConfig struct {
	BridgeName    string
	InterfaceName string
	MAC           net.HardwareAddr
}

// Configurator defines function setting the container configuration.
type Configurator func(config *Config)

// RunImageConfig represents container image execution configuration.
type RunImageConfig struct {
	// EnvVars sets environment variables inside container.
	EnvVars map[string]string

	// WorkingDir specifies a path to working directory.
	WorkingDir string

	// Entrypoint sets entrypoint for container.
	Entrypoint []string

	// Cmd sets command to execute inside container.
	Cmd []string
}

// RunImageConfigurator defines function setting the container image execution configuration.
type RunImageConfigurator func(config *RunImageConfig)

// New creates container.
func New(name string, configurators ...Configurator) host.Configurator {
	config := Config{
		Name: name,
	}

	for _, configurator := range configurators {
		configurator(&config)
	}

	return cloudless.Join(
		cloudless.KernelModules(kernel.Module{Name: "veth"}),
		cloudless.Service("container-"+name, parallel.Fail, func(ctx context.Context) error {
			cmd, stdInCloser, err := command(ctx, config)
			if err != nil {
				return err
			}
			if err := cmd.Start(); err != nil {
				return errors.WithStack(err)
			}

			if err := joinNetworks(cmd.Process.Pid, config); err != nil {
				return err
			}

			if err := stdInCloser.Close(); err != nil {
				return errors.WithStack(err)
			}

			return errors.WithStack(cmd.Wait())
		}),
	)
}

// Network adds network to the config.
func Network(bridgeName, ifaceName, mac string) Configurator {
	return func(c *Config) {
		c.Networks = append(c.Networks, NetworkConfig{
			BridgeName:    bridgeName,
			InterfaceName: ifaceName,
			MAC:           parse.MAC(mac),
		})
	}
}

// InstallImage installs image.
func InstallImage(imageTag string) host.Configurator {
	var c host.SealedConfiguration

	return cloudless.Join(
		cloudless.Configuration(&c),
		cloudless.RequireContainers(imageTag),
		cloudless.IsContainer(),
		cloudless.Prune(prune(imageTag)),
		cloudless.Prepare(func(ctx context.Context) error {
			_, err := installImage(ctx, imageTag, c.ContainerMirrors())
			return err
		}),
	)
}

// RunImage runs image.
func RunImage(imageTag string, configurators ...RunImageConfigurator) host.Configurator {
	var c host.SealedConfiguration

	return cloudless.Join(
		cloudless.Configuration(&c),
		cloudless.RequireContainers(imageTag),
		cloudless.IsContainer(),
		cloudless.Prune(prune(imageTag)),
		cloudless.Service("containerImage", parallel.Fail, func(ctx context.Context) error {
			log := logger.Get(ctx)

			ic, err := installImage(ctx, imageTag, c.ContainerMirrors())
			if err != nil {
				return err
			}

			config := RunImageConfig{
				Entrypoint: ic.Config.Entrypoint,
				Cmd:        ic.Config.Cmd,
				WorkingDir: ic.Config.WorkingDir,
				EnvVars:    map[string]string{},
			}

			for _, ev := range ic.Config.Env {
				pos := strings.Index(ev, "=")
				if pos < 0 {
					continue
				}

				evName := strings.TrimSpace(ev[:pos])
				if evName == "" {
					continue
				}
				evValue := strings.TrimSpace(ev[pos+1:])
				if evValue == "" {
					delete(config.EnvVars, evName)
					continue
				}

				config.EnvVars[evName] = evValue
			}

			for _, configurator := range configurators {
				configurator(&config)
			}

			args := append(append([]string{}, config.Entrypoint...), config.Cmd...)
			if len(args) == 0 {
				return errors.Errorf("no command specified")
			}
			envVars := make([]string, 0, len(config.EnvVars))
			for k, v := range config.EnvVars {
				envVars = append(envVars, fmt.Sprintf("%s=%s", k, v))
			}

			stdoutLogger := newStreamLogger(log)
			stderrLogger := newStreamLogger(log)
			for {
				err := libexec.Exec(ctx, &exec.Cmd{
					Path:   args[0],
					Args:   args,
					Env:    envVars,
					Dir:    config.WorkingDir,
					Stdout: stdoutLogger,
					Stderr: stderrLogger,
				})
				if ctx.Err() != nil {
					return errors.WithStack(ctx.Err())
				}
				if err != nil {
					log.Error("Container failed", zap.Error(err))
				}
			}
		}),
	)
}

// EnvVar sets environment variable inside container.
func EnvVar(name, value string) RunImageConfigurator {
	return func(config *RunImageConfig) {
		config.EnvVars[name] = value
	}
}

// WorkingDir sets working directory inside container.
func WorkingDir(workingDir string) RunImageConfigurator {
	return func(config *RunImageConfig) {
		config.WorkingDir = workingDir
	}
}

// Entrypoint sets container's entrypoint.
func Entrypoint(entrypoint ...string) RunImageConfigurator {
	return func(config *RunImageConfig) {
		config.Entrypoint = entrypoint
	}
}

// Cmd sets command to execute inside container.
func Cmd(args ...string) RunImageConfigurator {
	return func(config *RunImageConfig) {
		config.Cmd = args
	}
}

// AppMount returns docker volume definition for app's directory.
func AppMount(appName string) host.Configurator {
	appDir := cloudless.AppDir(appName)
	return cloudless.Mount(appDir, appDir, true)
}

func command(ctx context.Context, config Config) (*exec.Cmd, io.Closer, error) {
	containerDir := filepath.Join(containersDir, config.Name)

	if err := os.MkdirAll(containerDir, 0o700); err != nil {
		return nil, nil, errors.WithStack(err)
	}

	pipeReader, pipeWriter := io.Pipe()

	cmd := exec.CommandContext(ctx, "/proc/self/exe")
	cmd.Dir = containerDir
	cmd.Stdin = pipeReader
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{host.ContainerEnvVar + "=" + config.Name}
	cmd.SysProcAttr = &unix.SysProcAttr{
		Setsid:    true,
		Pdeathsig: unix.SIGKILL,
		Cloneflags: unix.CLONE_NEWPID |
			unix.CLONE_NEWNS |
			unix.CLONE_NEWUSER |
			unix.CLONE_NEWIPC |
			unix.CLONE_NEWUTS |
			unix.CLONE_NEWCGROUP |
			unix.CLONE_NEWNET,
		AmbientCaps: []uintptr{
			unix.CAP_SYS_ADMIN, // by adding CAP_SYS_ADMIN executor may mount /proc
		},
		UidMappings: []syscall.SysProcIDMap{
			{
				HostID:      0,
				ContainerID: 0,
				Size:        65535,
			},
		},
		GidMappingsEnableSetgroups: true,
		GidMappings: []syscall.SysProcIDMap{
			{
				HostID:      0,
				ContainerID: 0,
				Size:        65535,
			},
		},
	}
	cmd.Cancel = func() error {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Process.Signal(syscall.SIGINT)
		return nil
	}

	return cmd, pipeWriter, nil
}

func joinNetworks(pid int, config Config) error {
	for _, n := range config.Networks {
		link, err := netlink.LinkByName(n.BridgeName)
		if err != nil {
			return errors.WithStack(err)
		}
		bridgeLink, ok := link.(*netlink.Bridge)
		if !ok {
			return errors.New("link is not a bridge")
		}

		vethHost := &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name: n.InterfaceName,
			},
			PeerName:         n.InterfaceName + "1",
			PeerHardwareAddr: n.MAC,
		}

		if err := netlink.LinkAdd(vethHost); err != nil {
			return errors.WithStack(err)
		}

		if err := netlink.LinkSetUp(vethHost); err != nil {
			return errors.WithStack(err)
		}

		if err := netlink.LinkSetMaster(vethHost, bridgeLink); err != nil {
			return errors.WithStack(err)
		}

		vethContainer, err := netlink.LinkByName(vethHost.PeerName)
		if err != nil {
			return errors.WithStack(err)
		}

		if err := netlink.LinkSetNsPid(vethContainer, pid); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func fetchManifest(ctx context.Context, imageTag string, mirrors []string) (cache.Manifest, error) {
	manifestFile, err := cache.ManifestFile(imageTag)
	if err != nil {
		return cache.Manifest{}, err
	}

	var m cache.Manifest
	if err := retry.Do(ctx, retry.FixedConfig{RetryAfter: 5 * time.Second, MaxAttempts: 10}, func() error {
		mirror, err := selectMirror(mirrors)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, mirror+"/"+manifestFile, nil)
		if err != nil {
			return errors.WithStack(err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return retry.Retriable(errors.WithStack(err))
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return retry.Retriable(errors.Errorf("unexpected status code %d", resp.StatusCode))
		}

		return retry.Retriable(json.NewDecoder(resp.Body).Decode(&m))
	}); err != nil {
		return cache.Manifest{}, err
	}

	return m, nil
}

func fetchConfig(ctx context.Context, imageTag string, m cache.Manifest, mirrors []string) (imageConfig, error) {
	blobFile, err := cache.BlobFile(imageTag, m.Config.Digest)
	if err != nil {
		return imageConfig{}, err
	}

	var ic imageConfig
	if err := retry.Do(ctx, retry.FixedConfig{RetryAfter: 5 * time.Second, MaxAttempts: 10}, func() error {
		mirror, err := selectMirror(mirrors)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, mirror+"/"+blobFile, nil)
		if err != nil {
			return errors.WithStack(err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return retry.Retriable(errors.WithStack(err))
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return retry.Retriable(errors.Errorf("unexpected status code %d", resp.StatusCode))
		}

		return retry.Retriable(json.NewDecoder(resp.Body).Decode(&ic))
	}); err != nil {
		return imageConfig{}, err
	}

	return ic, nil
}

func icFileName(imageTag string) string {
	return strings.ReplaceAll(imageTag, "/", "-")
}

func prune(imageTag string) host.PruneFn {
	return func() (bool, error) {
		_, err := os.Stat(icFileName(imageTag))
		switch {
		case err == nil:
			return false, nil
		case os.IsNotExist(err):
			return true, nil
		default:
			return false, errors.WithStack(err)
		}
	}
}

func installImage(ctx context.Context, imageTag string, mirrors []string) (imageConfig, error) {
	icFileName := icFileName(imageTag)

	var ic imageConfig
	icRaw, err := os.ReadFile(icFileName)
	switch {
	case err == nil:
		if err := json.Unmarshal(icRaw, &ic); err != nil {
			return imageConfig{}, errors.WithStack(err)
		}
	case !os.IsNotExist(err):
		return imageConfig{}, errors.WithStack(err)
	default:
		if err := wait.HTTP(ctx, mirrors...); err != nil {
			return imageConfig{}, err
		}

		m, err := fetchManifest(ctx, imageTag, mirrors)
		if err != nil {
			return imageConfig{}, err
		}

		ic, err = fetchConfig(ctx, imageTag, m, mirrors)
		if err != nil {
			return imageConfig{}, err
		}

		if err := inflateImage(ctx, imageTag, m, mirrors); err != nil {
			return imageConfig{}, err
		}

		if icRaw, err = json.Marshal(ic); err != nil {
			return imageConfig{}, errors.WithStack(err)
		}

		icFileNameTmp := icFileName + ".tmp"
		if err := os.WriteFile(icFileNameTmp, icRaw, 0o600); err != nil {
			return imageConfig{}, errors.WithStack(err)
		}
		if err := os.Rename(icFileNameTmp, icFileName); err != nil {
			return imageConfig{}, errors.WithStack(err)
		}
	}

	return ic, nil
}

func inflateImage(ctx context.Context, imageTag string, m cache.Manifest, mirrors []string) error {
	for _, layer := range m.Layers {
		blobFile, err := cache.BlobFile(imageTag, layer.Digest)
		if err != nil {
			return err
		}

		if err := retry.Do(ctx, retry.FixedConfig{RetryAfter: 5 * time.Second, MaxAttempts: 10}, func() error {
			mirror, err := selectMirror(mirrors)
			if err != nil {
				return err
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, mirror+"/"+blobFile, nil)
			if err != nil {
				return errors.WithStack(err)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return retry.Retriable(errors.WithStack(err))
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return retry.Retriable(errors.Errorf("unexpected status code %d", resp.StatusCode))
			}

			return inflateBlob(resp.Body)
		}); err != nil {
			return err
		}
	}

	return nil
}

//nolint:gocyclo
func inflateBlob(r io.Reader) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return errors.WithStack(err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	del := map[string]bool{}
	added := map[string]bool{}
loop:
	for {
		header, err := tr.Next()
		switch {
		case errors.Is(err, io.EOF):
			break loop
		case err != nil:
			return retry.Retriable(err)
		case header == nil:
			continue
		}

		absPath, err := filepath.Abs(header.Name)
		if err != nil {
			return errors.WithStack(err)
		}
		if _, exists := protectedFiles[absPath]; exists {
			continue
		}

		// We take mode from header.FileInfo().Mode(), not from header.Mode because they may be in different formats
		// (meaning of bits may be different). header.FileInfo().Mode() returns compatible value.
		mode := header.FileInfo().Mode()

		switch {
		case filepath.Base(header.Name) == ".wh..wh..plnk":
			// just ignore this
			continue
		case filepath.Base(header.Name) == ".wh..wh..opq":
			// It means that content in this directory created by earlier layers should not be visible,
			// so content created earlier must be deleted.
			dir := filepath.Dir(header.Name)
			files, err := os.ReadDir(dir)
			if err != nil {
				return errors.WithStack(err)
			}
			for _, f := range files {
				toDelete := filepath.Join(dir, f.Name())
				if added[toDelete] {
					continue
				}
				if err := os.RemoveAll(toDelete); err != nil {
					return errors.WithStack(err)
				}
			}
			continue
		case strings.HasPrefix(filepath.Base(header.Name), ".wh."):
			// delete or mark to delete corresponding file
			toDelete := filepath.Join(filepath.Dir(header.Name), strings.TrimPrefix(filepath.Base(header.Name), ".wh."))
			delete(added, toDelete)
			if err := os.RemoveAll(toDelete); err != nil {
				if os.IsNotExist(err) {
					del[toDelete] = true
					continue
				}
				return errors.WithStack(err)
			}
			continue
		case del[header.Name]:
			delete(del, header.Name)
			delete(added, header.Name)
			continue
		case header.Typeflag == tar.TypeDir:
			if err := os.MkdirAll(header.Name, mode); err != nil {
				return errors.WithStack(err)
			}
		case header.Typeflag == tar.TypeReg:
			f, err := os.OpenFile(header.Name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return errors.WithStack(err)
			}
			_, err = io.Copy(f, tr)
			_ = f.Close()
			if err != nil {
				return errors.WithStack(err)
			}
		case header.Typeflag == tar.TypeSymlink:
			if err := os.Symlink(header.Linkname, header.Name); err != nil {
				return errors.WithStack(err)
			}
		case header.Typeflag == tar.TypeLink:
			// linked file may not exist yet, so let's create it - it will be overwritten later
			if err := os.MkdirAll(filepath.Dir(header.Linkname), 0o700); err != nil {
				return errors.WithStack(err)
			}
			f, err := os.OpenFile(header.Linkname, os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				if !os.IsExist(err) {
					return errors.WithStack(err)
				}
			} else {
				_ = f.Close()
			}
			if err := os.Link(header.Linkname, header.Name); err != nil {
				return errors.WithStack(err)
			}
		default:
			return errors.Errorf("unsupported file type: %d", header.Typeflag)
		}

		added[header.Name] = true
		if err := os.Lchown(header.Name, header.Uid, header.Gid); err != nil {
			return errors.WithStack(err)
		}

		// Unless CAP_FSETID capability is set for the process every operation modifying the file/dir will reset
		// setuid, setgid nd sticky bits. After saving those files/dirs the mode has to be set once again to set those
		// bits. This has to be the last operation on the file/dir.
		// On linux mode is not supported for symlinks, mode is always taken from target location.
		if header.Typeflag != tar.TypeSymlink {
			if err := os.Chmod(header.Name, mode); err != nil {
				return errors.WithStack(err)
			}
		}
	}
	return nil
}

func selectMirror(mirrors []string) (string, error) {
	if len(mirrors) == 0 {
		return "", errors.New("there are no mirrors")
	}
	return mirrors[rand.Intn(len(mirrors))], nil
}

type imageConfig struct {
	Config struct {
		Env        []string
		Entrypoint []string
		Cmd        []string
		WorkingDir string
	} `json:"config"`
}
