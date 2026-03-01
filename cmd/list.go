package cmd

import (
	"fmt"

	"factorio-updater/internal/factorio"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list [ROOT_DIR]",
	Short: "List the currently installed mods with versions",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := parseConfig(cmd, args)
		updater, err := buildUpdater(cfg)
		if err != nil {
			return err
		}

		if pterm.RawOutput {
			pterm.Info.Println("Starting Factorio Mod Updater (List Mode)...")
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


		_ = printModList(updater)
		return nil
	},
}

// printModList renders the list of tracked mods to the console, using a rich
// It returns a summary string of the operations computed for persistent logging.
func printModList(updater *factorio.Updater) string {
	mods := updater.GetMods()

	upToDate := 0
	outdated := 0
	missing := 0
	disabled := 0

	tableData := pterm.TableData{
		{"Mod Name", "Enabled", "Installed", "Current Version", "Latest Version"},
	}

	for _, mod := range mods {
		lver := "N/A"
		if mod.Latest != nil {
			lver = mod.Latest.Version
		}
		cver := "N/A"
		if mod.Installed {
			cver = mod.Version
		}

		if !mod.Enabled {
			disabled++
			updater.WriteLog("  DISABLED  %s", mod.Title)
		} else if !mod.Installed {
			missing++
			updater.WriteLog("  MISSING   %s (latest: %s)", mod.Title, lver)
		} else if cver != lver {
			outdated++
			updater.WriteLog("  OUTDATED  %s (%s -> %s)", mod.Title, cver, lver)
		} else {
			upToDate++
			updater.WriteLog("  CURRENT   %s (%s)", mod.Title, cver)
		}

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

	summaryStr := fmt.Sprintf("Summary: %d up to date, %d outdated, %d missing, %d disabled (%d total)",
		upToDate, outdated, missing, disabled, len(mods))

	if pterm.RawOutput {
		fmt.Printf("\n%s\n", summaryStr)
	} else {
		_ = pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
	}

	return summaryStr
}

func init() {
	rootCmd.AddCommand(listCmd)
}
