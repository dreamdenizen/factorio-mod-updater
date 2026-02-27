package cmd

import (
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update [ROOT_DIR]",
	Short: "Update all mods to their latest release",
	Run: func(cmd *cobra.Command, args []string) {
		updater, err := buildUpdater(args)
		if err != nil {
			pterm.Error.Println(err)
			return
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
		pterm.Info.Println("Built-in Space Age expansions (space-age, quality, elevated-rails, core) are ignored.")

		updatedCount, err := updater.UpdateMods()
		if err != nil {
			pterm.Error.Println("Failed to complete update:", err)
		} else if updatedCount == 0 {
			pterm.Success.Println("No mod updates were required.")
		} else {
			pterm.Success.Printf("Update complete! Successfully updated %d mod(s).\n", updatedCount)
		}
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
