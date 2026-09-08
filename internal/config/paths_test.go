package config

import (
	"path/filepath"
	"testing"
)

func TestSearchPaths(t *testing.T) {
	home := t.TempDir()

	tests := []struct {
		name       string
		configHome string
		configDirs string
		want       []string
	}{
		{
			name: "defaults to /etc/xdg and ~/.config",
			want: []string{
				filepath.Join("/etc/xdg", "teamster", "config.yaml"),
				filepath.Join(home, ".config", "teamster", "config.yaml"),
				"config.yaml",
			},
		},
		{
			name:       "honours XDG_CONFIG_HOME",
			configHome: "/custom/config",
			want: []string{
				filepath.Join("/etc/xdg", "teamster", "config.yaml"),
				filepath.Join("/custom/config", "teamster", "config.yaml"),
				"config.yaml",
			},
		},
		{
			name:       "relative XDG_CONFIG_HOME is ignored per spec",
			configHome: "relative/config",
			want: []string{
				filepath.Join("/etc/xdg", "teamster", "config.yaml"),
				filepath.Join(home, ".config", "teamster", "config.yaml"),
				"config.yaml",
			},
		},
		{
			name:       "XDG_CONFIG_DIRS is reversed so the preferred dir wins",
			configDirs: "/etc/xdg:/opt/teamster/etc",
			want: []string{
				filepath.Join("/opt/teamster/etc", "teamster", "config.yaml"),
				filepath.Join("/etc/xdg", "teamster", "config.yaml"),
				filepath.Join(home, ".config", "teamster", "config.yaml"),
				"config.yaml",
			},
		},
		{
			name:       "empty XDG_CONFIG_DIRS falls back to the default",
			configDirs: "  ",
			want: []string{
				filepath.Join("/etc/xdg", "teamster", "config.yaml"),
				filepath.Join(home, ".config", "teamster", "config.yaml"),
				"config.yaml",
			},
		},
		{
			name:       "relative entries in XDG_CONFIG_DIRS are dropped",
			configDirs: "relative:/etc/xdg",
			want: []string{
				filepath.Join("/etc/xdg", "teamster", "config.yaml"),
				filepath.Join(home, ".config", "teamster", "config.yaml"),
				"config.yaml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", tt.configHome)
			t.Setenv("XDG_CONFIG_DIRS", tt.configDirs)

			got := SearchPaths()
			if len(got) != len(tt.want) {
				t.Fatalf("SearchPaths() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("SearchPaths()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
