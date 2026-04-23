package config

import (
	"os"
	"path/filepath"
)

const AppName = "sniffer-tui"

// Dir returns the directory of the running executable, where config.json
// lives. Falls back to the current working directory if the executable path
// cannot be resolved (e.g. during `go test`).
func Dir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return os.Getwd()
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		real = exe
	}
	return filepath.Dir(real), nil
}

func FilePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}
