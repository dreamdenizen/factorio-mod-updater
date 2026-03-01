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

type CLIConfig struct {
	Username     string
	Token        string
	SettingsPath string
	DataPath     string
	ModPath      string
	FactPath     string
	RootDir      string
}

var rootCmd = &cobra.Command{
	Use:   "factorio-updater [ROOT_DIR]",
	Short: "Updates mods for a target factorio installation",
	Long:  `A modern cliff tool to manage updating and installing mods on a given Factorio server.`,
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := parseConfig(cmd, args)
		return runUpdateFlow(cfg)
	},
}

// Execute initializes the root command tree and delegates to Cobra for
// argument parsing and subcommand dispatch.
// Why: Serves as the primary CLI entrypoint, isolating Cobra initialization
// and global flags (like TTY detection) from the business logic.
func Execute() {
	// Disable pterm rich output and enforce RawOutput when stdout is not a terminal (e.g., AMP, CI, piped output)
	if !term.IsTerminal(int(os.Stdout.Fd())) || os.Getenv("NO_COLOR") != "" {
		pterm.DisableStyling()
		pterm.RawOutput = true
	}
	if err := rootCmd.Execute(); err != nil {
		pterm.Error.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringP("username", "u", "", "factorio.com username overriding server-settings.json/player-data.json")
	rootCmd.PersistentFlags().StringP("token", "t", "", "factorio.com API token overriding server-settings.json/player-data.json")
	rootCmd.PersistentFlags().StringP("server-settings", "s", "", "Absolute path to the server-settings.json file (overrides player-data.json)")
	rootCmd.PersistentFlags().StringP("player-data", "d", "", "Absolute path to the player-data.json file")
	rootCmd.PersistentFlags().StringP("mod-path", "m", "", "Path to the mods directory")
	rootCmd.PersistentFlags().StringP("bin-path", "b", "", "Path to the Factorio executable")
}

func parseConfig(cmd *cobra.Command, args []string) CLIConfig {
	cfg := CLIConfig{}
	cfg.Username, _ = cmd.Flags().GetString("username")
	cfg.Token, _ = cmd.Flags().GetString("token")
	cfg.SettingsPath, _ = cmd.Flags().GetString("server-settings")
	cfg.DataPath, _ = cmd.Flags().GetString("player-data")
	cfg.ModPath, _ = cmd.Flags().GetString("mod-path")
	cfg.FactPath, _ = cmd.Flags().GetString("bin-path")
	if len(args) > 0 {
		cfg.RootDir = args[0]
	}
	return cfg
}

// resolvePaths applies the path inference logic, deriving factPath and modPath
// from a root directory positional argument when explicit flags are absent.
func resolvePaths(cfg CLIConfig) (resolvedFactPath, resolvedModPath string, err error) {
	rd := cfg.RootDir
	fp := cfg.FactPath
	mp := cfg.ModPath

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
func buildUpdater(cfg CLIConfig) (*factorio.Updater, error) {
	resolvedFactPath, resolvedModPath, err := resolvePaths(cfg)
	if err != nil {
		return nil, err
	}

	return factorio.NewUpdater(
		cfg.SettingsPath,
		cfg.DataPath,
		resolvedModPath,
		resolvedFactPath,
		cfg.Username,
		cfg.Token,
	)
}
