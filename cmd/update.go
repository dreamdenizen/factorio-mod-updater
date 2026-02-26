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
			pterm.Println("Fetching metadata and resolving dependencies...")
			err = updater.ResolveMetadata()
			if err != nil {
				pterm.Error.Println("Failed to resolve metadata:", err)
				return
			}
			pterm.Success.Println("Metadata fully resolved")
		} else {
			spinner, _ := pterm.DefaultSpinner.Start("Fetching metadata and resolving dependencies...")
			err = updater.ResolveMetadata()
			if err != nil {
				spinner.Fail("Failed to resolve metadata: ", err)
				return
			}
			spinner.Success("Metadata fully resolved")
		}
		pterm.Info.Println("Built-in Space Age expansions (space-age, quality, elevated-rails, core) are ignored.")

		err = updater.UpdateMods()
		if err != nil {
			pterm.Error.Println("Failed to complete update:", err)
		} else {
			pterm.Success.Println("Update complete!")
		}
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
