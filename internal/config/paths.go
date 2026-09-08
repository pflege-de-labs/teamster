package config

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// appDir is the per-application directory below the XDG config directories.
	appDir = "teamster"
	// fileName is the config file name looked up in every search directory.
	fileName = "config.yaml"
	// LocalFile is the working-directory config, overriding the XDG locations.
	LocalFile = "config.yaml"
)

// SearchPaths returns config file candidates following the XDG Base Directory
// Specification, least specific first: kong applies the last resolved value.
func SearchPaths() []string {
	paths := []string{}
	for _, dir := range configDirs() {
		paths = append(paths, filepath.Join(dir, appDir, fileName))
	}
	paths = append(paths, filepath.Join(configHome(), appDir, fileName))
	return append(paths, LocalFile)
}

// configHome resolves $XDG_CONFIG_HOME, defaulting to ~/.config.
func configHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config"
	}
	return filepath.Join(home, ".config")
}

// configDirs resolves $XDG_CONFIG_DIRS, defaulting to /etc/xdg. The spec orders
// it most-preferred first, so it is reversed to match kong's last-wins lookup.
func configDirs() []string {
	raw := os.Getenv("XDG_CONFIG_DIRS")
	if strings.TrimSpace(raw) == "" {
		return []string{"/etc/xdg"}
	}

	dirs := []string{}
	for _, dir := range strings.Split(raw, string(os.PathListSeparator)) {
		if filepath.IsAbs(dir) {
			dirs = append(dirs, dir)
		}
	}
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}
