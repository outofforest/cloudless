package kernel

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/ulikunitz/xz"
	"golang.org/x/sys/unix"
)

const (
	basePath    = "/usr/lib/modules"
	fileBuiltIn = "modules.builtin"
	fileDeps    = "modules.dep"
	fileLoaded  = "/proc/modules"
)

// Module describes module to load.
type Module struct {
	Name   string
	Params string
}

// LoadModule loads kernel module.
func LoadModule(module Module) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = errors.Wrapf(retErr, "loading module %q failed", module.Name)
		}
	}()

	release, err := Release()
	if err != nil {
		return err
	}

	releaseBase := filepath.Join(basePath, release)

	if isBuiltIn, err := isBuiltInModule(module.Name, filepath.Join(releaseBase, fileBuiltIn)); err != nil || isBuiltIn {
		return err
	}

	modulesToLoad, err := findModulesToLoad(module.Name, filepath.Join(releaseBase, fileDeps))
	if err != nil {
		return err
	}

	var params string
	for i := len(modulesToLoad) - 1; i >= 0; i-- {
		m := modulesToLoad[i]
		mName := filepath.Base(m)
		if dotIndex := strings.Index(mName, "."); dotIndex >= 0 {
			mName = mName[:dotIndex]
		}

		loaded, err := isLoaded(mName, fileLoaded)
		if err != nil {
			return err
		}
		if loaded {
			continue
		}

		if i == 0 {
			params = module.Params
		}
		if err := loadModule(releaseBase, m, params); err != nil {
			return err
		}
	}

	return nil
}

func isBuiltInModule(module string, fileBuiltIn string) (bool, error) {
	builtInF, err := os.Open(fileBuiltIn)
	if err != nil {
		return false, errors.WithStack(err)
	}
	defer builtInF.Close()

	expectedModuleName := module + ".ko"
	reader := bufio.NewReader(builtInF)
	for {
		line, err := reader.ReadString('\n')
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			return false, nil
		default:
			return false, errors.WithStack(err)
		}

		if filepath.Base(strings.TrimSpace(line)) == expectedModuleName {
			return true, nil
		}
	}
}

func findModulesToLoad(module string, fileDeps string) ([]string, error) {
	depF, err := os.Open(fileDeps)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer depF.Close()

	reader := bufio.NewReader(depF)
	for {
		line, err := reader.ReadString('\n')
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			return nil, errors.Errorf("module %q not found", module)
		default:
			return nil, errors.WithStack(err)
		}

		dotIndex := strings.Index(line, ".")
		if dotIndex < 0 {
			continue
		}

		if filepath.Base(line[:dotIndex]) != module {
			continue
		}

		colonIndex := strings.Index(line, ":")
		if colonIndex < 0 {
			continue
		}

		modulesToLoad := []string{
			line[:colonIndex],
		}

		if deps := strings.TrimSpace(line[colonIndex+1:]); deps != "" {
			for _, m := range strings.Split(deps, " ") {
				modulesToLoad = append(modulesToLoad, strings.TrimSpace(m))
			}
		}

		return modulesToLoad, nil
	}
}

func isLoaded(modulePath string, fileLoaded string) (bool, error) {
	module1 := strings.ReplaceAll(strings.TrimSuffix(filepath.Base(modulePath), ".ko.xz"), "_", "-")
	module2 := strings.ReplaceAll(module1, "-", "_")
	modulesF, err := os.Open(fileLoaded)
	if err != nil {
		return false, errors.WithStack(err)
	}
	defer modulesF.Close()

	reader := bufio.NewReader(modulesF)
	for {
		line, err := reader.ReadString('\n')
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			return false, nil
		default:
			return false, errors.WithStack(err)
		}

		spaceIndex := strings.Index(line, " ")
		if spaceIndex < 0 {
			continue
		}
		moduleName := line[:spaceIndex]
		if moduleName == module1 || moduleName == module2 {
			return true, nil
		}
	}
}

func loadModule(releaseBase, modulePath, params string) error {
	f, err := os.Open(filepath.Join(releaseBase, modulePath))
	if err != nil {
		return errors.WithStack(err)
	}
	defer f.Close()

	r, err := xz.NewReader(f)
	if err != nil {
		return errors.WithStack(err)
	}

	mod, err := io.ReadAll(r)
	if err != nil {
		return errors.WithStack(err)
	}

	return errors.WithStack(unix.InitModule(mod, params))
}
