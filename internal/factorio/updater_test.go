package factorio

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			name:      "mismatch full",
			installed: "2.0.1",
			mod:       "2.0.2",
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
	modList := map[string]interface{}{
		"mods": []map[string]interface{}{
			{"name": "base", "enabled": true},
			{"name": "space-age", "enabled": true},
			{"name": "quality", "enabled": true},
			{"name": "helmod", "enabled": true},
			{"name": "jetpack", "enabled": false},
			{"name": "not-installed-mod", "enabled": true},
		},
	}
	modListData, _ := json.Marshal(modList)
	os.WriteFile(filepath.Join(tmpDir, "mod-list.json"), modListData, 0644)

	// Create fake zip files to simulate installed mods
	os.WriteFile(filepath.Join(tmpDir, "helmod_2.2.12.zip"), []byte("fake"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "jetpack_0.4.15.zip"), []byte("fake"), 0644)

	u := &Updater{
		modPath: tmpDir,
		mods:    make(map[string]*ModData),
	}

	if err := u.parseModList(); err != nil {
		t.Fatalf("parseModList() returned unexpected error: %v", err)
	}

	// Built-in mods should be excluded
	for _, builtin := range []string{"base", "space-age", "quality"} {
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
		os.WriteFile(filepath.Join(tmpDir, "server-settings.json"), []byte(serverSettings), 0644)

		playerData := `{"service-username": "player_user", "service-token": "player_token"}`
		os.WriteFile(filepath.Join(tmpDir, "player-data.json"), []byte(playerData), 0644)

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
		os.WriteFile(filepath.Join(tmpDir, "player-data.json"), []byte(playerData), 0644)

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
		os.WriteFile(filepath.Join(tmpDir, "server-settings.json"), []byte(serverSettings), 0644)

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
		os.MkdirAll(modsDir, 0755)

		serverSettings := `{"username": "discovered_user", "token": "discovered_token"}`
		os.WriteFile(filepath.Join(tmpDir, "server-settings.json"), []byte(serverSettings), 0644)

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
}

func TestSaveModList(t *testing.T) {
	tmpDir := t.TempDir()

	// Seed an existing mod-list.json so saveModList can back it up
	os.WriteFile(filepath.Join(tmpDir, "mod-list.json"), []byte(`{"mods":[]}`), 0644)

	u := &Updater{
		modPath: tmpDir,
		mods: map[string]*ModData{
			"zebra-mod":   {Name: "zebra-mod", Enabled: true},
			"alpha-mod":   {Name: "alpha-mod", Enabled: false},
			"middle-mod":  {Name: "middle-mod", Enabled: true},
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

	// Verify alphabetical sort order
	if result.Mods[0].Name != "alpha-mod" {
		t.Errorf("first mod should be 'alpha-mod', got %q", result.Mods[0].Name)
	}
	if result.Mods[1].Name != "middle-mod" {
		t.Errorf("second mod should be 'middle-mod', got %q", result.Mods[1].Name)
	}
	if result.Mods[2].Name != "zebra-mod" {
		t.Errorf("third mod should be 'zebra-mod', got %q", result.Mods[2].Name)
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
	os.WriteFile(testFile, content, 0644)

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
