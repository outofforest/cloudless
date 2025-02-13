package mount

import (
	"archive/tar"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/pkg/errors"
)

const caCertFile = "etc/ssl/cert.pem"

// ProcFS mounts procfs.
func ProcFS(dir string) error {
	if err := os.MkdirAll(dir, 0o555); err != nil {
		return errors.WithStack(err)
	}
	return errors.WithStack(syscall.Mount("none", dir, "proc", 0, ""))
}

// DevFS mounts devfs.
func DevFS(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.WithStack(err)
	}
	return errors.WithStack(syscall.Mount("none", dir, "devtmpfs", 0, "size=4m"))
}

// DevPtsFS mounts devpts.
func DevPtsFS(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.WithStack(err)
	}
	return errors.WithStack(syscall.Mount("none", dir, "devpts", 0, ""))
}

// HugeTlbFs mounts hugetlbfs.
func HugeTlbFs(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.WithStack(err)
	}
	return errors.WithStack(syscall.Mount("none", dir, "hugetlbfs", 0, ""))
}

// SysFS mounts sysfs.
func SysFS(dir string) error {
	if err := os.MkdirAll(dir, 0o555); err != nil {
		return errors.WithStack(err)
	}
	return errors.WithStack(syscall.Mount("none", dir, "sysfs", 0, ""))
}

// TmpFS mounts tmpfs.
func TmpFS(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.WithStack(err)
	}
	return errors.WithStack(syscall.Mount("none", dir, "tmpfs", 0, ""))
}

// HostRoot mounts host root filesystem.
func HostRoot() error {
	if err := TmpFS("/newroot"); err != nil {
		return err
	}

	if err := os.Chdir("/newroot"); err != nil {
		return errors.WithStack(err)
	}

	if err := os.MkdirAll("/newroot/oldroot", 0o700); err != nil {
		return errors.WithStack(err)
	}
	if err := syscall.Mount("/", "/newroot/oldroot", "", syscall.MS_BIND|syscall.MS_SLAVE, ""); err != nil {
		return errors.WithStack(err)
	}
	if err := syscall.Mount("/newroot", "/", "", syscall.MS_MOVE, ""); err != nil {
		return errors.WithStack(err)
	}
	if err := syscall.Chroot("."); err != nil {
		return errors.WithStack(err)
	}

	if err := untarDistro(); err != nil {
		return err
	}

	if err := os.MkdirAll("/root", 0o750); err != nil {
		return errors.WithStack(err)
	}
	if err := os.Chmod("/root", 0o750); err != nil {
		return errors.WithStack(err)
	}

	if err := ProcFS("/proc"); err != nil {
		return err
	}
	if err := SysFS("/sys"); err != nil {
		return err
	}
	if err := DevFS("/dev"); err != nil {
		return err
	}
	if err := DevPtsFS("/dev/pts"); err != nil {
		return err
	}
	return HugeTlbFs("/dev/hugepages")
}

// ContainerRootPrepare prepares container root directory.
func ContainerRootPrepare() error {
	// systemd remounts everything as MS_SHARED, to prevent mess let's remount everything back to
	// MS_PRIVATE inside namespace
	if err := syscall.Mount("", "/", "", syscall.MS_SLAVE|syscall.MS_REC, ""); err != nil {
		return errors.WithStack(err)
	}

	if err := os.MkdirAll("root/.old", 0o700); err != nil {
		return errors.WithStack(err)
	}

	// PivotRoot requires new root to be on different mountpoint, so let's bind it to itself
	if err := syscall.Mount(".", ".", "", syscall.MS_BIND|syscall.MS_PRIVATE, ""); err != nil {
		return errors.WithStack(err)
	}
	if err := syscall.Mount("root", "root", "", syscall.MS_BIND|syscall.MS_PRIVATE, ""); err != nil {
		return errors.WithStack(err)
	}
	return errors.WithStack(os.Chdir("root"))
}

// ContainerRoot mounts container root filesystem.
func ContainerRoot() error {
	if err := ProcFS("proc"); err != nil {
		return err
	}
	if err := populateDev(); err != nil {
		return err
	}
	if err := storeRootCertificates(); err != nil {
		return err
	}
	return pivotRoot()
}

func untarDistro() error {
	f, err := os.Open("/oldroot/distro.tar")
	if err != nil {
		return errors.WithStack(err)
	}
	defer f.Close()

	return untar(f)
}

func untar(reader io.Reader) error {
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return errors.WithStack(err)
		case header == nil:
			continue
		}

		// We take mode from header.FileInfo().Mode(), not from header.Mode because they may be in
		// different formats (meaning of bits may be different).
		// header.FileInfo().Mode() returns compatible value.
		mode := header.FileInfo().Mode()
		header.Name = strings.TrimPrefix(header.Name, "./")

		switch {
		case header.Name == "":
			continue
		case header.Typeflag == tar.TypeDir:
			if err := os.MkdirAll(header.Name, mode); err != nil {
				return errors.WithStack(err)
			}
		case header.Typeflag == tar.TypeReg:
			if err := ensureDir(header.Name); err != nil {
				return err
			}

			f, err := os.OpenFile(header.Name, os.O_CREATE|os.O_WRONLY, mode)
			if err != nil {
				return errors.WithStack(err)
			}
			_, err = io.Copy(f, tr)
			_ = f.Close()
			if err != nil {
				return errors.WithStack(err)
			}
		case header.Typeflag == tar.TypeSymlink:
			if err := ensureDir(header.Name); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, header.Name); err != nil {
				return errors.WithStack(err)
			}
		case header.Typeflag == tar.TypeLink:
			header.Linkname = strings.TrimPrefix(header.Linkname, "./")
			if err := ensureDir(header.Name); err != nil {
				return err
			}
			if err := ensureDir(header.Linkname); err != nil {
				return err
			}
			// linked file may not exist yet, so let's create it - it will be overwritten later
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
	}
}

func ensureDir(file string) error {
	return errors.WithStack(os.MkdirAll(filepath.Dir(file), 0o700))
}

func pivotRoot() error {
	if err := syscall.PivotRoot(".", ".old"); err != nil {
		return errors.WithStack(err)
	}
	if err := os.Chdir("/"); err != nil {
		return errors.WithStack(err)
	}
	if err := syscall.Mount("", ".old", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		return errors.WithStack(err)
	}
	if err := syscall.Unmount(".old", syscall.MNT_DETACH); err != nil {
		return errors.WithStack(err)
	}
	return errors.WithStack(os.Remove(".old"))
}

func populateDev() error {
	devDir := "dev"
	if err := os.Mkdir(devDir, 0o755); err != nil && !os.IsExist(err) {
		return errors.WithStack(err)
	}
	if err := syscall.Mount("none", devDir, "tmpfs", 0, "size=4m"); err != nil {
		return errors.WithStack(err)
	}
	for _, dev := range []string{"console", "null", "zero", "random", "urandom"} {
		devPath := filepath.Join(devDir, dev)

		f, err := os.OpenFile(devPath, os.O_CREATE|os.O_RDONLY, 0o644)
		if err != nil {
			return errors.WithStack(err)
		}
		if err := f.Close(); err != nil {
			return errors.WithStack(err)
		}
		if err := syscall.Mount(filepath.Join("/dev", dev), devPath, "",
			syscall.MS_BIND|syscall.MS_PRIVATE, ""); err != nil {
			return errors.WithStack(err)
		}
	}
	links := map[string]string{
		"fd":     "/proc/self/fd",
		"stdin":  "/dev/fd/0",
		"stdout": "/dev/fd/1",
		"stderr": "/dev/fd/2",
	}
	for newName, oldName := range links {
		if err := os.Symlink(oldName, devDir+"/"+newName); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// Possible certificate files; stop after finding one.
var certFiles = []string{
	"/etc/ssl/certs/ca-certificates.crt",                // Debian/Ubuntu/Gentoo etc.
	"/etc/pki/tls/certs/ca-bundle.crt",                  // Fedora/RHEL 6
	"/etc/ssl/ca-bundle.pem",                            // OpenSUSE
	"/etc/pki/tls/cacert.pem",                           // OpenELEC
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", // CentOS/RHEL 7
	"/etc/ssl/cert.pem",                                 // Alpine Linux
}

// Possible directories with certificate files; all will be read.
var certDirectories = []string{
	"/etc/ssl/certs",     // SLES10/SLES11, https://golang.org/issue/12139
	"/etc/pki/tls/certs", // Fedora/RHEL
}

func storeRootCertificates() error {
	if err := os.MkdirAll(filepath.Dir(caCertFile), 0o755); err != nil {
		return errors.WithStack(err)
	}

	certF, err := os.OpenFile(caCertFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return errors.WithStack(err)
	}
	defer certF.Close()

	visited := map[string]struct{}{}
	for _, file := range certFiles {
		if err := appendCerts(certF, file, visited); err != nil {
			return err
		}
	}

	for _, directory := range certDirectories {
		entries, err := os.ReadDir(directory)
		switch {
		case err == nil:
			for _, e := range entries {
				if e.IsDir() {
					continue
				}

				if err := appendCerts(certF, filepath.Join(directory, e.Name()), visited); err != nil {
					return err
				}
			}
		case os.IsNotExist(err):
		default:
			return errors.WithStack(err)
		}
	}

	return nil
}

func appendCerts(w io.Writer, file string, visited map[string]struct{}) error {
	file, err := filepath.Abs(file)
	if err != nil {
		return errors.WithStack(err)
	}

	if _, exists := visited[file]; exists {
		return nil
	}
	visited[file] = struct{}{}

	data, err := os.ReadFile(file)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		return nil
	default:
		return errors.WithStack(err)
	}

	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return nil
		}
		if block.Type != "CERTIFICATE" {
			continue
		}

		if err := pem.Encode(w, block); err != nil {
			return errors.WithStack(err)
		}
	}
}
