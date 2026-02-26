package factorio

import (
	"context"
	"crypto/sha1" // #nosec G505 - SHA-1 is mandated by the Factorio Mod Portal API for download validation.
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pterm/pterm"
)

// Package-level compiled regexps to avoid repeated compilation on every call.
var (
	versionRe  = regexp.MustCompile(`(?P<major>\d+)\.(?P<minor>\d+)(?:\.(?P<sub>\d+))?`)
	factVerRe  = regexp.MustCompile(`Version: (\d+)\.(\d+)\.\d+`)
	modZipRe   = regexp.MustCompile(`^(.*)_(\d+\.\d+\.\d+)\.zip$`)
	depRe      = regexp.MustCompile(`^(?:[~!?](?:\(\))? )?(?P<name>[\w -]+)(?: (?P<arg>(?:[<>]=?)|=) (?P<ver>\d+\.\d+\.\d+))?$`)
)

// maxAPIResponseBytes caps JSON response body reads to prevent memory exhaustion
// from malicious or malformed API responses.
const maxAPIResponseBytes = 10 * 1024 * 1024 // 10 MB

// Updater orchestrates the lifecycle of Factorio mod management, including
// authentication, version detection, metadata resolution, and download execution.
type Updater struct {
	modServerURL string
	settingsPath string
	dataPath     string
	modPath      string
	factPath     string
	username     string
	token        string

	factVersion string
	mods        map[string]*ModData
	httpClient  *http.Client
}

// ModData represents the tracked state of a single mod within the update graph.
type ModData struct {
	Name       string
	Title      string
	Enabled    bool
	Installed  bool
	Version    string
	Latest     *ModRelease
	Deprecated bool
}

// ModRelease represents a single versioned release artifact from the Mod Portal API.
type ModRelease struct {
	DownloadURL string `json:"download_url"`
	FileName    string `json:"file_name"`
	InfoJSON    struct {
		FactorioVersion string   `json:"factorio_version"`
		Dependencies    []string `json:"dependencies"`
	} `json:"info_json"`
	Sha1    string `json:"sha1"`
	Version string `json:"version"`
}

// ModPortalMetadata represents the JSON response from the /api/mods/{name}/full endpoint.
type ModPortalMetadata struct {
	Title      string       `json:"title"`
	Deprecated bool         `json:"deprecated"`
	Releases   []ModRelease `json:"releases"`
}

// NewUpdater hydrates the foundational configurations, attempting to ingest
// authentication tokens from explicit CLI flags, then falling back to
// server-settings.json or player-data.json.
func NewUpdater(settingsPath, dataPath, modPath, factPath, username, token string) (*Updater, error) {
	u := &Updater{
		modServerURL: "https://mods.factorio.com",
		settingsPath: settingsPath,
		dataPath:     dataPath,
		modPath:      modPath,
		factPath:     factPath,
		username:     username,
		token:        token,
		mods:         make(map[string]*ModData),
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}

	if u.username == "" || u.token == "" {
		if err := u.parseTokens(); err != nil {
			return nil, fmt.Errorf("parsing auth tokens: %w", err)
		}
	}

	if u.username == "" || u.token == "" {
		pathsMsg := u.settingsPath
		if u.dataPath != "" {
			if pathsMsg != "" {
				pathsMsg += " and "
			}
			pathsMsg += u.dataPath
		}
		if pathsMsg == "" {
			pathsMsg = "no default config files found"
		}
		return nil, fmt.Errorf("username or token not found in cli args or parsed configs (%s)", pathsMsg)
	}

	if err := u.determineVersion(); err != nil {
		return nil, fmt.Errorf("determining factorio version: %w", err)
	}

	if err := u.parseModList(); err != nil {
		return nil, fmt.Errorf("parsing mod list: %w", err)
	}

	return u, nil
}

func (u *Updater) parseTokens() error {
	type configData struct {
		Username        string `json:"username,omitempty"`
		Token           string `json:"token,omitempty"`
		ServiceUsername string `json:"service-username,omitempty"`
		ServiceToken    string `json:"service-token,omitempty"`
	}

	loadConfig := func(path string) (*configData, error) {
		if path == "" {
			return nil, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading config %s: %w", path, err)
		}
		var c configData
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parsing config %s: %w", path, err)
		}
		return &c, nil
	}

	baseDir := filepath.Dir(filepath.Clean(u.modPath))

	if u.settingsPath == "" {
		candidateData := filepath.Join(baseDir, "data", "server-settings.json")
		candidateRoot := filepath.Join(baseDir, "server-settings.json")
		if _, err := os.Stat(candidateData); err == nil {
			u.settingsPath = candidateData
		} else if _, err := os.Stat(candidateRoot); err == nil {
			u.settingsPath = candidateRoot
		}
	}

	if u.dataPath == "" {
		candidate := filepath.Join(baseDir, "player-data.json")
		if _, err := os.Stat(candidate); err == nil {
			u.dataPath = candidate
		}
	}

	settings, _ := loadConfig(u.settingsPath)
	data, _ := loadConfig(u.dataPath)

	if u.username == "" {
		if settings != nil && settings.Username != "" {
			u.username = settings.Username
		} else if data != nil && data.ServiceUsername != "" {
			u.username = data.ServiceUsername
		}
	}

	if u.token == "" {
		if settings != nil && settings.Token != "" {
			u.token = settings.Token
		} else if data != nil && data.ServiceToken != "" {
			u.token = data.ServiceToken
		}
	}

	return nil
}

func (u *Updater) determineVersion() error {
	cmd := exec.Command(u.factPath, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("running factorio binary %q: %w", u.factPath, err)
	}

	match := factVerRe.FindStringSubmatch(string(output))
	if len(match) > 2 {
		u.factVersion = fmt.Sprintf("%s.%s", match[1], match[2])
	} else {
		return fmt.Errorf("could not parse version from factorio binary output: %s", string(output))
	}

	return nil
}

func (u *Updater) parseModList() error {
	modListPath := filepath.Join(u.modPath, "mod-list.json")
	data, err := os.ReadFile(modListPath)
	if err != nil {
		return fmt.Errorf("reading mod-list.json: %w", err)
	}

	type modEntry struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	var modList struct {
		Mods []modEntry `json:"mods"`
	}

	if err := json.Unmarshal(data, &modList); err != nil {
		return fmt.Errorf("parsing mod-list.json: %w", err)
	}

	for _, m := range modList.Mods {
		if isBuiltInMod(m.Name) {
			continue
		}
		u.mods[m.Name] = &ModData{
			Name:    m.Name,
			Enabled: m.Enabled,
			Title:   m.Name, // Default to name until metadata resolves it
		}
	}

	// Detect currently installed mods from zip filenames
	files, err := os.ReadDir(u.modPath)
	if err == nil {
		for _, f := range files {
			if !f.IsDir() {
				match := modZipRe.FindStringSubmatch(f.Name())
				if len(match) == 3 {
					name := match[1]
					version := match[2]
					if m, ok := u.mods[name]; ok {
						m.Installed = true
						m.Version = version
					}
				}
			}
		}
	}

	return nil
}

// versionMatch determines if a mod release is compatible with the installed
// Factorio version, handling the legacy 0.18 ↔ 1.x equivalence.
func versionMatch(installed, mod string) bool {
	modMatch := versionRe.FindStringSubmatch(mod)
	instMatch := versionRe.FindStringSubmatch(installed)

	if len(modMatch) == 0 || len(instMatch) == 0 {
		return false
	}

	if strings.HasPrefix(installed, "1.") && strings.HasPrefix(mod, "0.18") {
		return true
	}

	if len(modMatch) > 3 && modMatch[3] != "" && len(instMatch) > 3 {
		return mod == installed
	}

	return modMatch[1] == instMatch[1] && modMatch[2] == instMatch[2]
}

// RetrieveModMetadata queries the Factorio Mod Portal API for a specific mod,
// selecting the latest release compatible with the detected Factorio version.
func (u *Updater) RetrieveModMetadata(mod string) error {
	m := u.mods[mod]
	apiURL := fmt.Sprintf("%s/api/mods/%s/full", u.modServerURL, url.PathEscape(mod))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("creating request for mod %q: %w", mod, err)
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching metadata for mod %q: %w", mod, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mod portal returned status %d for %q", resp.StatusCode, mod)
	}

	// Limit response body size to prevent memory exhaustion
	limitedReader := io.LimitReader(resp.Body, maxAPIResponseBytes)

	var meta ModPortalMetadata
	if err := json.NewDecoder(limitedReader).Decode(&meta); err != nil {
		return fmt.Errorf("decoding metadata for mod %q: %w", mod, err)
	}

	m.Title = meta.Title
	m.Deprecated = meta.Deprecated

	var latest *ModRelease
	for i := range meta.Releases {
		rel := &meta.Releases[i]
		if versionMatch(u.factVersion, rel.InfoJSON.FactorioVersion) {
			latest = rel
		}
	}
	m.Latest = latest

	return nil
}

// ResolveMetadata constructs the dependency graph by fetching metadata for all
// tracked mods and iteratively resolving transitive dependencies until the
// graph stabilizes.
func (u *Updater) ResolveMetadata() error {
	var errs []error

	// Fetch metadata for all initially tracked mods
	for mod := range u.mods {
		if err := u.RetrieveModMetadata(mod); err != nil {
			errs = append(errs, err)
		}
	}

	// Resolve missing transitive deps dynamically
	for {
		missingMods := make(map[string]bool)

		for _, data := range u.mods {
			if data.Latest == nil {
				continue
			}

			for _, depStr := range data.Latest.InfoJSON.Dependencies {
				// Skip optional (?) and incompatible (!) dependencies
				if strings.HasPrefix(depStr, "!") || strings.HasPrefix(depStr, "?") || strings.HasPrefix(depStr, "(?)") {
					continue
				}

				match := depRe.FindStringSubmatch(depStr)
				if len(match) > 1 {
					depName := match[1]
					if isBuiltInMod(depName) {
						continue
					}
					if _, ok := u.mods[depName]; !ok {
						missingMods[depName] = true
					}
				}
			}
		}

		if len(missingMods) == 0 {
			break
		}

		for m := range missingMods {
			u.mods[m] = &ModData{
				Name:    m,
				Title:   m,
				Enabled: true,
			}
			if err := u.RetrieveModMetadata(m); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// Non-fatal: metadata errors don't prevent listing/updating other mods
	if len(errs) > 0 {
		pterm.Warning.Printf("Encountered %d non-fatal metadata errors\n", len(errs))
	}

	return nil
}

// GetMods returns a sorted snapshot of all tracked mods, ordered alphabetically
// by title for deterministic UI rendering.
func (u *Updater) GetMods() []*ModData {
	list := make([]*ModData, 0, len(u.mods))
	for _, m := range u.mods {
		list = append(list, m)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Title < list[j].Title
	})

	return list
}

func (u *Updater) saveModList() error {
	type modEntry struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	type modOut struct {
		Mods []modEntry `json:"mods"`
	}

	out := modOut{Mods: make([]modEntry, 0, len(u.mods))}
	for mod, data := range u.mods {
		out.Mods = append(out.Mods, modEntry{Name: mod, Enabled: data.Enabled})
	}

	sort.Slice(out.Mods, func(i, j int) bool {
		return out.Mods[i].Name < out.Mods[j].Name
	})

	modListPath := filepath.Join(u.modPath, "mod-list.json")
	backupPath := filepath.Join(u.modPath, fmt.Sprintf("mod-list.%s.json", time.Now().Format("2006-01-02_1504.05")))

	if err := os.Rename(modListPath, backupPath); err != nil && !os.IsNotExist(err) {
		pterm.Warning.Printf("Failed to backup mod-list.json: %v\n", err)
	}

	bytes, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling mod-list: %w", err)
	}

	return os.WriteFile(modListPath, bytes, 0600)
}

// UpdateMods iterates over all tracked mods, pruning outdated releases and
// downloading the latest compatible versions. Errors for individual mods are
// accumulated and returned collectively rather than halting the entire process.
func (u *Updater) UpdateMods() error {
	var errs []error

	for mod, data := range u.mods {
		if data.Latest == nil {
			errs = append(errs, fmt.Errorf("metadata or release missing for mod %q on factorio version %q", data.Name, u.factVersion))
			continue
		}

		if err := u.pruneOld(mod); err != nil {
			errs = append(errs, fmt.Errorf("pruning old releases for %q: %w", mod, err))
		}
		if err := u.downloadLatest(mod); err != nil {
			errs = append(errs, fmt.Errorf("downloading %q: %w", mod, err))
		}
	}

	if err := u.saveModList(); err != nil {
		errs = append(errs, fmt.Errorf("saving mod-list: %w", err))
	}

	return errors.Join(errs...)
}

func (u *Updater) pruneOld(mod string) error {
	data := u.mods[mod]
	latestVersion := data.Latest.Version

	files, err := os.ReadDir(u.modPath)
	if err != nil {
		return fmt.Errorf("reading mod directory: %w", err)
	}

	pattern := regexp.MustCompile(fmt.Sprintf(`^%s_(\d+\.\d+\.\d+)\.zip$`, regexp.QuoteMeta(mod)))

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		match := pattern.FindStringSubmatch(f.Name())
		if len(match) == 2 && match[1] != latestVersion {
			removePath := filepath.Join(u.modPath, f.Name())
			if err := os.Remove(removePath); err != nil {
				return fmt.Errorf("removing %s: %w", f.Name(), err)
			}
			pterm.Info.Printf("Removed old release: %s\n", f.Name())
		}
	}

	return nil
}

func (u *Updater) downloadLatest(mod string) error {
	data := u.mods[mod]
	latest := data.Latest

	targetPath := filepath.Join(u.modPath, latest.FileName)

	needsDownload := false
	if data.Installed {
		if data.Version != latest.Version {
			needsDownload = true
		} else {
			// Validate hash of existing file
			// #nosec G401 - SHA-1 is mandated by the Factorio Mod Portal API.
			if !validateSHA1(latest.Sha1, targetPath) {
				needsDownload = true
			} else {
				pterm.Success.Printf("Validated %s (%s)\n", data.Title, data.Version)
			}
		}
	} else {
		needsDownload = true
	}

	if !needsDownload {
		return nil
	}

	// Build download URL using net/url for safe credential encoding
	dlURL, err := url.Parse(fmt.Sprintf("%s%s", u.modServerURL, latest.DownloadURL))
	if err != nil {
		return fmt.Errorf("parsing download URL for %q: %w", mod, err)
	}
	q := dlURL.Query()
	q.Set("username", u.username)
	q.Set("token", u.token)
	dlURL.RawQuery = q.Encode()

	var p *pterm.ProgressbarPrinter
	if pterm.RawOutput {
		pterm.Println(fmt.Sprintf("Downloading %s (%s)...", data.Title, latest.Version))
	} else {
		p, _ = pterm.DefaultProgressbar.WithTotal(100).WithTitle(fmt.Sprintf("Downloading %s (%s)", data.Title, latest.Version)).Start()
	}

	if err := downloadFile(targetPath, dlURL.String(), p, latest.Sha1); err != nil {
		pterm.Error.Printf("Failed to download %s: %v\n", data.Title, err)
		return err
	}

	pterm.Success.Printf("Downloaded %s\n", data.Title)
	return nil
}

// validateSHA1 computes the SHA-1 digest of the file at the given path and
// compares it against the expected hex-encoded hash string.
// #nosec G401 - SHA-1 is mandated by the Factorio Mod Portal API for download validation.
func validateSHA1(expectedHash, targetPath string) bool {
	f, err := os.Open(targetPath)
	if err != nil {
		return false
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}

	return hex.EncodeToString(h.Sum(nil)) == expectedHash
}

func downloadFile(targetPath string, dlURL string, p *pterm.ProgressbarPrinter, expectedHash string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return fmt.Errorf("creating download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	out, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", targetPath, err)
	}
	defer out.Close()

	counter := &writeCounter{
		Total:    uint64(resp.ContentLength),
		Progress: p,
	}

	if _, err = io.Copy(out, io.TeeReader(resp.Body, counter)); err != nil {
		if p != nil {
			p.Stop()
		}
		return fmt.Errorf("writing download data: %w", err)
	}
	if p != nil {
		p.Stop()
	}

	// #nosec G401 - SHA-1 is mandated by the Factorio Mod Portal API.
	if !validateSHA1(expectedHash, targetPath) {
		// Clean up corrupted download
		os.Remove(targetPath)
		return fmt.Errorf("SHA-1 validation failed for %s", targetPath)
	}

	return nil
}

type writeCounter struct {
	Total    uint64
	Current  uint64
	Progress *pterm.ProgressbarPrinter
}

func (wc *writeCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Current += uint64(n)
	if wc.Total > 0 && wc.Progress != nil {
		pct := int(float64(wc.Current) / float64(wc.Total) * 100)
		if pct > 100 {
			pct = 100
		}
		wc.Progress.Add(pct - wc.Progress.Current)
	}
	return n, nil
}

// isBuiltInMod determines if a given module name belongs to the official
// Factorio core distribution, which should not be queried on the mod portal.
func isBuiltInMod(name string) bool {
	switch name {
	case "base", "core", "space-age", "quality", "elevated-rails":
		return true
	default:
		return false
	}
}
