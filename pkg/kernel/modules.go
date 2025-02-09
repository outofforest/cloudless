package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

const (
	basePath = "/usr/lib/modules"
	fileDeps = basePath + "/deps.json"
)

var (
	loadedModules = map[string]struct{}{}
	deps          map[string][]string
)

// Module describes module to load.
type Module struct {
	Name   string
	Params string
}

// LoadModule loads kernel module.
func LoadModule(module Module) (retErr error) {
	module.Name = strings.ReplaceAll(module.Name, "_", "-")
	if _, exists := loadedModules[module.Name]; exists {
		return nil
	}

	if deps == nil {
		f, err := os.Open(fileDeps)
		if err != nil {
			return errors.WithStack(err)
		}
		defer f.Close()

		deps = map[string][]string{}
		if err := json.NewDecoder(f).Decode(&deps); err != nil {
			return errors.WithStack(err)
		}
	}

	for _, d := range deps[module.Name] {
		if err := LoadModule(Module{Name: d}); err != nil {
			return err
		}
	}

	f, err := os.Open(filepath.Join(basePath, module.Name+".ko"))
	if err != nil {
		return errors.WithStack(err)
	}
	defer f.Close()

	if err := unix.FinitModule(int(f.Fd()), module.Params, 0); err != nil {
		return errors.WithStack(err)
	}

	loadedModules[module.Name] = struct{}{}

	return nil
}
