package spaces

import (
	"fmt"
	"os"
	"path/filepath"
)

const configFileName = ".remux.yaml"

// FindRoot walks up from the given directory looking for .remux.yaml,
// returning the directory that contains it. Returns an error if it
// reaches the filesystem root without finding one.
func FindRoot(from string) (string, error) {
	dir, err := filepath.Abs(from)
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, configFileName)); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s found in any parent directory", configFileName)
		}
		dir = parent
	}
}
