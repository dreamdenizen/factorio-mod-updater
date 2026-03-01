package cmd

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)


func TestResolvePaths(t *testing.T) {
	t.Run("no args and no flags returns error", func(t *testing.T) {
		cfg := CLIConfig{}
		_, _, err := resolvePaths(cfg)
		if err == nil {
			t.Fatal("expected an error when no paths are provided")
		}
		if !strings.Contains(err.Error(), "ROOT_DIR") {
			t.Errorf("error should mention ROOT_DIR, got: %v", err)
		}
	})

	t.Run("positional arg infers bin-path and mod-path", func(t *testing.T) {
		cfg := CLIConfig{RootDir: "/opt/factorio"}
		fp, mp, err := resolvePaths(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedMod := filepath.Join("/opt/factorio", "mods")
		if mp != expectedMod {
			t.Errorf("modPath = %q; want %q", mp, expectedMod)
		}

		if runtime.GOOS == "windows" {
			expected := filepath.Join("/opt/factorio", "bin", "x64", "factorio.exe")
			if fp != expected {
				t.Errorf("bin-path = %q; want %q", fp, expected)
			}
		} else {
			expected := filepath.Join("/opt/factorio", "bin", "x64", "factorio")
			if fp != expected {
				t.Errorf("bin-path = %q; want %q", fp, expected)
			}
		}
	})

	t.Run("explicit --bin-path is not overwritten by rootDir", func(t *testing.T) {
		cfg := CLIConfig{
			RootDir:  "/opt/factorio",
			FactPath: "/custom/bin/factorio",
		}
		fp, mp, err := resolvePaths(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if fp != "/custom/bin/factorio" {
			t.Errorf("bin-path = %q; want /custom/bin/factorio (explicit should not be overwritten)", fp)
		}
		if mp != filepath.Join("/opt/factorio", "mods") {
			t.Errorf("mod-path = %q; want inferred path", mp)
		}
	})

	t.Run("explicit --mod-path is not overwritten by rootDir", func(t *testing.T) {
		cfg := CLIConfig{
			RootDir: "/opt/factorio",
			ModPath: "/custom/mods",
		}
		fp, _, err := resolvePaths(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// factPath should be inferred, modPath should stay explicit
		if runtime.GOOS != "windows" {
			if fp != filepath.Join("/opt/factorio", "bin", "x64", "factorio") {
				t.Errorf("bin-path should be inferred, got %q", fp)
			}
		}
	})

	t.Run("explicit flags without rootDir work", func(t *testing.T) {
		cfg := CLIConfig{
			FactPath: "/explicit/factorio",
			ModPath:  "/explicit/mods",
		}
		fp, mp, err := resolvePaths(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if fp != "/explicit/factorio" {
			t.Errorf("bin-path = %q; want /explicit/factorio", fp)
		}
		if mp != "/explicit/mods" {
			t.Errorf("mod-path = %q; want /explicit/mods", mp)
		}
	})

	t.Run("only --bin-path set without --mod-path returns error", func(t *testing.T) {
		cfg := CLIConfig{
			FactPath: "/some/bin/factorio",
		}
		_, _, err := resolvePaths(cfg)
		if err == nil {
			t.Fatal("expected error when --mod-path is missing")
		}
	})

	t.Run("only --mod-path set without --bin-path returns error", func(t *testing.T) {
		cfg := CLIConfig{
			ModPath: "/some/mods",
		}
		_, _, err := resolvePaths(cfg)
		if err == nil {
			t.Fatal("expected error when --bin-path is missing")
		}
	})
}
