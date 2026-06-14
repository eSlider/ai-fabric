package file

import (
	"fmt"
	"os"
	"path/filepath"
)

var rootPath string

// GetRootPath returns the module root (directory containing go.mod).
func GetRootPath() string {
	if rootPath != "" {
		return rootPath
	}
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			rootPath = dir
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic(fmt.Errorf("go.mod not found"))
		}
		dir = parent
	}
}
