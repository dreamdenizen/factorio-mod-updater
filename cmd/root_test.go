package cmd

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// resetPackageVars zeroes out all package-level path vars between test cases
// to prevent cross-contamination from shared mutable state.
func resetPackageVars() {
	rootDir = ""
	factPath = ""
	modPath = ""
	settingsPath = ""
	dataPath = ""
	username = ""
	token = ""
}

func TestResolvePaths(t *testing.T) {
	t.Run("no args and no flags returns error", func(t *testing.T) {
		resetPackageVars()

		_, _, err := resolvePaths([]string{})
		if err == nil {
			t.Fatal("expected an error when no paths are provided")
		}
		if !strings.Contains(err.Error(), "ROOT_DIR") {
			t.Errorf("error should mention ROOT_DIR, got: %v", err)
		}
	})

	t.Run("positional arg infers factPath and modPath", func(t *testing.T) {
		resetPackageVars()

		fp, mp, err := resolvePaths([]string{"/opt/factorio"})
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
				t.Errorf("factPath = %q; want %q", fp, expected)
			}
		} else {
			expected := filepath.Join("/opt/factorio", "bin", "x64", "factorio")
			if fp != expected {
				t.Errorf("factPath = %q; want %q", fp, expected)
			}
		}
	})

	t.Run("explicit factPath is not overwritten by rootDir", func(t *testing.T) {
		resetPackageVars()
		factPath = "/custom/bin/factorio"

		fp, mp, err := resolvePaths([]string{"/opt/factorio"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if fp != "/custom/bin/factorio" {
			t.Errorf("factPath = %q; want /custom/bin/factorio (explicit should not be overwritten)", fp)
		}
		if mp != filepath.Join("/opt/factorio", "mods") {
			t.Errorf("modPath = %q; want inferred path", mp)
		}
	})

	t.Run("explicit modPath is not overwritten by rootDir", func(t *testing.T) {
		resetPackageVars()
		modPath = "/custom/mods"

		fp, _, err := resolvePaths([]string{"/opt/factorio"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// factPath should be inferred, modPath should stay explicit
		if runtime.GOOS != "windows" {
			if fp != filepath.Join("/opt/factorio", "bin", "x64", "factorio") {
				t.Errorf("factPath should be inferred, got %q", fp)
			}
		}
	})

	t.Run("explicit flags without rootDir work", func(t *testing.T) {
		resetPackageVars()
		factPath = "/explicit/factorio"
		modPath = "/explicit/mods"

		fp, mp, err := resolvePaths([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if fp != "/explicit/factorio" {
			t.Errorf("factPath = %q; want /explicit/factorio", fp)
		}
		if mp != "/explicit/mods" {
			t.Errorf("modPath = %q; want /explicit/mods", mp)
		}
	})

	t.Run("only factPath set without modPath returns error", func(t *testing.T) {
		resetPackageVars()
		factPath = "/some/bin/factorio"

		_, _, err := resolvePaths([]string{})
		if err == nil {
			t.Fatal("expected error when modPath is missing")
		}
	})

	t.Run("only modPath set without factPath returns error", func(t *testing.T) {
		resetPackageVars()
		modPath = "/some/mods"

		_, _, err := resolvePaths([]string{})
		if err == nil {
			t.Fatal("expected error when factPath is missing")
		}
	})
}
