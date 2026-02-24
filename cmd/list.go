package cmd

import (
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list [ROOT_DIR]",
	Short: "List the currently installed mods with versions",
	Run: func(cmd *cobra.Command, args []string) {
		updater, err := buildUpdater(args)
		if err != nil {
			pterm.Error.Println(err)
			return
		}

		spinner, _ := pterm.DefaultSpinner.Start("Fetching metadata and resolving dependencies...")
		err = updater.ResolveMetadata()
		if err != nil {
			spinner.Fail("Failed to resolve metadata: ", err)
			return
		}
		spinner.Success("Metadata fully resolved")

		tableData := pterm.TableData{
			{"Mod Name", "Enabled", "Installed", "Current Version", "Latest Version"},
		}

		for _, mod := range updater.GetMods() {
			cver := "N/A"
			if mod.Installed {
				cver = mod.Version
			}
			lver := "N/A"
			if mod.Latest != nil {
				lver = mod.Latest.Version
			}

			// Evaluate semantic styling
			titleStr := mod.Title
			cverStr := cver
			lverStr := lver
			
			if !mod.Installed || cver != lver {
				titleStr = pterm.Red(titleStr)
				cverStr = pterm.Red(cverStr)
				lverStr = pterm.Red(lverStr)
			} else {
				titleStr = pterm.Green(titleStr)
				cverStr = pterm.Green(cverStr)
				lverStr = pterm.Green(lverStr)
			}

			enabledStr := pterm.Red("false")
			if mod.Enabled {
				enabledStr = pterm.Green("true")
			}

			installedStr := pterm.Red("false")
			if mod.Installed {
				installedStr = pterm.Green("true")
			}

			tableData = append(tableData, []string{
				titleStr,
				enabledStr,
				installedStr,
				cverStr,
				lverStr,
			})
		}
		
		pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
