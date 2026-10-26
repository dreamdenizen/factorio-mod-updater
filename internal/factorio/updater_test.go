package factorio

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVersionMatch(t *testing.T) {
	tests := []struct {
		name      string
		installed string
		mod       string
		expected  bool
	}{
		{
			name:      "exact match major minor",
			installed: "2.0.1",
			mod:       "2.0",
			expected:  true,
		},
		{
			name:      "exact match full",
			installed: "2.0.1",
			mod:       "2.0.1",
			expected:  true,
		},
		{
			name:      "mismatch minor",
			installed: "2.1.0",
			mod:       "2.0",
			expected:  false,
		},
		{
			// Compatibility is decided on the major.minor branch; a patch
			// component declared by a mod is informational.
			name:      "patch component is ignored",
			installed: "2.0.1",
			mod:       "2.0.2",
			expected:  true,
		},
		{
			// Regression: determineVersion only ever yields major.minor, so
			// comparing a three-component factorio_version as a string
			// rejected every mod that declared one.
			name:      "three component mod version on major minor install",
			installed: "2.0",
			mod:       "2.0.0",
			expected:  true,
		},
		{
			name:      "three component mod version with high patch",
			installed: "2.0",
			mod:       "2.0.55",
			expected:  true,
		},
		{
			name:      "three component mod version on wrong branch",
			installed: "2.0",
			mod:       "1.1.0",
			expected:  false,
		},
		{
			name:      "legacy 0.18 on 1.0 match",
			installed: "1.0.0",
			mod:       "0.18.33",
			expected:  true,
		},
		{
			name:      "legacy 0.18 on 1.1 match",
			installed: "1.1.0",
			mod:       "0.18.33",
			expected:  true,
		},
		{
			name:      "invalid mod format",
			installed: "2.0",
			mod:       "invalid",
			expected:  false,
		},
		{
			name:      "invalid installed format",
			installed: "invalid",
			mod:       "2.0",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := versionMatch(tt.installed, tt.mod)
			if result != tt.expected {
				t.Errorf("versionMatch(%q, %q) = %v; want %v", tt.installed, tt.mod, result, tt.expected)
			}
		})
	}
}

func TestIsBuiltInMod(t *testing.T) {
	tests := []struct {
		name     string
		modName  string
		expected bool
	}{
		{"base is built-in", "base", true},
		{"core is built-in", "core", true},
		{"space-age is built-in", "space-age", true},
		{"quality is built-in", "quality", true},
		{"elevated-rails is built-in", "elevated-rails", true},
		{"recycler is built-in", "recycler", true},
		{"random mod is not built-in", "helmod", false},
		{"empty string is not built-in", "", false},
		{"case sensitive check", "Base", false},
		{"partial match is not built-in", "space", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBuiltInMod(tt.modName)
			if result != tt.expected {
				t.Errorf("isBuiltInMod(%q) = %v; want %v", tt.modName, result, tt.expected)
			}
		})
	}
}

func TestParseModList(t *testing.T) {
	// Create a temp directory to simulate a mods folder
	tmpDir := t.TempDir()

	// Write a mod-list.json that includes built-in and real mods
	modList := map[string]any{
		"mods": []map[string]any{
			{"name": "base", "enabled": true},
			{"name": "space-age", "enabled": true},
			{"name": "quality", "enabled": true},
			{"name": "recycler", "enabled": true},
			{"name": "helmod", "enabled": true},
			{"name": "jetpack", "enabled": false},
			{"name": "not-installed-mod", "enabled": true},
		},
	}
	modListData, _ := json.Marshal(modList)
	_ = os.WriteFile(filepath.Join(tmpDir, "mod-list.json"), modListData, 0644)

	// Create fake zip files to simulate installed mods
	_ = os.WriteFile(filepath.Join(tmpDir, "helmod_2.2.12.zip"), []byte("fake"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "jetpack_0.4.15.zip"), []byte("fake"), 0644)

	u := &Updater{
		modPath: tmpDir,
		mods:    make(map[string]*ModData),
	}

	if err := u.parseModList(); err != nil {
		t.Fatalf("parseModList() returned unexpected error: %v", err)
	}

	// Built-in mods should be excluded
	for _, builtin := range []string{"base", "space-age", "quality", "recycler"} {
		if _, ok := u.mods[builtin]; ok {
			t.Errorf("built-in mod %q should have been excluded from mods map", builtin)
		}
	}

	// Real mods should be present
	if _, ok := u.mods["helmod"]; !ok {
		t.Fatal("expected 'helmod' to be in mods map")
	}
	if _, ok := u.mods["jetpack"]; !ok {
		t.Fatal("expected 'jetpack' to be in mods map")
	}
	if _, ok := u.mods["not-installed-mod"]; !ok {
		t.Fatal("expected 'not-installed-mod' to be in mods map")
	}

	// Verify installed versions were detected from zip filenames
	if !u.mods["helmod"].Installed || u.mods["helmod"].Version != "2.2.12" {
		t.Errorf("helmod: installed=%v version=%q; want installed=true version=2.2.12",
			u.mods["helmod"].Installed, u.mods["helmod"].Version)
	}
	if !u.mods["jetpack"].Installed || u.mods["jetpack"].Version != "0.4.15" {
		t.Errorf("jetpack: installed=%v version=%q; want installed=true version=0.4.15",
			u.mods["jetpack"].Installed, u.mods["jetpack"].Version)
	}

	// Verify enabled state
	if !u.mods["helmod"].Enabled {
		t.Error("helmod should be enabled")
	}
	if u.mods["jetpack"].Enabled {
		t.Error("jetpack should be disabled")
	}

	// Verify not-installed mod
	if u.mods["not-installed-mod"].Installed {
		t.Error("not-installed-mod should not be marked as installed")
	}
}

func TestParseTokens(t *testing.T) {
	t.Run("server-settings takes priority over player-data", func(t *testing.T) {
		tmpDir := t.TempDir()

		serverSettings := `{"username": "server_user", "token": "server_token"}`
		_ = os.WriteFile(filepath.Join(tmpDir, "server-settings.json"), []byte(serverSettings), 0644)

		playerData := `{"service-username": "player_user", "service-token": "player_token"}`
		_ = os.WriteFile(filepath.Join(tmpDir, "player-data.json"), []byte(playerData), 0644)

		u := &Updater{
			settingsPath: filepath.Join(tmpDir, "server-settings.json"),
			dataPath:     filepath.Join(tmpDir, "player-data.json"),
			modPath:      filepath.Join(tmpDir, "mods"),
		}

		if err := u.parseTokens(); err != nil {
			t.Fatalf("parseTokens() returned unexpected error: %v", err)
		}

		if u.username != "server_user" {
			t.Errorf("username = %q; want %q", u.username, "server_user")
		}
		if u.token != "server_token" {
			t.Errorf("token = %q; want %q", u.token, "server_token")
		}
	})

	t.Run("falls back to player-data when server-settings missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		playerData := `{"service-username": "player_user", "service-token": "player_token"}`
		_ = os.WriteFile(filepath.Join(tmpDir, "player-data.json"), []byte(playerData), 0644)

		u := &Updater{
			dataPath: filepath.Join(tmpDir, "player-data.json"),
			modPath:  filepath.Join(tmpDir, "mods"),
		}

		if err := u.parseTokens(); err != nil {
			t.Fatalf("parseTokens() returned unexpected error: %v", err)
		}

		if u.username != "player_user" {
			t.Errorf("username = %q; want %q", u.username, "player_user")
		}
		if u.token != "player_token" {
			t.Errorf("token = %q; want %q", u.token, "player_token")
		}
	})

	t.Run("cli flags are not overwritten", func(t *testing.T) {
		tmpDir := t.TempDir()

		serverSettings := `{"username": "server_user", "token": "server_token"}`
		_ = os.WriteFile(filepath.Join(tmpDir, "server-settings.json"), []byte(serverSettings), 0644)

		u := &Updater{
			settingsPath: filepath.Join(tmpDir, "server-settings.json"),
			modPath:      filepath.Join(tmpDir, "mods"),
			username:     "cli_user",
			token:        "cli_token",
		}

		if err := u.parseTokens(); err != nil {
			t.Fatalf("parseTokens() returned unexpected error: %v", err)
		}

		if u.username != "cli_user" {
			t.Errorf("username = %q; want %q (CLI should take priority)", u.username, "cli_user")
		}
		if u.token != "cli_token" {
			t.Errorf("token = %q; want %q (CLI should take priority)", u.token, "cli_token")
		}
	})

	t.Run("auto-discovers server-settings.json from modPath parent", func(t *testing.T) {
		tmpDir := t.TempDir()
		modsDir := filepath.Join(tmpDir, "mods")
		_ = os.MkdirAll(modsDir, 0755)

		serverSettings := `{"username": "discovered_user", "token": "discovered_token"}`
		_ = os.WriteFile(filepath.Join(tmpDir, "server-settings.json"), []byte(serverSettings), 0644)

		u := &Updater{
			modPath: modsDir,
		}

		if err := u.parseTokens(); err != nil {
			t.Fatalf("parseTokens() returned unexpected error: %v", err)
		}

		if u.username != "discovered_user" {
			t.Errorf("username = %q; want %q", u.username, "discovered_user")
		}
		if u.token != "discovered_token" {
			t.Errorf("token = %q; want %q", u.token, "discovered_token")
		}
	})

	t.Run("malformed settings JSON falls back to player-data", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Write invalid JSON to server-settings
		_ = os.WriteFile(filepath.Join(tmpDir, "server-settings.json"), []byte("{invalid json}"), 0644)

		// Write valid player-data
		playerData := `{"service-username": "fallback_user", "service-token": "fallback_token"}`
		_ = os.WriteFile(filepath.Join(tmpDir, "player-data.json"), []byte(playerData), 0644)

		u := &Updater{
			settingsPath: filepath.Join(tmpDir, "server-settings.json"),
			dataPath:     filepath.Join(tmpDir, "player-data.json"),
			modPath:      filepath.Join(tmpDir, "mods"),
		}

		if err := u.parseTokens(); err != nil {
			t.Fatalf("parseTokens() should not return error on malformed config: %v", err)
		}

		if u.username != "fallback_user" {
			t.Errorf("username = %q; want %q (should fall back to player-data)", u.username, "fallback_user")
		}
		if u.token != "fallback_token" {
			t.Errorf("token = %q; want %q (should fall back to player-data)", u.token, "fallback_token")
		}
	})

	t.Run("both configs malformed returns no error but leaves credentials empty", func(t *testing.T) {
		tmpDir := t.TempDir()

		_ = os.WriteFile(filepath.Join(tmpDir, "server-settings.json"), []byte("{bad}"), 0644)
		_ = os.WriteFile(filepath.Join(tmpDir, "player-data.json"), []byte("{bad}"), 0644)

		u := &Updater{
			settingsPath: filepath.Join(tmpDir, "server-settings.json"),
			dataPath:     filepath.Join(tmpDir, "player-data.json"),
			modPath:      filepath.Join(tmpDir, "mods"),
		}

		if err := u.parseTokens(); err != nil {
			t.Fatalf("parseTokens() should not return error on malformed configs: %v", err)
		}

		// Both configs were unparseable, so credentials remain empty
		if u.username != "" {
			t.Errorf("username = %q; want empty (both configs malformed)", u.username)
		}
		if u.token != "" {
			t.Errorf("token = %q; want empty (both configs malformed)", u.token)
		}
	})
}

func TestSaveModList(t *testing.T) {
	tmpDir := t.TempDir()

	// Seed an existing mod-list.json so saveModList can back it up
	_ = os.WriteFile(filepath.Join(tmpDir, "mod-list.json"), []byte(`{"mods":[]}`), 0644)

	u := &Updater{
		modPath: tmpDir,
		mods: map[string]*ModData{
			"zebra-mod":  {Name: "zebra-mod", Enabled: true},
			"alpha-mod":  {Name: "alpha-mod", Enabled: false},
			"middle-mod": {Name: "middle-mod", Enabled: true},
		},
	}

	if err := u.saveModList(); err != nil {
		t.Fatalf("saveModList() returned unexpected error: %v", err)
	}

	// Read back the written file
	data, err := os.ReadFile(filepath.Join(tmpDir, "mod-list.json"))
	if err != nil {
		t.Fatalf("failed to read written mod-list.json: %v", err)
	}

	type modEntry struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	var result struct {
		Mods []modEntry `json:"mods"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse written mod-list.json: %v", err)
	}

	// Verify all 3 mods are present
	if len(result.Mods) != 3 {
		t.Fatalf("expected 3 mods, got %d", len(result.Mods))
	}

	if result.Mods[0].Name != "alpha-mod" {
		t.Errorf("first mod should be 'alpha-mod', got %q (cmp.Compare sort failed)", result.Mods[0].Name)
	}
	if result.Mods[1].Name != "middle-mod" {
		t.Errorf("second mod should be 'middle-mod', got %q (cmp.Compare sort failed)", result.Mods[1].Name)
	}
	if result.Mods[2].Name != "zebra-mod" {
		t.Errorf("third mod should be 'zebra-mod', got %q (cmp.Compare sort failed)", result.Mods[2].Name)
	}

	// Verify enabled states
	if result.Mods[0].Enabled {
		t.Error("alpha-mod should be disabled")
	}
	if !result.Mods[1].Enabled {
		t.Error("middle-mod should be enabled")
	}

	// Verify a backup file was created
	files, _ := os.ReadDir(tmpDir)
	backupFound := false
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "mod-list.") && f.Name() != "mod-list.json" {
			backupFound = true
			break
		}
	}
	if !backupFound {
		t.Error("expected a backup file (mod-list.*.json) to be created")
	}
}

func TestValidateHash(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.zip")
	content := []byte("hello world")
	_ = os.WriteFile(testFile, content, 0644)

	// Compute expected SHA-1
	h := sha1.New()
	h.Write(content)
	expectedHash := hex.EncodeToString(h.Sum(nil))

	t.Run("correct hash returns true", func(t *testing.T) {
		if !validateSHA1(expectedHash, testFile) {
			t.Errorf("validateSHA1(%q) should return true for matching content", expectedHash)
		}
	})

	t.Run("incorrect hash returns false", func(t *testing.T) {
		if validateSHA1("deadbeef1234567890abcdef1234567890abcdef", testFile) {
			t.Error("validateSHA1 should return false for non-matching hash")
		}
	})

	t.Run("missing file returns false", func(t *testing.T) {
		if validateSHA1(expectedHash, filepath.Join(tmpDir, "nonexistent.zip")) {
			t.Error("validateSHA1 should return false for missing file")
		}
	})
}

func TestGetMods(t *testing.T) {
	u := &Updater{
		mods: map[string]*ModData{
			"zebra":  {Name: "zebra", Title: "Zebra Mod"},
			"alpha":  {Name: "alpha", Title: "Alpha Mod"},
			"middle": {Name: "middle", Title: "Middle Mod"},
		},
	}

	mods := u.GetMods()

	if len(mods) != 3 {
		t.Fatalf("expected 3 mods, got %d", len(mods))
	}

	if mods[0].Title != "Alpha Mod" {
		t.Errorf("first mod title = %q; want Alpha Mod (cmp.Compare sort failed)", mods[0].Title)
	}
	if mods[1].Title != "Middle Mod" {
		t.Errorf("second mod title = %q; want Middle Mod (cmp.Compare sort failed)", mods[1].Title)
	}
	if mods[2].Title != "Zebra Mod" {
		t.Errorf("third mod title = %q; want Zebra Mod (cmp.Compare sort failed)", mods[2].Title)
	}

	// Verify returned slice is independent (capacity pre-allocated)
	if cap(mods) < 3 {
		t.Errorf("slice capacity = %d; expected at least 3", cap(mods))
	}
}

func TestWriteCounter(t *testing.T) {
	t.Run("nil progress pointer does not panic", func(t *testing.T) {
		wc := &writeCounter{
			Total:    1000,
			Progress: nil,
		}
		data := make([]byte, 500)
		n, err := wc.Write(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 500 {
			t.Errorf("Write returned %d; want 500", n)
		}
		if wc.Current != 500 {
			t.Errorf("Current = %d; want 500", wc.Current)
		}
	})

	t.Run("tracks cumulative bytes", func(t *testing.T) {
		wc := &writeCounter{
			Total:    1000,
			Progress: nil,
		}
		_, _ = wc.Write(make([]byte, 300))
		_, _ = wc.Write(make([]byte, 400))
		if wc.Current != 700 {
			t.Errorf("Current = %d; want 700", wc.Current)
		}
	})

	t.Run("zero total does not divide by zero", func(t *testing.T) {
		wc := &writeCounter{
			Total:    0,
			Progress: nil,
		}
		n, err := wc.Write(make([]byte, 100))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 100 {
			t.Errorf("Write returned %d; want 100", n)
		}
	})
}

func TestPruneOld(t *testing.T) {
	t.Run("removes old versions and keeps latest", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create multiple versioned zips for "helmod"
		_ = os.WriteFile(filepath.Join(tmpDir, "helmod_2.1.0.zip"), []byte("old1"), 0644)
		_ = os.WriteFile(filepath.Join(tmpDir, "helmod_2.1.5.zip"), []byte("old2"), 0644)
		_ = os.WriteFile(filepath.Join(tmpDir, "helmod_2.2.12.zip"), []byte("latest"), 0644)

		// Create an unrelated mod zip (should not be touched)
		_ = os.WriteFile(filepath.Join(tmpDir, "jetpack_0.4.15.zip"), []byte("other"), 0644)

		// Create a non-zip file (should not be touched)
		_ = os.WriteFile(filepath.Join(tmpDir, "README.txt"), []byte("readme"), 0644)

		u := &Updater{
			modPath: tmpDir,
			mods: map[string]*ModData{
				"helmod": {
					Name: "helmod",
					Latest: &ModRelease{
						Version:  "2.2.12",
						FileName: "helmod_2.2.12.zip",
					},
				},
			},
		}

		if err := u.pruneOld("helmod"); err != nil {
			t.Fatalf("pruneOld() returned unexpected error: %v", err)
		}

		// Verify old versions were removed
		if _, err := os.Stat(filepath.Join(tmpDir, "helmod_2.1.0.zip")); !os.IsNotExist(err) {
			t.Error("helmod_2.1.0.zip should have been pruned")
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "helmod_2.1.5.zip")); !os.IsNotExist(err) {
			t.Error("helmod_2.1.5.zip should have been pruned")
		}

		// Verify latest version was kept
		if _, err := os.Stat(filepath.Join(tmpDir, "helmod_2.2.12.zip")); err != nil {
			t.Error("helmod_2.2.12.zip (latest) should NOT have been pruned")
		}

		// Verify unrelated files were not touched
		if _, err := os.Stat(filepath.Join(tmpDir, "jetpack_0.4.15.zip")); err != nil {
			t.Error("jetpack_0.4.15.zip should NOT have been touched")
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "README.txt")); err != nil {
			t.Error("README.txt should NOT have been touched")
		}
	})

	t.Run("empty filename returns error without deleting files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create files that should NOT be deleted
		_ = os.WriteFile(filepath.Join(tmpDir, "helmod_2.1.0.zip"), []byte("keep"), 0644)

		u := &Updater{
			modPath: tmpDir,
			mods: map[string]*ModData{
				"helmod": {
					Name: "helmod",
					Latest: &ModRelease{
						Version:  "2.1.0",
						FileName: "", // empty filename — dangerous without guard
					},
				},
			},
		}

		err := u.pruneOld("helmod")
		if err == nil {
			t.Fatal("pruneOld() should return error for empty FileName")
		}
		if !strings.Contains(err.Error(), "empty filename") {
			t.Errorf("error should mention empty filename, got: %v", err)
		}

		// Verify no files were deleted
		if _, statErr := os.Stat(filepath.Join(tmpDir, "helmod_2.1.0.zip")); statErr != nil {
			t.Error("helmod_2.1.0.zip should NOT have been deleted when FileName is empty")
		}
	})
}

func TestDownloadFile(t *testing.T) {
	// Test content and its expected SHA-1
	content := []byte("hello factorio mods")
	h := sha1.New()
	h.Write(content)
	correctHash := hex.EncodeToString(h.Sum(nil))

	t.Run("successful download validates hash", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			_, _ = w.Write(content)
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "test_mod_1.0.0.zip")

		err := downloadFile(server.Client(), target, server.URL, nil, correctHash)
		if err != nil {
			t.Fatalf("downloadFile() returned unexpected error: %v", err)
		}

		// Verify file exists and content matches
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("failed to read downloaded file: %v", err)
		}
		if string(data) != string(content) {
			t.Errorf("file content = %q; want %q", data, content)
		}
	})

	t.Run("hash mismatch removes downloaded file", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			_, _ = w.Write(content)
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "bad_hash_1.0.0.zip")

		err := downloadFile(server.Client(), target, server.URL, nil, "0000000000000000000000000000000000000000")
		if err == nil {
			t.Fatal("downloadFile() should return error on hash mismatch")
		}

		// File should have been cleaned up
		if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
			t.Error("file should have been removed after hash mismatch")
		}
	})

	t.Run("server error mid-stream cleans up partial file", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Claim a large file, but close connection early
			w.Header().Set("Content-Length", "1000000")
			_, _ = w.Write([]byte("partial"))
			// Server handler returns, connection closes - io.Copy gets unexpected EOF
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "partial_1.0.0.zip")

		err := downloadFile(server.Client(), target, server.URL, nil, correctHash)
		if err == nil {
			t.Fatal("downloadFile() should return error on truncated download")
		}

		// Partial file should have been cleaned up (CR-4)
		if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
			t.Error("partial file should have been removed after io.Copy error")
		}
	})
}

// --- Unit test for ResolveMetadata using httptest ---

func TestResolveMetadata(t *testing.T) {
	t.Run("resolves transitive dependencies", func(t *testing.T) {
		// Mock API: "mod-a" depends on "mod-b", "mod-b" has no further deps.
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/mods/mod-a/full":
				resp := ModPortalMetadata{
					Title: "Mod A",
					Releases: []ModRelease{
						{
							Version:     "1.0.0",
							DownloadURL: "/download/mod-a",
							FileName:    "mod-a_1.0.0.zip",
							Sha1:        "aaa",
							InfoJSON: struct {
								FactorioVersion string   `json:"factorio_version"`
								Dependencies    []string `json:"dependencies"`
							}{
								FactorioVersion: "2.0",
								Dependencies:    []string{"mod-b >= 1.0.0"},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			case "/api/mods/mod-b/full":
				resp := ModPortalMetadata{
					Title: "Mod B",
					Releases: []ModRelease{
						{
							Version:     "1.2.0",
							DownloadURL: "/download/mod-b",
							FileName:    "mod-b_1.2.0.zip",
							Sha1:        "bbb",
							InfoJSON: struct {
								FactorioVersion string   `json:"factorio_version"`
								Dependencies    []string `json:"dependencies"`
							}{
								FactorioVersion: "2.0",
								Dependencies:    []string{},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			default:
				http.NotFound(w, r)
			}
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		u := &Updater{
			modServerURL: server.URL,
			factVersion:  "2.0",
			mods: map[string]*ModData{
				"mod-a": {Name: "mod-a", Title: "mod-a", Enabled: true},
			},
			httpClient: server.Client(),
		}

		err := u.ResolveMetadata()
		if err != nil {
			t.Fatalf("ResolveMetadata() returned unexpected error: %v", err)
		}

		// mod-a should have its title resolved
		if u.mods["mod-a"].Title != "Mod A" {
			t.Errorf("mod-a title = %q; want %q", u.mods["mod-a"].Title, "Mod A")
		}

		// mod-b should have been discovered as a transitive dependency
		modB, ok := u.mods["mod-b"]
		if !ok {
			t.Fatal("expected transitive dependency 'mod-b' to be discovered")
		}
		if modB.Title != "Mod B" {
			t.Errorf("mod-b title = %q; want %q", modB.Title, "Mod B")
		}
		if modB.Latest == nil {
			t.Fatal("mod-b should have a Latest release")
		}
		if modB.Latest.Version != "1.2.0" {
			t.Errorf("mod-b latest version = %q; want %q", modB.Latest.Version, "1.2.0")
		}
	})

	t.Run("handles circular dependencies without infinite loop", func(t *testing.T) {
		// "circle-a" depends on "circle-b", "circle-b" depends on "circle-a"
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var resp ModPortalMetadata
			switch r.URL.Path {
			case "/api/mods/circle-a/full":
				resp = ModPortalMetadata{
					Title: "Circle A",
					Releases: []ModRelease{
						{
							Version:  "1.0.0",
							FileName: "circle-a_1.0.0.zip",
							InfoJSON: struct {
								FactorioVersion string   `json:"factorio_version"`
								Dependencies    []string `json:"dependencies"`
							}{
								FactorioVersion: "2.0",
								Dependencies:    []string{"circle-b"},
							},
						},
					},
				}
			case "/api/mods/circle-b/full":
				resp = ModPortalMetadata{
					Title: "Circle B",
					Releases: []ModRelease{
						{
							Version:  "1.0.0",
							FileName: "circle-b_1.0.0.zip",
							InfoJSON: struct {
								FactorioVersion string   `json:"factorio_version"`
								Dependencies    []string `json:"dependencies"`
							}{
								FactorioVersion: "2.0",
								Dependencies:    []string{"circle-a"},
							},
						},
					},
				}
			default:
				http.NotFound(w, r)
			}
			_ = json.NewEncoder(w).Encode(resp)
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		u := &Updater{
			modServerURL: server.URL,
			factVersion:  "2.0",
			mods: map[string]*ModData{
				"circle-a": {Name: "circle-a", Title: "circle-a", Enabled: true},
			},
			httpClient: server.Client(),
		}

		// Should complete without hanging
		err := u.ResolveMetadata()
		if err != nil {
			t.Fatalf("ResolveMetadata() returned unexpected error: %v", err)
		}

		// Both should be resolved
		if _, ok := u.mods["circle-a"]; !ok {
			t.Error("circle-a should be in mods map")
		}
		if _, ok := u.mods["circle-b"]; !ok {
			t.Error("circle-b should have been discovered")
		}
	})

	t.Run("trims whitespace in dependency strings", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/mods/trimmod/full":
				resp := ModPortalMetadata{
					Title: "Trim Mod",
					Releases: []ModRelease{
						{
							Version:  "1.0.0",
							FileName: "trimmod_1.0.0.zip",
							InfoJSON: struct {
								FactorioVersion string   `json:"factorio_version"`
								Dependencies    []string `json:"dependencies"`
							}{
								FactorioVersion: "2.0",
								Dependencies:    []string{"  spacey-dep >= 1.0.0  "},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			case "/api/mods/spacey-dep/full":
				resp := ModPortalMetadata{
					Title: "Spacey Dep",
					Releases: []ModRelease{
						{
							Version:  "1.0.0",
							FileName: "spacey-dep_1.0.0.zip",
							InfoJSON: struct {
								FactorioVersion string   `json:"factorio_version"`
								Dependencies    []string `json:"dependencies"`
							}{
								FactorioVersion: "2.0",
								Dependencies:    []string{},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			default:
				http.NotFound(w, r)
			}
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		u := &Updater{
			modServerURL: server.URL,
			factVersion:  "2.0",
			mods: map[string]*ModData{
				"trimmod": {Name: "trimmod", Title: "trimmod", Enabled: true},
			},
			httpClient: server.Client(),
		}

		err := u.ResolveMetadata()
		if err != nil {
			t.Fatalf("ResolveMetadata() returned unexpected error: %v", err)
		}

		if _, ok := u.mods["spacey-dep"]; !ok {
			t.Error("expected whitespace-padded dependency 'spacey-dep' to be discovered after TrimSpace")
		}
	})
}

// --- Integration tests below require a live Factorio installation at ~/factorio ---
// These skip automatically when the installation is not present (e.g., in CI).

func factorioRoot(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("could not determine home directory")
	}
	root := filepath.Join(home, "factorio")
	if _, err := os.Stat(filepath.Join(root, "bin", "x64", "factorio")); err != nil {
		t.Skip("live Factorio installation not found at ~/factorio, skipping integration test")
	}
	return root
}

func TestDetermineVersion(t *testing.T) {
	root := factorioRoot(t)

	u := &Updater{
		factPath: filepath.Join(root, "bin", "x64", "factorio"),
	}

	if err := u.determineVersion(); err != nil {
		t.Fatalf("determineVersion() returned unexpected error: %v", err)
	}

	// factVersion should be a major.minor string like "2.0"
	if u.factVersion == "" {
		t.Fatal("factVersion should not be empty")
	}

	parts := strings.Split(u.factVersion, ".")
	if len(parts) != 2 {
		t.Errorf("factVersion = %q; expected major.minor format (e.g. '2.0')", u.factVersion)
	}
}

func TestNewUpdater(t *testing.T) {
	root := factorioRoot(t)

	// Verify auth config exists
	settingsPath := filepath.Join(root, "data", "server-settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		settingsPath = ""
	}
	playerData := filepath.Join(root, "player-data.json")
	if _, err := os.Stat(playerData); err != nil {
		playerData = ""
	}
	if settingsPath == "" && playerData == "" {
		t.Skip("no auth config found, skipping NewUpdater integration test")
	}

	updater, err := NewUpdater(
		settingsPath,
		playerData,
		filepath.Join(root, "mods"),
		filepath.Join(root, "bin", "x64", "factorio"),
		"", "",
	)
	if err != nil {
		t.Fatalf("NewUpdater() returned unexpected error: %v", err)
	}

	// Should have detected a factorio version
	if updater.factVersion == "" {
		t.Error("factVersion should have been populated")
	}

	// Should have parsed at least some mods
	if len(updater.mods) == 0 {
		t.Error("mods map should not be empty after parsing mod-list.json")
	}

	// Built-in mods should be excluded
	for _, builtin := range []string{"base", "core", "space-age", "quality", "elevated-rails", "recycler"} {
		if _, ok := updater.mods[builtin]; ok {
			t.Errorf("built-in mod %q should have been excluded", builtin)
		}
	}

	// Auth should have been resolved
	if updater.username == "" || updater.token == "" {
		t.Error("username and token should have been resolved from config files")
	}
}

func TestRetrieveModMetadata(t *testing.T) {
	root := factorioRoot(t)

	settingsPath := filepath.Join(root, "data", "server-settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		settingsPath = ""
	}
	playerData := filepath.Join(root, "player-data.json")
	if _, err := os.Stat(playerData); err != nil {
		playerData = ""
	}

	updater, err := NewUpdater(
		settingsPath,
		playerData,
		filepath.Join(root, "mods"),
		filepath.Join(root, "bin", "x64", "factorio"),
		"", "",
	)
	if err != nil {
		t.Fatalf("NewUpdater() failed: %v", err)
	}

	// Pick a well-known mod that should always exist on the portal
	testMod := "helmod"
	if _, ok := updater.mods[testMod]; !ok {
		// Add it manually if not in mod-list
		updater.mods[testMod] = &ModData{
			Name:  testMod,
			Title: testMod,
		}
	}

	if err := updater.RetrieveModMetadata(testMod); err != nil {
		t.Fatalf("RetrieveModMetadata(%q) returned unexpected error: %v", testMod, err)
	}

	mod := updater.mods[testMod]

	// Title should have been populated from the API
	if mod.Title == "" || mod.Title == testMod {
		t.Errorf("mod title should have been resolved from API, got %q", mod.Title)
	}

	// Should have found a compatible release
	if mod.Latest == nil {
		t.Fatal("expected a compatible release to be found")
	}

	// Release should have required fields
	if mod.Latest.Version == "" {
		t.Error("latest release version should not be empty")
	}
	if mod.Latest.DownloadURL == "" {
		t.Error("latest release download URL should not be empty")
	}
	if mod.Latest.FileName == "" {
		t.Error("latest release filename should not be empty")
	}
	if mod.Latest.Sha1 == "" {
		t.Error("latest release SHA-1 should not be empty")
	}
}

// TestRedactSecrets verifies that credentials are stripped from error text in
// both their raw and URL-encoded forms.
func TestRedactSecrets(t *testing.T) {
	t.Run("nil error stays nil", func(t *testing.T) {
		if got := redactSecrets(nil, "tok"); got != nil {
			t.Errorf("redactSecrets(nil) = %v; want nil", got)
		}
	})

	t.Run("empty secrets are ignored", func(t *testing.T) {
		err := redactSecrets(fmt.Errorf("boom"), "", "")
		if err.Error() != "boom" {
			t.Errorf("message = %q; want %q", err.Error(), "boom")
		}
	})

	t.Run("raw and encoded forms are both replaced", func(t *testing.T) {
		secret := "a b+c"
		err := redactSecrets(fmt.Errorf("url=%s encoded=%s", secret, "a+b%2Bc"), secret)
		if strings.Contains(err.Error(), secret) {
			t.Errorf("raw secret survived redaction: %q", err.Error())
		}
	})

	t.Run("original error remains unwrappable", func(t *testing.T) {
		base := fmt.Errorf("token=hunter2")
		wrapped := redactSecrets(base, "hunter2")
		if !errors.Is(wrapped, base) {
			t.Error("errors.Is could not reach the original error")
		}
	})
}

// TestDownloadLatestRedactsCredentials is the regression test for the
// credential leak: Go's *url.Error embeds the full query string, so a transport
// failure must not surface the factorio.com username or token.
func TestDownloadLatestRedactsCredentials(t *testing.T) {
	const (
		token = "SUPERSECRETTOKEN99"
		user  = "secret-operator"
	)

	u := &Updater{
		// Port 1 is reserved and closed, forcing a dial error that carries the URL.
		modServerURL: "http://127.0.0.1:1",
		modPath:      t.TempDir(),
		username:     user,
		token:        token,
		mods:         make(map[string]*ModData),
		httpClient:   &http.Client{Timeout: 5 * time.Second},
	}
	u.mods["helmod"] = &ModData{
		Name:  "helmod",
		Title: "Helmod",
		Latest: &ModRelease{
			DownloadURL: "/download/helmod/deadbeef",
			FileName:    "helmod_1.0.0.zip",
			Version:     "1.0.0",
			Sha1:        "0000000000000000000000000000000000000000",
		},
	}

	_, err := u.downloadLatest("helmod", nil)
	if err == nil {
		t.Fatal("expected a transport error against a closed port")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("token leaked into error text: %q", err.Error())
	}
	if strings.Contains(err.Error(), user) {
		t.Errorf("username leaked into error text: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("expected a redaction marker, got: %q", err.Error())
	}
}

// TestSaveModListPreservesExistingEntries is the regression test for the
// mod-list.json erasure bug. Built-in and DLC mods are deliberately excluded
// from the tracking map so they are never downloaded, but they must still
// survive a rewrite: Factorio treats a mod with no entry as enabled, so losing
// `"space-age": false` silently re-enables the DLC.
func TestSaveModListPreservesExistingEntries(t *testing.T) {
	tmpDir := t.TempDir()

	original := `{
  "mods": [
    {"name": "base", "enabled": true},
    {"name": "space-age", "enabled": false},
    {"name": "quality", "enabled": false},
    {"name": "elevated-rails", "enabled": false},
    {"name": "helmod", "enabled": true, "version": "2.2.12"},
    {"name": "a-local-only-mod", "enabled": false}
  ]
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "mod-list.json"), []byte(original), 0600); err != nil {
		t.Fatalf("seeding mod-list.json: %v", err)
	}

	// The tracking map mirrors reality: built-ins filtered out by parseModList,
	// plus one dependency newly discovered during resolution.
	u := &Updater{
		modPath: tmpDir,
		mods: map[string]*ModData{
			"helmod": {Name: "helmod", Enabled: true},
			"flib":   {Name: "flib", Enabled: true},
		},
	}

	if err := u.saveModList(); err != nil {
		t.Fatalf("saveModList() returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "mod-list.json"))
	if err != nil {
		t.Fatalf("reading written mod-list.json: %v", err)
	}

	var result struct {
		Mods []map[string]any `json:"mods"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parsing written mod-list.json: %v", err)
	}

	got := make(map[string]map[string]any, len(result.Mods))
	for _, m := range result.Mods {
		name, _ := m["name"].(string)
		got[name] = m
	}

	// Nothing may be deleted, and disabled DLC must stay disabled.
	for _, name := range []string{"base", "space-age", "quality", "elevated-rails", "helmod", "a-local-only-mod"} {
		if _, ok := got[name]; !ok {
			t.Errorf("entry %q was deleted from mod-list.json", name)
		}
	}
	for _, name := range []string{"space-age", "quality", "elevated-rails", "a-local-only-mod"} {
		if entry, ok := got[name]; ok {
			if enabled, _ := entry["enabled"].(bool); enabled {
				t.Errorf("%q was silently re-enabled (enabled=true); want false", name)
			}
		}
	}

	// Fields this tool does not model must survive untouched.
	if v, _ := got["helmod"]["version"].(string); v != "2.2.12" {
		t.Errorf(`helmod lost its unmodelled "version" field: got %q, want "2.2.12"`, v)
	}

	// Newly discovered dependencies are appended.
	if entry, ok := got["flib"]; !ok {
		t.Error("newly discovered dependency 'flib' was not added to mod-list.json")
	} else if enabled, _ := entry["enabled"].(bool); !enabled {
		t.Error("newly added 'flib' should be enabled")
	}

	if len(result.Mods) != 7 {
		t.Errorf("expected 7 entries (6 original + flib), got %d", len(result.Mods))
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"2.1", "2.1.0", 0},
		{"2.0.16", "2.2.0", -1},
		{"2.2.1", "2.0.17", 1},
		{"2.10.0", "2.9.0", 1},  // lexical comparison would get this backwards
		{"2.9.0", "2.10.0", -1}, // and this one too
		{"1.0.0", "1.0.1", -1},
		{"0.18.47", "1.1.0", -1},
		{"", "0.0.0", 0},
		{"garbage", "0", 0},
	}
	for _, tt := range tests {
		if got := compareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d; want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

// TestRetrieveModMetadataSelectsHighestVersion is the regression test for
// release selection. The Mod Portal orders releases by publish time, not by
// version, so an author backporting to an older branch leaves a lower version
// as the last element. This mirrors helmod's real publish order.
func TestRetrieveModMetadataSelectsHighestVersion(t *testing.T) {
	body := `{
	  "title": "Helmod",
	  "deprecated": false,
	  "releases": [
	    {"version": "2.2.0",  "file_name": "helmod_2.2.0.zip",  "sha1": "a", "info_json": {"factorio_version": "2.0"}},
	    {"version": "2.2.1",  "file_name": "helmod_2.2.1.zip",  "sha1": "b", "info_json": {"factorio_version": "2.0"}},
	    {"version": "1.1.9",  "file_name": "helmod_1.1.9.zip",  "sha1": "c", "info_json": {"factorio_version": "1.1"}},
	    {"version": "2.0.16", "file_name": "helmod_2.0.16.zip", "sha1": "d", "info_json": {"factorio_version": "2.0"}}
	  ]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	u := &Updater{
		modServerURL: srv.URL,
		factVersion:  "2.0",
		mods:         map[string]*ModData{"helmod": {Name: "helmod"}},
		httpClient:   srv.Client(),
	}

	if err := u.RetrieveModMetadata("helmod"); err != nil {
		t.Fatalf("RetrieveModMetadata() returned unexpected error: %v", err)
	}

	latest := u.mods["helmod"].Latest
	if latest == nil {
		t.Fatal("no compatible release was selected")
	}
	if latest.Version != "2.2.1" {
		t.Errorf("selected version %q; want 2.2.1 (the highest compatible, not the last listed)", latest.Version)
	}
}

// TestDownloadLatestNeverDowngrades verifies the monotonicity guard: when the
// installed release is newer than the latest compatible one, nothing is
// fetched and nothing is reported as updated.
func TestDownloadLatestNeverDowngrades(t *testing.T) {
	tmpDir := t.TempDir()

	u := &Updater{
		// Any network attempt against a closed port would surface as an error.
		modServerURL: "http://127.0.0.1:1",
		modPath:      tmpDir,
		username:     "u",
		token:        "t",
		httpClient:   &http.Client{Timeout: 5 * time.Second},
		mods: map[string]*ModData{
			"helmod": {
				Name:      "helmod",
				Title:     "Helmod",
				Installed: true,
				Version:   "2.2.1",
				Latest: &ModRelease{
					Version:     "2.0.16",
					FileName:    "helmod_2.0.16.zip",
					DownloadURL: "/download/helmod/old",
					Sha1:        "0000000000000000000000000000000000000000",
				},
			},
		},
	}

	updated, err := u.downloadLatest("helmod", nil)
	if err != nil {
		t.Fatalf("expected no error and no network attempt, got: %v", err)
	}
	if updated {
		t.Error("reported an update while moving from 2.2.1 down to 2.0.16")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "helmod_2.0.16.zip")); err == nil {
		t.Error("the older release was downloaded despite being a downgrade")
	}
}

// TestParseModListPrefersHighestInstalledVersion covers several zips of the
// same mod sitting side by side; the highest version is the live one, which
// directory order does not reliably give.
func TestParseModListPrefersHighestInstalledVersion(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "mod-list.json"), []byte(`{"mods":[{"name":"helmod","enabled":true}]}`), 0600); err != nil {
		t.Fatalf("seeding mod-list.json: %v", err)
	}
	for _, v := range []string{"2.10.0", "2.9.0", "2.2.1"} {
		if err := os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("helmod_%s.zip", v)), []byte("x"), 0600); err != nil {
			t.Fatalf("seeding zip: %v", err)
		}
	}

	u := &Updater{modPath: tmpDir, mods: make(map[string]*ModData)}
	if err := u.parseModList(); err != nil {
		t.Fatalf("parseModList() returned unexpected error: %v", err)
	}

	if got := u.mods["helmod"].Version; got != "2.10.0" {
		t.Errorf("installed version = %q; want 2.10.0 (highest on disk)", got)
	}
}

// TestDetermineVersionPrefersBaseInfoJSON verifies that the Factorio version
// is read from the bundled base mod rather than by executing the binary, so a
// missing, non-executable or slow executable cannot abort the run.
func TestDetermineVersionPrefersBaseInfoJSON(t *testing.T) {
	root := t.TempDir()
	baseDir := filepath.Join(root, "data", "base")
	if err := os.MkdirAll(baseDir, 0750); err != nil {
		t.Fatalf("creating base mod dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "info.json"),
		[]byte(`{"name":"base","version":"2.0.55","title":"Base Mod"}`), 0600); err != nil {
		t.Fatalf("seeding info.json: %v", err)
	}

	u := &Updater{
		modPath: filepath.Join(root, "mods"),
		// Deliberately bogus: it must never be executed.
		factPath: filepath.Join(root, "bin", "x64", "does-not-exist"),
	}

	if err := u.determineVersion(); err != nil {
		t.Fatalf("determineVersion() returned unexpected error: %v", err)
	}
	if u.factVersion != "2.0" {
		t.Errorf("factVersion = %q; want 2.0", u.factVersion)
	}
}

// TestDetermineVersionErrorsClearlyWithoutBinaryOrInfoJSON checks the guidance
// given when neither source is available.
func TestDetermineVersionErrorsClearlyWithoutBinaryOrInfoJSON(t *testing.T) {
	u := &Updater{modPath: filepath.Join(t.TempDir(), "mods")}

	err := u.determineVersion()
	if err == nil {
		t.Fatal("expected an error when neither info.json nor a binary is available")
	}
	if !strings.Contains(err.Error(), "--bin-path") {
		t.Errorf("error should point at --bin-path, got: %v", err)
	}
}
