package factorio

import (
	"cmp"
	"context"
	"crypto/sha1" // #nosec G505 - SHA-1 is mandated by the Factorio Mod Portal API for download validation.
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pterm/pterm"
	"golang.org/x/sync/errgroup"
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
// Why: Acts as the central domain model for all mod operations, decoupling the
// CLI presentation layer from HTTP interactions and filesystem mutations.
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
// Why: Normalizes state tracking between currently installed filesystem
// representation and remote API metadata, simplifying evaluation logic.
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
// Why: Provides strict structural typing for the Factorio API's release payload,
// enabling safe unmarshaling and reliable hash validation.
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
// Why: Serves as the root bounded context for all Mod Portal API queries, capturing
// only the required attributes (Title, Deprecated, Releases) avoiding memory overhead.
type ModPortalMetadata struct {
	Title      string       `json:"title"`
	Deprecated bool         `json:"deprecated"`
	Releases   []ModRelease `json:"releases"`
}

// NewUpdater hydrates the foundational configurations, attempting to ingest
// authentication tokens from explicit CLI flags, then falling back to
// server-settings.json or player-data.json.
// Why: Centralizes instantiation and enforces fail-fast credential, version,
// and local mod resolution before permitting any network operations.
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
		httpClient: &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
			},
		},
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

// parseTokens resolves authentication credentials by checking server-settings.json
// first, then falling back to player-data.json. CLI flags take priority over both.
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

	settings, err := loadConfig(u.settingsPath)
	if err != nil {
		pterm.Warning.Printf("Failed to parse %s: %v\n", u.settingsPath, err)
	}
	data, err := loadConfig(u.dataPath)
	if err != nil {
		pterm.Warning.Printf("Failed to parse %s: %v\n", u.dataPath, err)
	}

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

// determineVersion executes the Factorio binary with --version and parses
// the major.minor version string from its output.
// Why: Context timeout prevents the application from hanging indefinitely if
// the local factorio executable is artificially slow or blocking.
func (u *Updater) determineVersion() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, u.factPath, "--version")
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

// parseModList reads mod-list.json and scans the mods directory for installed
// zip files, populating the Updater's mod tracking map.
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
// Why: Segregates the network IO required for metadata hydration, allowing the
// graph resolver to iteratively fetch details precisely when new deps are discovered.
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
	defer func() { _ = resp.Body.Close() }()

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
// Why: Pre-computes the entire deployment plan to guarantee zero missing
// dependencies before executing any destructive filesystem modifications.
func (u *Updater) ResolveMetadata() error {
	var errs []error
	
	// mu protects the errs slice during concurrent metadata hydration requests, 
	// preventing data races.
	var mu sync.Mutex
	
	// eg bounds concurrent HTTP fetches. Waiting on this group explicitly blocks
	// function exit until all Goroutines complete, actively preventing memory leaks.
	eg := new(errgroup.Group)
	eg.SetLimit(10)

	// Collect and sort mod names to ensure deterministic network dispatch order
	var modNames []string
	for mod := range u.mods {
		modNames = append(modNames, mod)
	}
	slices.Sort(modNames)

	// Fetch metadata for all initially tracked mods
	for _, mod := range modNames {
		eg.Go(func() error {
			if err := u.RetrieveModMetadata(mod); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
			return nil
		})
	}
	_ = eg.Wait()

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

		var newModNames []string
		for m := range missingMods {
			newModNames = append(newModNames, m)
			u.mods[m] = &ModData{
				Name:    m,
				Title:   m,
				Enabled: true,
			}
		}
		slices.Sort(newModNames)

		// egDeps bounds concurrent missing dependency metadata fetches. 
		// Guaranteeing we await all Goroutines averts memory leaks on closure.
		egDeps := new(errgroup.Group)
		egDeps.SetLimit(10)

		for _, m := range newModNames {
			egDeps.Go(func() error {
				if err := u.RetrieveModMetadata(m); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
				return nil
			})
		}
		_ = egDeps.Wait()
	}

	if len(errs) > 0 {
		return fmt.Errorf("encountered %d metadata errors: %w", len(errs), errors.Join(errs...))
	}

	return nil
}

// GetMods returns a sorted snapshot of all tracked mods, ordered alphabetically
// by title for deterministic UI rendering.
// Why: Ensures the CLI or structured output consumes a predictable sequence,
// isolating map iteration randomness from the presentation tier.
func (u *Updater) GetMods() []*ModData {
	list := make([]*ModData, 0, len(u.mods))
	for _, m := range u.mods {
		list = append(list, m)
	}

	slices.SortFunc(list, func(a, b *ModData) int {
		return cmp.Compare(a.Title, b.Title)
	})

	return list
}

// saveModList writes the current mod tracking state back to mod-list.json,
// creating a timestamped backup of the previous version first.
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

	slices.SortFunc(out.Mods, func(a, b modEntry) int {
		return cmp.Compare(a.Name, b.Name)
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

	tmpPath := modListPath + ".tmp"
	if err := os.WriteFile(tmpPath, bytes, 0600); err != nil {
		return fmt.Errorf("writing mod-list to temporary file: %w", err)
	}

	if err := os.Rename(tmpPath, modListPath); err != nil {
		return fmt.Errorf("atomically renaming mod-list: %w", err)
	}

	return nil
}

// UpdateMods iterates over all tracked mods, pruning outdated releases and
// downloading the latest compatible versions. Errors for individual mods are
// accumulated and returned collectively rather than halting the entire process.
// Why: Adopts a fault-tolerant batch application model, maximizing the number of
// successfully updated mods even during partial Mod Portal outages.
func (u *Updater) UpdateMods() (int, error) {
	var errs []error
	var updatedCount int32
	
	// mu provides thread-safe appends to the errs slice across parallel downloads.
	var mu sync.Mutex

	var multi *pterm.MultiPrinter
	if !pterm.RawOutput {
		multi, _ = pterm.DefaultMultiPrinter.Start()
	}

	// eg bounds concurrent mod port API downloads to 5 parallel Goroutines.
	// We wait on the group at the end to ensure no runaway Goroutines or memory leaks.
	eg := new(errgroup.Group)
	eg.SetLimit(5)

	sortedMods := u.GetMods()
	for _, data := range sortedMods {
		mod := data.Name
		eg.Go(func() error {
			if data.Latest == nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("metadata or release missing for mod %q on factorio version %q", data.Name, u.factVersion))
				mu.Unlock()
				return nil
			}

			didUpdate, err := u.downloadLatest(mod, multi)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("downloading %q: %w", mod, err))
				mu.Unlock()
				return nil
			}

			if didUpdate {
				atomic.AddInt32(&updatedCount, 1)
			}
			return nil
		})
	}
	_ = eg.Wait()

	if multi != nil {
		_, _ = multi.Stop()
		fmt.Println() // Flush cursor downward to prevent print masking
	}

	// Safely prune old mod releases sequentially after rendering stops
	for _, data := range sortedMods {
		if data.Latest == nil {
			continue
		}
		if err := u.pruneOld(data.Name); err != nil {
			errs = append(errs, fmt.Errorf("pruning old releases for %q: %w", data.Name, err))
		}
	}

	if err := u.saveModList(); err != nil {
		errs = append(errs, fmt.Errorf("saving mod-list: %w", err))
	}

	return int(updatedCount), errors.Join(errs...)
}

// pruneOld removes all versioned zip files for the given mod that do not
// match the latest release version, ONLY if the latest version exists on disk.
func (u *Updater) pruneOld(mod string) error {
	data := u.mods[mod]
	latestVersion := data.Latest.Version
	
	// Sanitize against directory traversal payloads
	safeFileName := filepath.Base(filepath.Clean(data.Latest.FileName))
	latestPath := filepath.Join(u.modPath, safeFileName)

	if _, err := os.Stat(latestPath); os.IsNotExist(err) {
		// Newest version wasn't downloaded or is missing. Abort pruning to remain safe.
		return nil
	}

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

// downloadLatest checks whether the given mod needs a download (new install,
// version mismatch, or hash mismatch) and fetches it from the Mod Portal.
func (u *Updater) downloadLatest(mod string, multi *pterm.MultiPrinter) (bool, error) {
	data := u.mods[mod]
	latest := data.Latest

	// Sanitize against directory traversal payloads
	safeFileName := filepath.Base(filepath.Clean(latest.FileName))
	targetPath := filepath.Join(u.modPath, safeFileName)

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
				if pterm.RawOutput || multi == nil {
					pterm.Success.Printf("Validated %s (%s)\n", data.Title, data.Version)
				}
			}
		}
	} else {
		needsDownload = true
	}

	if !needsDownload {
		return false, nil
	}

	// Build download URL using net/url for safe credential encoding
	dlURL, err := url.Parse(fmt.Sprintf("%s%s", u.modServerURL, latest.DownloadURL))
	if err != nil {
		return false, fmt.Errorf("parsing download URL for %q: %w", mod, err)
	}
	q := dlURL.Query()
	q.Set("username", u.username)
	q.Set("token", u.token)
	dlURL.RawQuery = q.Encode()

	var p *pterm.ProgressbarPrinter
	if !pterm.RawOutput && multi != nil {
		pWriter := multi.NewWriter()
		p, _ = pterm.DefaultProgressbar.WithTotal(100).WithWriter(pWriter).WithTitle(fmt.Sprintf("Downloading %s (%s)", data.Title, latest.Version)).Start()
	} else {
		pterm.Info.Printf("Downloading %s (%s)...\n", data.Title, latest.Version)
	}

	if err := downloadFile(u.httpClient, targetPath, dlURL.String(), p, latest.Sha1); err != nil {
		if pterm.RawOutput || multi == nil {
			pterm.Error.Printf("Failed to download %s: %v\n", data.Title, err)
		}
		return false, err
	}

	if pterm.RawOutput || multi == nil {
		pterm.Success.Printf("Downloaded %s\n", data.Title)
	}
	return true, nil
}

// validateSHA1 computes the SHA-1 digest of the file at the given path and
// compares it against the expected hex-encoded hash string.
// #nosec G401 - SHA-1 is mandated by the Factorio Mod Portal API for download validation.
func validateSHA1(expectedHash, targetPath string) bool {
	f, err := os.Open(targetPath)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}

	return hex.EncodeToString(h.Sum(nil)) == expectedHash
}

// downloadFile fetches a file from dlURL, writes it to targetPath, tracks
// progress via the optional ProgressbarPrinter, and validates the SHA-1 hash.
func downloadFile(client *http.Client, targetPath string, dlURL string, p *pterm.ProgressbarPrinter, expectedHash string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return fmt.Errorf("creating download request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("executing download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	tmpPath := targetPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", tmpPath, err)
	}
	defer func() { _ = out.Close() }()

	counter := &writeCounter{
		Total:    uint64(resp.ContentLength),
		Progress: p,
	}

	if _, err = io.Copy(out, io.TeeReader(resp.Body, counter)); err != nil {
		if p != nil {
			_, _ = p.Stop()
		}
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing download data: %w", err)
	}
	if p != nil {
		_, _ = p.Stop()
	}

	// Ensure file is flushed and closed before reading it for validation
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("flushing to disk %s: %w", tmpPath, err)
	}

	// #nosec G401 - SHA-1 is mandated by the Factorio Mod Portal API.
	if !validateSHA1(expectedHash, tmpPath) {
		// Clean up corrupted download
		_ = os.Remove(tmpPath)
		return fmt.Errorf("SHA-1 validation failed for %s", tmpPath)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("atomically renaming download: %w", err)
	}

	return nil
}

// writeCounter wraps an io.Writer to track download progress and update
// a pterm ProgressbarPrinter with the current completion percentage.
type writeCounter struct {
	Total    uint64
	Current  uint64
	Progress *pterm.ProgressbarPrinter
}

// Write implements io.Writer, accumulating byte counts and updating the
// progress bar when both Total and Progress are non-zero/non-nil.
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
