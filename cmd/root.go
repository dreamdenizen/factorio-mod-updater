package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"factorio-updater/internal/factorio"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	username     string
	token        string
	settingsPath string
	dataPath     string
	modPath      string
	factPath     string
	rootDir      string
)

var rootCmd = &cobra.Command{
	Use:   "factorio-updater [ROOT_DIR]",
	Short: "Updates mods for a target factorio installation",
	Long:  `A modern cliff tool to manage updating and installing mods on a given Factorio server.`,
	Run: func(cmd *cobra.Command, args []string) {
		runUpdateFlow(args)
	},
}

// Execute initializes the root command tree and delegates to Cobra for
// argument parsing and subcommand dispatch.
// Why: Serves as the primary CLI entrypoint, isolating Cobra initialization
// and global flags (like TTY detection) from the business logic.
func Execute() {
	// Disable pterm rich output when stdout is not a terminal (e.g., AMP, CI, piped output)
	if !term.IsTerminal(int(os.Stdout.Fd())) || os.Getenv("NO_COLOR") != "" {
		pterm.DisableStyling()
	}
	if err := rootCmd.Execute(); err != nil {
		pterm.Error.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "", "factorio.com username overriding server-settings.json/player-data.json")
	rootCmd.PersistentFlags().StringVarP(&token, "token", "t", "", "factorio.com API token overriding server-settings.json/player-data.json")
	rootCmd.PersistentFlags().StringVarP(&settingsPath, "server-settings", "s", "", "Absolute path to the server-settings.json file (overrides player-data.json)")
	rootCmd.PersistentFlags().StringVarP(&dataPath, "player-data", "d", "", "Absolute path to the player-data.json file")
	rootCmd.PersistentFlags().StringVarP(&modPath, "mod-path", "m", "", "Path to the mods directory")
	rootCmd.PersistentFlags().StringVarP(&factPath, "bin-path", "b", "", "Path to the Factorio executable")
}

// resolvePaths applies the path inference logic, deriving factPath and modPath
// from a root directory positional argument when explicit flags are absent.
func resolvePaths(args []string) (resolvedFactPath, resolvedModPath string, err error) {
	rd := rootDir
	fp := factPath
	mp := modPath

	if len(args) > 0 {
		rd = args[0]
	}

	if rd != "" {
		if fp == "" {
			if runtime.GOOS == "windows" {
				fp = filepath.Join(rd, "bin", "x64", "factorio.exe")
			} else {
				fp = filepath.Join(rd, "bin", "x64", "factorio")
			}
		}
		if mp == "" {
			mp = filepath.Join(rd, "mods")
		}
	}

	if fp == "" || mp == "" {
		return "", "", fmt.Errorf("must specify either a ROOT_DIR positional argument, or both --bin-path and --mod-path")
	}

	return fp, mp, nil
}

// buildUpdater resolves paths from CLI args/flags and constructs a fully
// initialized Updater ready for metadata resolution and mod operations.
func buildUpdater(args []string) (*factorio.Updater, error) {
	resolvedFactPath, resolvedModPath, err := resolvePaths(args)
	if err != nil {
		return nil, err
	}

	// Apply resolved paths back to package vars for consistency
	factPath = resolvedFactPath
	modPath = resolvedModPath
	if len(args) > 0 {
		rootDir = args[0]
	}

	return factorio.NewUpdater(
		settingsPath,
		dataPath,
		modPath,
		factPath,
		username,
		token,
	)
}
