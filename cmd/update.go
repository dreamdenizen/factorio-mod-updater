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
	summaryStr := printModList(updater)
	fmt.Println()

	if !updatesAvailable(updater) {
		msg := "All mods are up to date."
		pterm.Success.Println(msg)
		updater.WriteLog("%s", msg)
		_ = updater.SaveLog(summaryStr)
		return nil
	}

	pterm.Info.Println("Built-in Space Age expansions (space-age, quality, elevated-rails, core) are ignored.")

	updatedCount, err := updater.UpdateMods()
	var finalMsg string
	if err != nil {
		finalMsg = fmt.Sprintf("Failed to complete update: %v", err)
	} else if updatedCount == 0 {
		finalMsg = "No mod updates were required."
		pterm.Success.Println(finalMsg)
	} else {
		finalMsg = fmt.Sprintf("Update complete! Successfully updated %d mod(s).", updatedCount)
		pterm.Success.Printf("%s\n", finalMsg)
	}
	
	updater.WriteLog("%s", finalMsg)
	if logErr := updater.SaveLog(summaryStr); logErr != nil {
		pterm.Warning.Printf("Failed to write last-mod-update.log: %v\n", logErr)
	}
	
	if err != nil {
		return fmt.Errorf("failed to complete update: %w", err)
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
