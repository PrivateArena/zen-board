package cli

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"zen-board/internal/model"
)

// BinaryDir is the directory holding the executable, or the working directory
// when running under `go test` / `go run` (where the exe lives in a build cache).
func BinaryDir() string {
	exe, err := os.Executable()
	if err != nil {
		dir, _ := os.Getwd()
		return dir
	}
	dir := filepath.Dir(exe)
	if strings.Contains(exe, "go-build") || strings.HasPrefix(filepath.ToSlash(exe), filepath.ToSlash(os.TempDir())) {
		dir, _ = os.Getwd()
	}
	return dir
}

// LoadConfig reads config.json from next to the binary, then from the working
// directory, and returns the project defaults. The first file that parses wins;
// with none present the built-in defaults stand alone.
func LoadConfig(binDir string) *model.Project {
	p := model.NewDefaultProject()
	for _, path := range []string{
		filepath.Join(binDir, "config.json"),
		"config.json",
	} {
		absPath, _ := filepath.Abs(path)
		if _, err := os.Stat(absPath); err != nil {
			continue
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, p); err == nil {
			log.Printf("[Config] Loaded from: %s", absPath)
			return p
		}
	}
	return p
}
