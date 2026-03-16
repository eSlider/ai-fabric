package file

// This package makes it easier to implement tests to load data from a files.
//
// NOTE: Although it was not explicitly mentioned in the task,
// I decided to make the GetModRootPath() function return path and an error if `go.mod` was not found.

import (
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
)

var rootPath string

// GetModRootPath get the root path of the module
func GetModRootPath() (string, error) {

	// Get a current path
	path, _ := os.Getwd()
	// Get base directory by file info
	var prevPath string
	for {
		if Exists(path+"/etc") || Exists(path+"/data") {
			rootPath = path
			return rootPath, nil
		}
		// Break if we reach root directory: '/'
		// Break if prevPath is the same, maybe on Windows, something like C:// or c:\\
		if prevPath == path || len(path) < 2 {
			break
		}
		prevPath = path
		path = filepath.Dir(path)
	}
	return path, fmt.Errorf("go.mod not found")
}

// GetRootPath get the root path of the module
func GetRootPath() string {
	if rootPath != "" {
		return rootPath
	}
	path, err := GetModRootPath()
	if err != nil {
		panic(err)
	}
	rootPath = path
	return path
}

// GetCmdRootPath get the root path of the module by searching 'etc'
func GetCmdRootPath() string {
	dir, _ := os.Getwd()
	if strings.Contains(dir, "/etc/") {
		// We are in the cmd directory
		// Replace anything after /cmd/ using regular expressions
		dir = strings.Split(dir, "/etc/")[0]
	}
	return dir
}

// IsExist the path and isn't directory
func IsExist(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

// Size - Get file size
func Size(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// Exists - Check if file exists
func Exists(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	return true
}

// IsWritable - Check if the path is writable
func IsWritable(path string) bool {
	// Check if the directory is writable
	file, err := os.OpenFile(path+"/.test", os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return false
	}
	defer func() {
		file.Close()
		os.Remove(path + "/.test")
	}()
	return true
}

func PreCreateDirectory(path string) error {
	if Exists(path) {
		return nil
	}
	return os.MkdirAll(path, 0755)

}

// ReadDir reads all files with the specified extension in the directory and subdirectories
func ReadDir(dir, ext string) (map[string][]byte, error) {
	if dir == "" || ext == "" {
		return nil, fmt.Errorf("empty directory or extension")
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	// map with path and content
	var filteredFiles = make(map[string][]byte)
	for _, file := range files {
		if file.IsDir() {
			subFiles, err := ReadDir(filepath.Join(dir, file.Name()), ext)
			if err != nil {
				fmt.Printf("failed to read sub directory: %v\n", err)
				continue
			}
			for k, v := range subFiles {
				filteredFiles[k] = v
			}
			continue
		}

		// Filter files by extension
		_ex := filepath.Ext(file.Name())
		if _ex == ext {
			// Read file content
			bs, err := os.ReadFile(filepath.Join(dir, file.Name()))
			if err != nil {
				return nil, fmt.Errorf("failed to read file: %v", err)
			}
			name := dir + "/" + file.Name()
			filteredFiles[name] = bs
		}
	}
	return filteredFiles, nil
}

// GetFileName returns the file name without extension
func GetFileName(path string) string {
	// Get the base name of the file
	base := filepath.Base(path)
	// Remove the extension
	ext := filepath.Ext(base)
	if ext != "" {
		return base[:len(base)-len(ext)]
	}
	return base
}

// GetTempDir creates and returns a unique temporary directory under var/temp.
func GetTempDir() string {
	uq := uuid.New().String()
	path := GetRootPath() + "/var/temp/" + uq
	if err := os.MkdirAll(path, 0755); err != nil {
		panic(err)
	}
	return path
}
