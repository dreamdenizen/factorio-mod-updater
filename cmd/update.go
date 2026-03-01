package cmd

import (
	"fmt"

	"factorio-updater/internal/factorio"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:    "update [ROOT_DIR]",
	Short:  "Update all mods to their latest release",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := parseConfig(cmd, args)
		return runUpdateFlow(cfg)
	},
}

func runUpdateFlow(cfg CLIConfig) error {
	updater, err := buildUpdater(cfg)
	if err != nil {
		return err
	}

	if pterm.RawOutput {
		pterm.Info.Println("Starting Factorio Mod Updater (Update Mode)...")
		pterm.Println("Fetching metadata and resolving dependencies...")
		err = updater.ResolveMetadata()
		if err != nil {
			pterm.Warning.Println("Some metadata could not be resolved:", err)
		}
		pterm.Success.Println("Metadata resolution complete")
	} else {
		spinner, _ := pterm.DefaultSpinner.Start("Fetching metadata and resolving dependencies...")
		err = updater.ResolveMetadata()
		if err != nil {
			spinner.Warning("Some metadata could not be resolved")
		} else {
			spinner.Success("Metadata fully resolved")
		}
	}
	
	fmt.Println()
	printModList(updater)
	fmt.Println()

	if !updatesAvailable(updater) {
		pterm.Success.Println("All mods are up to date.")
		return nil
	}

	pterm.Info.Println("Built-in Space Age expansions (space-age, quality, elevated-rails, core) are ignored.")

	updatedCount, err := updater.UpdateMods()
	if err != nil {
		return fmt.Errorf("failed to complete update: %w", err)
	} else if updatedCount == 0 {
		pterm.Success.Println("No mod updates were required.")
	} else {
		pterm.Success.Printf("Update complete! Successfully updated %d mod(s).\n", updatedCount)
	}
	return nil
}

func updatesAvailable(updater *factorio.Updater) bool {
	for _, mod := range updater.GetMods() {
		if mod.Latest == nil {
			continue
		}
		if !mod.Installed {
			return true
		}
		if mod.Version != mod.Latest.Version {
			return true
		}
	}
	return false
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
